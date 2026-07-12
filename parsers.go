package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// timeSyncTolerance is the maximum allowed drift (in seconds) between the
// exporter host clock and the FreeSWITCH clock before time_synced reports 0.
// A tolerance is required because a scrape involves a network round-trip that
// can legitimately cross a one-second boundary even when clocks are in sync.
const timeSyncTolerance = 1

// parseFunc turns a raw ESL api response into a metric value. now is the
// exporter's wall-clock at fetch time (used by parseTimeSynced); logger is
// used for non-fatal diagnostics. Parsers that need neither ignore them.
type parseFunc func(response []byte, now time.Time, logger *slog.Logger) (float64, error)

// Metric describes a single exported metric. It is sourced either from its own
// ESL api Command (parsed by Parse), or from a capture group of the "api status"
// text output (identified by RegexIndex, with Command left empty).
type Metric struct {
	Name       string
	Help       string
	Type       prometheus.ValueType
	Command    string
	RegexIndex int
	Parse      parseFunc

	desc *prometheus.Desc
}

var (
	metricList = []Metric{
		{Name: "current_calls", Type: prometheus.GaugeValue, Help: "Number of calls active", Command: "api show calls count as json", Parse: parseCallsCount},
		{Name: "uptime_seconds", Type: prometheus.GaugeValue, Help: "Uptime in seconds", Command: "api uptime s", Parse: parseUptime},
		{Name: "time_synced", Type: prometheus.GaugeValue, Help: "Is FreeSWITCH time in sync with exporter host time", Command: "api strepoch", Parse: parseTimeSynced},
		{Name: "sessions_total", Type: prometheus.CounterValue, Help: "Number of sessions since startup", RegexIndex: 1},
		{Name: "current_sessions", Type: prometheus.GaugeValue, Help: "Number of sessions active", RegexIndex: 2},
		{Name: "current_sessions_peak", Type: prometheus.GaugeValue, Help: "Peak sessions since startup", RegexIndex: 3},
		{Name: "current_sessions_peak_last_5min", Type: prometheus.GaugeValue, Help: "Peak sessions for the last 5 minutes", RegexIndex: 4},
		{Name: "current_sps", Type: prometheus.GaugeValue, Help: "Number of sessions per second", RegexIndex: 5},
		{Name: "current_sps_peak", Type: prometheus.GaugeValue, Help: "Peak sessions per second since startup", RegexIndex: 7},
		{Name: "current_sps_peak_last_5min", Type: prometheus.GaugeValue, Help: "Peak sessions per second for the last 5 minutes", RegexIndex: 8},
		{Name: "max_sps", Type: prometheus.GaugeValue, Help: "Max sessions per second allowed", RegexIndex: 6},
		{Name: "max_sessions", Type: prometheus.GaugeValue, Help: "Max sessions allowed", RegexIndex: 9},
		{Name: "current_idle_cpu", Type: prometheus.GaugeValue, Help: "CPU idle", RegexIndex: 11},
		{Name: "min_idle_cpu", Type: prometheus.GaugeValue, Help: "Minimum CPU idle", RegexIndex: 10},
	}
	statusRegex = regexp.MustCompile(`(\d+) session\(s\) since startup\s+(\d+) session\(s\) - peak (\d+), last 5min (\d+)\s+(\d+) session\(s\) per Sec out of max (\d+), peak (\d+), last 5min (\d+)\s+(\d+) session\(s\) max\s+min idle cpu (\d+\.\d+)\/(\d+\.\d+)`)
)

// parseCallsCount reads the row_count field from the JSON returned by
// "api show calls count as json".
func parseCallsCount(response []byte, _ time.Time, _ *slog.Logger) (float64, error) {
	r := struct {
		Count float64 `json:"row_count"`
	}{}

	if err := json.Unmarshal(response, &r); err != nil {
		return 0, fmt.Errorf("cannot read JSON response: %w", err)
	}

	return r.Count, nil
}

// parseUptime reads the integer/float uptime returned by "api uptime s".
func parseUptime(response []byte, _ time.Time, _ *slog.Logger) (float64, error) {
	raw := strings.TrimSpace(string(response))

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot read uptime: %w", err)
	}

	return value, nil
}

// parseTimeSynced compares the FreeSWITCH epoch returned by "api strepoch"
// against the exporter host clock, returning 1 when within timeSyncTolerance
// and 0 otherwise.
func parseTimeSynced(response []byte, now time.Time, logger *slog.Logger) (float64, error) {
	raw := strings.TrimSpace(string(response))

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot read FreeSWITCH time: %w", err)
	}

	diff := now.Unix() - value
	if diff < 0 {
		diff = -diff
	}

	if diff <= timeSyncTolerance {
		return 1, nil
	}

	logger.Warn("time not in sync between system and FreeSWITCH",
		"system", now.Unix(), "freeswitch", value)

	return 0, nil
}

// parseStatus extracts the numeric capture groups from the "api status" output.
// The returned slice is indexed by capture-group number (index 0 is unused, to
// match Metric.RegexIndex which counts from 1).
func parseStatus(response []byte) ([]float64, error) {
	matches := statusRegex.FindAllSubmatch(response, -1)

	if len(matches) != 1 {
		return nil, errors.New("error parsing status")
	}

	groups := matches[0]
	values := make([]float64, len(groups))

	for i := 1; i < len(groups); i++ {
		value, err := strconv.ParseFloat(string(groups[i]), 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing status: %w", err)
		}

		values[i] = value
	}

	return values, nil
}
