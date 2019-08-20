package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/textproto"
    "net/url"
    "regexp"
    "strconv"
    "sync"
    "time"

    "github.com/pkg/errors"
    "github.com/prometheus/client_golang/prometheus"
)

// Collector implements prometheus.Collector (see below).
// it also contains the config of the exporter.
type Collector struct {
    URI      string
    Timeout  time.Duration
    Password string

    conn  net.Conn
    input *bufio.Reader
    url   *url.URL
    mutex sync.Mutex

    up            prometheus.Gauge
    failedScrapes prometheus.Counter
    totalScrapes  prometheus.Counter
}

// Metric are aither fetch from an api command, or from "status" parsing (thus RegexIndex)
type Metric struct {
    Name       string
    Help       string
    Type       prometheus.ValueType
    Command    string
    RegexIndex int
}

const (
    namespace = "freeswitch"
)

var (
    metricList = []Metric{
        Metric{Name: "current_calls", Type: prometheus.GaugeValue, Help: "Number of calls active", Command: "api show calls count as json"},
        Metric{Name: "uptime_seconds", Type: prometheus.GaugeValue, Help: "Uptime in seconds", Command: "api uptime s"},
        Metric{Name: "time_synced", Type: prometheus.GaugeValue, Help: "Is FreeSWITCH time in sync with exporter host time", Command: "api strepoch"},
        Metric{Name: "sessions_total", Type: prometheus.CounterValue, Help: "Number of sessions since startup", RegexIndex: 1},
        Metric{Name: "current_sessions", Type: prometheus.GaugeValue, Help: "Number of sessions active", RegexIndex: 2},
        Metric{Name: "current_sessions_peak", Type: prometheus.GaugeValue, Help: "Peak sessions since startup", RegexIndex: 3},
        Metric{Name: "current_sessions_peak_last_5min", Type: prometheus.GaugeValue, Help: "Peak sessions for the last 5 minutes", RegexIndex: 4},
        Metric{Name: "current_sps", Type: prometheus.GaugeValue, Help: "Number of sessions per second", RegexIndex: 5},
        Metric{Name: "current_sps_peak", Type: prometheus.GaugeValue, Help: "Peak sessions per second since startup", RegexIndex: 7},
        Metric{Name: "current_sps_peak_last_5min", Type: prometheus.GaugeValue, Help: "Peak sessions per second for the last 5 minutes", RegexIndex: 8},
        Metric{Name: "max_sps", Type: prometheus.GaugeValue, Help: "Max sessions per second allowed", RegexIndex: 6},
        Metric{Name: "max_sessions", Type: prometheus.GaugeValue, Help: "Max sessions allowed", RegexIndex: 9},
        Metric{Name: "current_idle_cpu", Type: prometheus.GaugeValue, Help: "CPU idle", RegexIndex: 11},
        Metric{Name: "min_idle_cpu", Type: prometheus.GaugeValue, Help: "Minimum CPU idle", RegexIndex: 10},
    }
    statusRegex = regexp.MustCompile(`(\d+) session\(s\) since startup\s+(\d+) session\(s\) - peak (\d+), last 5min (\d+)\s+(\d+) session\(s\) per Sec out of max (\d+), peak (\d+), last 5min (\d+)\s+(\d+) session\(s\) max\s+min idle cpu (\d+\.\d+)\/(\d+\.\d+)`)
)

// NewCollector processes uri, timeout and methods and returns a new Collector.
func NewCollector(uri string, timeout time.Duration, password string) (*Collector, error) {
    var c Collector

    c.URI = uri
    c.Timeout = timeout
    c.Password = password

    var url *url.URL
    var err error

    if url, err = url.Parse(c.URI); err != nil {
        return nil, errors.Wrap(err, "cannot parse URI")
    }

    c.url = url

    c.up = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: namespace,
        Name:      "up",
        Help:      "Was the last scrape successful.",
    })

    c.totalScrapes = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: namespace,
        Name:      "exporter_total_scrapes",
        Help:      "Current total freeswitch scrapes.",
    })

    c.failedScrapes = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: namespace,
        Name:      "exporter_failed_scrapes",
        Help:      "Number of failed freeswitch scrapes.",
    })

    return &c, nil
}

// scrape will connect to the freeswitch instance and push metrics to the Prometheus channel.
func (c *Collector) scrape(ch chan<- prometheus.Metric) error {
    c.totalScrapes.Inc()

    address := c.url.Host

    if c.url.Scheme == "unix" {
        address = c.url.Path
    }

    var err error

    c.conn, err = net.DialTimeout(c.url.Scheme, address, c.Timeout)

    if err != nil {
        return err
    }

    c.conn.SetDeadline(time.Now().Add(c.Timeout))
    defer c.conn.Close()

    c.input = bufio.NewReader(c.conn)

    if err = c.fsAuth(); err != nil {
        return err
    }

    if err = c.scapeMetrics(ch); err != nil {
        return err
    }

    if err = c.scrapeStatus(ch); err != nil {
        return err
    }

    return nil
}

