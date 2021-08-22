package main

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Short('l').Default(":9282").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		scrapeURI     = kingpin.Flag("freeswitch.scrape-uri", `URI on which to scrape freeswitch. E.g. "tcp://localhost:8021"`).Short('u').Default("tcp://localhost:8021").String()
		timeout       = kingpin.Flag("freeswitch.timeout", "Timeout for trying to get stats from freeswitch.").Short('t').Default("5s").Duration()
		password      = kingpin.Flag("freeswitch.password", "Password for freeswitch event socket.").Short('P').Default("ClueCon").String()
	)

	kingpin.Parse()

	c, err := NewCollector(*scrapeURI, *timeout, *password)

	if err != nil {
		panic(err)
	}

	prometheus.MustRegister(c)

	http.Handle(*metricsPath, promhttp.Handler())
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
