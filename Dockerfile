# build
FROM golang:1.17 as builder

WORKDIR /go/src

COPY . /go/src/

RUN CGO_ENABLED=0 go build -a -o freeswitch_exporter

# run
FROM scratch
COPY --from=builder /go/src/freeswitch_exporter /freeswitch_exporter

LABEL author="Florent CHAUVEAU <florent.chauveau@gmail.com>"

EXPOSE 9636

ENTRYPOINT [ "/freeswitch_exporter" ]
