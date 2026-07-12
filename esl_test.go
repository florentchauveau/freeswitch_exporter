package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// serveFakeFS plays the FreeSWITCH ESL side of conn: it performs the auth
// handshake (accepting authPassword) and then answers each api command with the
// matching body from responses. It is best-effort and simply returns when the
// client disconnects.
func serveFakeFS(conn net.Conn, authPassword string, responses map[string]string) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := textproto.NewReader(bufio.NewReader(conn))

	// Auth handshake.
	if _, err := io.WriteString(conn, "Content-Type: auth/request\n\n"); err != nil {
		return
	}
	line, err := reader.ReadLine() // "auth <password>"
	if err != nil {
		return
	}
	_, _ = reader.ReadLine() // trailing blank line

	if line != "auth "+authPassword {
		_, _ = io.WriteString(conn, "Content-Type: command/reply\nReply-Text: -ERR invalid\n\n")
		return
	}
	if _, err := io.WriteString(conn, "Content-Type: command/reply\nReply-Text: +OK accepted\n\n"); err != nil {
		return
	}

	// Command loop.
	for {
		command, err := reader.ReadLine()
		if err != nil {
			return
		}
		_, _ = reader.ReadLine() // trailing blank line

		body := responses[command]
		_, _ = fmt.Fprintf(conn, "Content-Type: api/response\nContent-Length: %d\n\n%s", len(body), body)
	}
}

func TestESLClientAPI(t *testing.T) {
	server, client := net.Pipe()
	go serveFakeFS(server, "ClueCon", map[string]string{"api uptime s": "12345\n"})

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	c, err := newESLClient(client, "ClueCon")
	if err != nil {
		t.Fatalf("newESLClient: %v", err)
	}

	body, err := c.api("api uptime s")
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	if got := string(body); got != "12345\n" {
		t.Fatalf("got %q, want %q", got, "12345\n")
	}

	_ = client.Close()
}

func TestESLClientAuthFailure(t *testing.T) {
	server, client := net.Pipe()
	go serveFakeFS(server, "ClueCon", nil)

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := newESLClient(client, "wrong-password"); err == nil {
		t.Fatal("expected auth error with wrong password")
	}

	_ = client.Close()
}

func TestCollectorScrape(t *testing.T) {
	responses := map[string]string{
		"api show calls count as json": `{"row_count": 7}`,
		"api uptime s":                 "123456\n",
		"api strepoch":                 strconv.FormatInt(time.Now().Unix(), 10),
		"api status":                   statusSample,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		serveFakeFS(conn, "ClueCon", responses)
	}()

	c, err := NewCollector("tcp://"+ln.Addr().String(), 5*time.Second, "ClueCon", testLogger())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	want := map[string]float64{
		"freeswitch_up":                      1,
		"freeswitch_exporter_total_scrapes":  1,
		"freeswitch_exporter_failed_scrapes": 0,
		"freeswitch_current_calls":           7,
		"freeswitch_uptime_seconds":          123456,
		"freeswitch_time_synced":             1,
		"freeswitch_sessions_total":          1234,
		"freeswitch_current_sessions":        5,
		"freeswitch_max_sessions":            1000,
		"freeswitch_min_idle_cpu":            0.00,
		"freeswitch_current_idle_cpu":        95.5,
	}

	for name, exp := range want {
		if got := metricValue(t, mfs, name); got != exp {
			t.Errorf("%s: got %v, want %v", name, got, exp)
		}
	}
}

func TestCollectorScrapeUnreachable(t *testing.T) {
	// A port nothing is listening on: scrape must fail gracefully with up=0
	// and must not error out of Gather.
	c, err := NewCollector("tcp://127.0.0.1:1", 500*time.Millisecond, "ClueCon", testLogger())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	if got := metricValue(t, mfs, "freeswitch_up"); got != 0 {
		t.Errorf("freeswitch_up: got %v, want 0", got)
	}
	if got := metricValue(t, mfs, "freeswitch_exporter_failed_scrapes"); got != 1 {
		t.Errorf("freeswitch_exporter_failed_scrapes: got %v, want 1", got)
	}
}

func TestDescribeNoNetwork(t *testing.T) {
	// Describe must enumerate every descriptor without dialing FreeSWITCH.
	c, err := NewCollector("tcp://127.0.0.1:1", time.Second, "ClueCon", testLogger())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	ch := make(chan *prometheus.Desc)
	go func() {
		c.Describe(ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}

	want := len(metricList) + 3 // + up, total_scrapes, failed_scrapes
	if count != want {
		t.Fatalf("Describe emitted %d descriptors, want %d", count, want)
	}
}

// metricValue returns the value of the first sample of the named metric family.
func metricValue(t *testing.T, mfs []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		m := mf.GetMetric()[0]
		switch {
		case m.Gauge != nil:
			return m.Gauge.GetValue()
		case m.Counter != nil:
			return m.Counter.GetValue()
		case m.Untyped != nil:
			return m.Untyped.GetValue()
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}
