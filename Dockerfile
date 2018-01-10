FROM golang:alpine AS builder
WORKDIR /go/src/gitlab.com/alxrem/prometheus-logstash-exporter
COPY vendor/ ./vendor/
COPY main.go ./
RUN apk -U add binutils && go build && strip prometheus-logstash-exporter

FROM alpine
WORKDIR /
COPY --from=builder /go/src/gitlab.com/alxrem/prometheus-logstash-exporter/prometheus-logstash-exporter /
EXPOSE 9304
ENTRYPOINT ["/prometheus-logstash-exporter"]