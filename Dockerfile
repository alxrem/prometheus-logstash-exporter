FROM golang:1.19-alpine AS builder
WORKDIR /src/
COPY go.mod go.sum main.go ./
RUN apk -U add binutils && CGO_ENABLED=0 go build -o prometheus-logstash-exporter && strip prometheus-logstash-exporter

FROM scratch
WORKDIR /
COPY --from=builder /src/prometheus-logstash-exporter /
EXPOSE 9304
ENTRYPOINT ["/prometheus-logstash-exporter"]