func (c *Collector) scapeMetrics(ch chan<- prometheus.Metric) error {
    for _, metricDef := range metricList {
        if len(metricDef.Command) == 0 {
            // this metric will be fetched by scapeStatus
            continue
        }

        value, err := c.fetchMetric(&metricDef)

        if err != nil {
            return err
        }

        metric, err := prometheus.NewConstMetric(
            prometheus.NewDesc(namespace+"_"+metricDef.Name, metricDef.Help, nil, nil),
            metricDef.Type,
            value,
        )

        if err != nil {
            return err
        }

        ch <- metric
    }

    return nil
}

func (c *Collector) scrapeStatus(ch chan<- prometheus.Metric) error {
    response, err := c.fsCommand("api status")

    if err != nil {
        return err
    }

    matches := statusRegex.FindAllStringSubmatch(string(response), -1)

    if len(matches) != 1 {
        return errors.New("error parsing status")
    }

    for _, metricDef := range metricList {
        if len(metricDef.Command) != 0 {
            // this metric will be fetched by fetchMetric
            continue
        }

        if len(matches[0]) < metricDef.RegexIndex {
            return errors.New("error parsing status")
        }

        strValue := matches[0][metricDef.RegexIndex]
        value, err := strconv.ParseFloat(strValue, 64)

        if err != nil {
            return errors.Wrap(err, "error parsing status")
        }

        metric, err := prometheus.NewConstMetric(
            prometheus.NewDesc(namespace+"_"+metricDef.Name, metricDef.Help, nil, nil),
            metricDef.Type,
            value,
        )

        if err != nil {
            return err
        }

        ch <- metric
    }

    return nil
}

func (c *Collector) fetchMetric(metricDef *Metric) (float64, error) {
    now := time.Now()
    response, err := c.fsCommand(metricDef.Command)

    if err != nil {
        return 0, err
    }

    switch metricDef.Name {
    case "current_calls":
        r := struct {
            Count float64 `json:"row_count"`
        }{}

        err = json.Unmarshal(response, &r)

        if err != nil {
            return 0, errors.Wrap(err, "cannot read JSON response")
        }

        return r.Count, nil
    case "uptime_seconds":
        raw := string(response)

        if raw[len(raw)-1:len(raw)] == "\n" {
            raw = raw[:len(raw)-1]
        }

        value, err := strconv.ParseFloat(raw, 64)

        if err != nil {
            return 0, errors.Wrap(err, "cannot read uptime")
        }

        return value, nil
    case "time_synced":
        value, err := strconv.ParseInt(string(response), 10, 64)

        if err != nil {
            return 0, errors.Wrap(err, "cannot read FreeSWITCH time")
        }

        if now.Unix() == value {
            return 1, nil
        }

        return 0, nil
    }

    return 0, errors.Errorf("unknown metric: %s", metricDef.Name)
}

func (c *Collector) fsCommand(command string) ([]byte, error) {
    _, err := c.conn.Write([]byte(command + "\n\n"))

    if err != nil {
        return nil, errors.Wrap(err, "cannot write command")
    }

    mimeReader := textproto.NewReader(c.input)
    message, err := mimeReader.ReadMIMEHeader()

    if err != nil {
        return nil, errors.Wrap(err, "cannot read command response")
    }

    value := message.Get("Content-Length")
    length, _ := strconv.Atoi(value)

    body := make([]byte, length)
    _, err = io.ReadFull(c.input, body)

    if err != nil {
        return nil, err
    }

    return body, nil
}

func (c *Collector) fsAuth() error {
    mimeReader := textproto.NewReader(c.input)
    message, err := mimeReader.ReadMIMEHeader()

    if err != nil {
        return errors.Wrap(err, "read auth failed")
    }

    if message.Get("Content-Type") != "auth/request" {
        return errors.New("auth failed (unknown content-type)")
    }

    _, err = c.conn.Write([]byte(fmt.Sprintf("auth %s\n\n", c.Password)))

    if err != nil {
        return errors.Wrap(err, "write auth failed")
    }

    message, err = mimeReader.ReadMIMEHeader()

    if err != nil {
        return errors.Wrap(err, "read auth failed")
    }

    if message.Get("Content-Type") != "command/reply" {
        return errors.New("auth failed (unknown reply)")
    }

    if message.Get("Reply-Text") != "+OK accepted" {
        return errors.Errorf("auth failed: %s", message.Get("Reply-Text"))
    }

    return nil
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
    prometheus.DescribeByCollect(c, ch)
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
    c.mutex.Lock()
    defer c.mutex.Unlock()

    err := c.scrape(ch)

    if err != nil {
        c.failedScrapes.Inc()
        c.up.Set(0)
        log.Println("[error]", err)
    } else {
        c.up.Set(1)
    }

    ch <- c.up
    ch <- c.totalScrapes
    ch <- c.failedScrapes
}
