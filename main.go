// Command freeswitch_exporter is a Prometheus exporter for FreeSWITCH,
// scraping metrics over the Event Socket Layer (ESL).
package main

import (
	"net/http"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

func main() {
	var (
		metricsPath = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		scrapeURI   = kingpin.Flag("freeswitch.scrape-uri", `URI on which to scrape freeswitch. E.g. "tcp://localhost:8021"`).Short('u').Default("tcp://localhost:8021").String()
		timeout     = kingpin.Flag("freeswitch.timeout", "Timeout for trying to get stats from freeswitch.").Short('t').Default("5s").Duration()
		password    = kingpin.Flag("freeswitch.password", "Password for freeswitch event socket. Can also be set via the FREESWITCH_PASSWORD environment variable.").Short('P').Envar("FREESWITCH_PASSWORD").Default("ClueCon").String()
	)

	promslogConfig := &promslog.Config{}
	promslogflag.AddFlags(kingpin.CommandLine, promslogConfig)
	toolkitFlags := webflag.AddFlags(kingpin.CommandLine, ":9282")

	kingpin.Version(version.Print("freeswitch_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)

	logger.Info("Starting freeswitch_exporter", "version", version.Info())
	logger.Info("Build context", "build_context", version.BuildContext())

	c, err := NewCollector(*scrapeURI, *timeout, *password, logger)
	if err != nil {
		logger.Error("Error creating collector", "err", err)
		os.Exit(1)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(c)
	registry.MustRegister(versioncollector.NewCollector("freeswitch_exporter"))

	http.Handle(*metricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}))

	if *metricsPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "FreeSWITCH Exporter",
			Description: "Prometheus exporter for FreeSWITCH",
			Version:     version.Info(),
			Links: []web.LandingLinks{
				{Address: *metricsPath, Text: "Metrics"},
			},
		}

		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			logger.Error("Error creating landing page", "err", err)
			os.Exit(1)
		}

		http.Handle("/", landingPage)
	}

	server := &http.Server{}
	if err := web.ListenAndServe(server, toolkitFlags, logger); err != nil {
		logger.Error("Error running HTTP server", "err", err)
		os.Exit(1)
	}
}
