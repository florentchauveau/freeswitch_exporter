# FreeSWITCH Exporter for Prometheus
[![Go Report Card](https://goreportcard.com/badge/github.com/florentchauveau/freeswitch_exporter)](https://goreportcard.com/report/github.com/florentchauveau/freeswitch_exporter)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/florentchauveau/freeswitch_exporter/blob/master/LICENSE)

A [FreeSWITCH](https://freeswitch.org/confluence/display/FREESWITCH/FreeSWITCH+Explained) exporter for Prometheus.

It communicates with FreeSWITCH using [mod_event_socket](https://freeswitch.org/confluence/display/FREESWITCH/mod_event_socket).

## Getting Started

Pre-built binaries are available in [releases](https://github.com/florentchauveau/freeswitch_exporter/releases).

To run it:
```bash
./freeswitch_exporter [flags]
```

Help on flags:
```
./freeswitch_exporter --help

Flags:
      --help                   Show context-sensitive help (also try --help-long and --help-man).
  -l, --web.listen-address=":9636"  
                               Address to listen on for web interface and telemetry.
      --web.telemetry-path="/metrics"  
                               Path under which to expose metrics.
  -u, --freeswitch.scrape-uri="tcp://localhost:8021"  
                               URI on which to scrape freeswitch. E.g. "tcp://localhost:8021"
  -t, --freeswitch.timeout=5s  Timeout for trying to get stats from freeswitch.
  -P, --freeswitch.password="ClueCon"  
                               Password for freeswitch event socket.
```

## Usage

Make sure [mod_event_socket](https://freeswitch.org/confluence/display/FREESWITCH/mod_event_socket) is enabled on your FreeSWITCH instance. The default mod_event_socket configuration binds to `::` (i.e., to listen to connections from any host), which will work on IPv4 or IPv6. 

You can specify the scrape URI with the `--freeswitch.scrape-uri` flag. Example:

```
./freeswitch_exporter -u "tcp://localhost:5049"
```

Also, you need to make sure that the exporter will be allowed by the ACL (if any), and that the password matches.

## Metrics

The exporter will try to fetch values from the following commands:

- `api show calls count`: Calls count
- `api uptime s`: Uptime
- `api strepoch`: Time synced with system
- `status`

List of exposed metrics:

```bash
# HELP freeswitch_current_calls Number of calls active
# TYPE freeswitch_current_calls gauge
# HELP freeswitch_current_idle_cpu CPU idle
# TYPE freeswitch_current_idle_cpu gauge
# HELP freeswitch_current_sessions Number of sessions active
# TYPE freeswitch_current_sessions gauge
# HELP freeswitch_current_sessions_peak Peak sessions since startup
# TYPE freeswitch_current_sessions_peak gauge
# HELP freeswitch_current_sessions_peak_last_5min Peak sessions for the last 5 minutes
# TYPE freeswitch_current_sessions_peak_last_5min gauge
# HELP freeswitch_current_sps Number of sessions per second
# TYPE freeswitch_current_sps gauge
# HELP freeswitch_current_sps_peak Peak sessions per second since startup
# TYPE freeswitch_current_sps_peak gauge
# HELP freeswitch_current_sps_peak_last_5min Peak sessions per second for the last 5 minutes
# TYPE freeswitch_current_sps_peak_last_5min gauge
# HELP freeswitch_exporter_failed_scrapes Number of failed freeswitch scrapes.
# TYPE freeswitch_exporter_failed_scrapes counter
# HELP freeswitch_exporter_total_scrapes Current total freeswitch scrapes.
# TYPE freeswitch_exporter_total_scrapes counter
# HELP freeswitch_max_sessions Max sessions allowed
# TYPE freeswitch_max_sessions gauge
# HELP freeswitch_max_sps Max sessions per second allowed
# TYPE freeswitch_max_sps gauge
# HELP freeswitch_min_idle_cpu Minimum CPU idle
# TYPE freeswitch_min_idle_cpu gauge
# HELP freeswitch_sessions_total Number of sessions since startup
# TYPE freeswitch_sessions_total counter
# HELP freeswitch_time_synced Is FreeSWITCH time in sync with exporter host time
# TYPE freeswitch_time_synced gauge
# HELP freeswitch_up Was the last scrape successful.
# TYPE freeswitch_up gauge
# HELP freeswitch_uptime_seconds Uptime in seconds
# TYPE freeswitch_uptime_seconds gauge
```

## Compiling

With go1.17+, clone the project and:

```bash
go build
```

Dependencies will be fetched automatically.

## Contributing

Feel free to send pull requests.
