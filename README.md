Logstash exporter
=================

Prometheus exporter for metrics provided by Node Stats API of Logstash.

Building and running
--------------------

    go get gitlab.com/alxrem/prometheus-logstash-exporter
    cd ${GOPATH-$HOME/go}/src/gitlab.com/alxrem/prometheus-logstash-exporter
    go build
    ./prometheus-logstash-exporter <flags>

To see all available configuration flags:

    ./prometheus-logstash-exporter -h
    
Packages
--------

Packages for Debian Stretch and Ubuntu Xenial are available on
[PackageCloud](https://packagecloud.io/alxrem/prometheus-logstash-exporter/).

Docker image is available at [Docker Hub](https://hub.docker.com/r/alxrem/prometheus-logstash-exporter/).
Pull the latest version with

    docker pull alxrem/prometheus-logstash-exporter