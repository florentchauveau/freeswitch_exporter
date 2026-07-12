package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "freeswitch"

// Collector implements prometheus.Collector and holds the exporter config. It
// opens a fresh ESL connection per scrape; only immutable config, precomputed
// descriptors and the self-metrics live on the struct.
type Collector struct {
	URI      string
	Timeout  time.Duration
	Password string

	url     *url.URL
	logger  *slog.Logger
	metrics []Metric
	mutex   sync.Mutex

	up            prometheus.Gauge
	failedScrapes prometheus.Counter
	totalScrapes  prometheus.Counter
}

// NewCollector processes uri, timeout and password and returns a new Collector.
func NewCollector(uri string, timeout time.Duration, password string, logger *slog.Logger) (*Collector, error) {
	if logger == nil {
		logger = slog.Default()
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("cannot parse URI: %w", err)
	}

	c := &Collector{
		URI:      uri,
		Timeout:  timeout,
		Password: password,
		url:      u,
		logger:   logger,
	}

	// Copy the metric definitions so each collector owns its descriptors,
	// built once here rather than on every scrape.
	c.metrics = make([]Metric, len(metricList))
	copy(c.metrics, metricList)
	for i := range c.metrics {
		c.metrics[i].desc = prometheus.NewDesc(
			namespace+"_"+c.metrics[i].Name, c.metrics[i].Help, nil, nil)
	}

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

	return c, nil
}

// scrape connects to the freeswitch instance and pushes metrics to ch.
func (c *Collector) scrape(ch chan<- prometheus.Metric) error {
	c.totalScrapes.Inc()

	address := c.url.Host
	if c.url.Scheme == "unix" {
		address = c.url.Path
	}

	conn, err := net.DialTimeout(c.url.Scheme, address, c.Timeout)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return err
	}

	client, err := newESLClient(conn, c.Password)
	if err != nil {
		return err
	}

	if err := c.scrapeCommands(ch, client); err != nil {
		return err
	}

	return c.scrapeStatus(ch, client)
}

// scrapeCommands fetches the metrics that have their own ESL api command.
func (c *Collector) scrapeCommands(ch chan<- prometheus.Metric, client *eslClient) error {
	for i := range c.metrics {
		metricDef := &c.metrics[i]
		if metricDef.Command == "" {
			// this metric is parsed from "api status" by scrapeStatus
			continue
		}

		now := time.Now()

		response, err := client.api(metricDef.Command)
		if err != nil {
			return err
		}

		value, err := metricDef.Parse(response, now, c.logger)
		if err != nil {
			return err
		}

		metric, err := prometheus.NewConstMetric(metricDef.desc, metricDef.Type, value)
		if err != nil {
			return err
		}

		ch <- metric
	}

	return nil
}

// scrapeStatus fetches "api status" once and emits every regex-sourced metric.
func (c *Collector) scrapeStatus(ch chan<- prometheus.Metric, client *eslClient) error {
	response, err := client.api("api status")
	if err != nil {
		return err
	}

	values, err := parseStatus(response)
	if err != nil {
		return err
	}

	for i := range c.metrics {
		metricDef := &c.metrics[i]
		if metricDef.Command != "" {
			// this metric is fetched by scrapeCommands
			continue
		}

		if metricDef.RegexIndex >= len(values) {
			return errors.New("error parsing status")
		}

		metric, err := prometheus.NewConstMetric(metricDef.desc, metricDef.Type, values[metricDef.RegexIndex])
		if err != nil {
			return err
		}

		ch <- metric
	}

	return nil
}

// Describe implements prometheus.Collector. Descriptors are known up front, so
// registration performs no network I/O.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for i := range c.metrics {
		ch <- c.metrics[i].desc
	}

	ch <- c.up.Desc()
	ch <- c.totalScrapes.Desc()
	ch <- c.failedScrapes.Desc()
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := c.scrape(ch); err != nil {
		c.failedScrapes.Inc()
		c.up.Set(0)
		c.logger.Error("scrape failed", "err", err)
	} else {
		c.up.Set(1)
	}

	ch <- c.up
	ch <- c.totalScrapes
	ch <- c.failedScrapes
}
