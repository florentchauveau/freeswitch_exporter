package main

import (
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"
)

// statusSample mirrors the numeric lines of a real "api status" response. The
// eleven captured values are, in RegexIndex order (1..11):
// 1234, 5, 20, 10, 3, 30, 25, 15, 1000, 0.00, 95.50.
const statusSample = `UP 0 years, 0 days, 1 hour, 2 minutes, 3 seconds
FreeSWITCH (Version 1.10.9) is ready
1234 session(s) since startup
5 session(s) - peak 20, last 5min 10
3 session(s) per Sec out of max 30, peak 25, last 5min 15
1000 session(s) max
min idle cpu 0.00/95.50
`

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseCallsCount(t *testing.T) {
	value, err := parseCallsCount([]byte(`{"row_count": 7}`), time.Time{}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != 7 {
		t.Fatalf("got %v, want 7", value)
	}

	if _, err := parseCallsCount([]byte("not json"), time.Time{}, testLogger()); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestParseUptime(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    float64
		wantErr bool
	}{
		{"trailing newline", "123456\n", 123456, false},
		{"no newline", "789", 789, false},
		{"surrounding space", "  42 \n", 42, false},
		{"empty does not panic", "", 0, true},
		{"garbage", "abc", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := parseUptime([]byte(tc.in), time.Time{}, testLogger())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != tc.want {
				t.Fatalf("got %v, want %v", value, tc.want)
			}
		})
	}
}

func TestParseTimeSynced(t *testing.T) {
	now := time.Unix(1_600_000_000, 0)

	cases := []struct {
		name  string
		epoch int64
		want  float64
	}{
		{"exact", now.Unix(), 1},
		{"one second behind", now.Unix() - 1, 1},
		{"one second ahead", now.Unix() + 1, 1},
		{"two seconds behind", now.Unix() - 2, 0},
		{"two seconds ahead", now.Unix() + 2, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(strconv.FormatInt(tc.epoch, 10))
			value, err := parseTimeSynced(in, now, testLogger())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != tc.want {
				t.Fatalf("epoch %d: got %v, want %v", tc.epoch, value, tc.want)
			}
		})
	}

	if _, err := parseTimeSynced([]byte("not-a-number"), now, testLogger()); err == nil {
		t.Fatal("expected error on non-numeric epoch")
	}
}

func TestParseStatus(t *testing.T) {
	values, err := parseStatus([]byte(statusSample))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[int]float64{
		1:  1234, // sessions_total
		2:  5,    // current_sessions
		3:  20,   // current_sessions_peak
		4:  10,   // current_sessions_peak_last_5min
		5:  3,    // current_sps
		6:  30,   // max_sps
		7:  25,   // current_sps_peak
		8:  15,   // current_sps_peak_last_5min
		9:  1000, // max_sessions
		10: 0.00, // min_idle_cpu
		11: 95.5, // current_idle_cpu
	}

	for idx, exp := range want {
		if idx >= len(values) {
			t.Fatalf("index %d out of range (len %d)", idx, len(values))
		}
		if values[idx] != exp {
			t.Errorf("index %d: got %v, want %v", idx, values[idx], exp)
		}
	}

	// Every regex-sourced metric must map to a valid index.
	for _, m := range metricList {
		if m.Command == "" && m.RegexIndex >= len(values) {
			t.Errorf("metric %s RegexIndex %d out of range (len %d)", m.Name, m.RegexIndex, len(values))
		}
	}

	if _, err := parseStatus([]byte("no numbers here")); err == nil {
		t.Fatal("expected error on unparseable status")
	}
}
