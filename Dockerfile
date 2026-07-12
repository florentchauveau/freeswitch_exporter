# build
FROM golang:1.24 AS builder

ARG VERSION=dev
ARG REVISION=unknown
ARG BRANCH=unknown

WORKDIR /src

# Cache module downloads separately from the source build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -a \
    -ldflags="-s -w \
      -X github.com/prometheus/common/version.Version=${VERSION} \
      -X github.com/prometheus/common/version.Revision=${REVISION} \
      -X github.com/prometheus/common/version.Branch=${BRANCH} \
      -X github.com/prometheus/common/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /freeswitch_exporter

# run
FROM scratch

COPY --from=builder /freeswitch_exporter /freeswitch_exporter

LABEL org.opencontainers.image.title="freeswitch_exporter" \
      org.opencontainers.image.description="Prometheus exporter for FreeSWITCH" \
      org.opencontainers.image.source="https://github.com/florentchauveau/freeswitch_exporter" \
      org.opencontainers.image.authors="Florent CHAUVEAU <florentch@pm.me>"

# Run unprivileged (nobody); the exporter binds to a high port only.
USER 65534:65534

EXPOSE 9282
ENTRYPOINT ["/freeswitch_exporter"]
