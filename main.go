package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"io/ioutil"
	"net/http"
	"time"
)

const (
	namespace = "logstash"
)

type Exporter struct {
	nodeStatsUri string
	timeout      time.Duration
	up           prometheus.Gauge
}

type Stats map[string]interface{}

func NewExporter(host string, timeout time.Duration) *Exporter {
	return &Exporter{
		nodeStatsUri: fmt.Sprintf("http://%s/_node/stats", host),
		timeout:      timeout,
		up: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "up",
				Help:      "Was the last scrape of logstash successful",
			},
		),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	stats, err := e.fetchStats()
	if err != nil {
		log.Errorln(err)
	} else {
		e.collectMetrics(stats, ch)
	}
	ch <- e.up
}

func (e *Exporter) collectMetrics(stats *Stats, ch chan<- prometheus.Metric) {
	for _, k := range []string{"jvm", "process", "reloads"} {
		e.collectTree(k, (*stats)[k], prometheus.Labels{}, ch)
	}
	e.collectPipeline((*stats)["pipeline"], ch)
}

func (e *Exporter) collectTree(name string, data interface{}, labels prometheus.Labels, ch chan<- prometheus.Metric) {
	if v, ok := data.(float64); ok {
		if len(labels) == 0 {
			metric := prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      name,
			})
			metric.Set(v)
			ch <- metric
		} else {
			labelNames := make([]string, 0)
			for k := range labels {
				labelNames = append(labelNames, k)
			}
			vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      name,
			}, labelNames)
			vec.With(labels).Set(v)
			vec.Collect(ch)
		}
		return
	}

	if v, ok := data.(map[string]interface{}); ok {
		for k, d := range v {
			e.collectTree(name+"_"+k, d, labels, ch)
		}
	}
}

func (e *Exporter) collectPipeline(data interface{}, ch chan<- prometheus.Metric) {
	stats, ok := data.(map[string]interface{})
	if !ok {
		log.Error("Wrong format of pipeline statistics")
		return
	}
	for _, k := range []string{"events", "reloads", "queue"} {
		e.collectTree("pipeline_"+k, stats[k], prometheus.Labels{}, ch)
	}
	for _, k := range []string{"inputs", "filters", "outputs"} {
		e.collectPlugins("pipeline_plugins", k, stats["plugins"], ch)
	}
}

func (e *Exporter) collectPlugins(name string, section string, data interface{}, ch chan<- prometheus.Metric) {
	stats := data.(map[string]interface{})
	plugins := stats[section].([]interface{})
	for _, p := range plugins {
		plugin := p.(map[string]interface{})
		labels := prometheus.Labels{
			"id":   plugin["id"].(string),
			"name": plugin["name"].(string),
		}
		delete(plugin, "id")
		delete(plugin, "name")
		e.collectTree(name+"_"+section, plugin, labels, ch)
	}
}

func (e *Exporter) fetchStats() (*Stats, error) {
	body, err := e.fetch(e.nodeStatsUri)
	if err != nil {
		return nil, err
	}

	var stats Stats
	err = json.Unmarshal(body, &stats)
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

func (e *Exporter) fetch(uri string) ([]byte, error) {
	client := http.Client{
		Timeout: e.timeout,
	}

	resp, err := client.Get(uri)
	if err != nil {
		e.up.Set(0)
		return nil, err
	}
	defer resp.Body.Close()

	e.up.Set(1)

	if resp.StatusCode != http.StatusOK {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9304", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		logstashHost  = flag.String("logstash.host", "localhost", "Host address of logstash server.")
		logstashPort  = flag.Int("logstash.port", 9600, "Port of logstash server.")
		timeout       = flag.Duration("logstash.timeout", 5*time.Second, "Timeout to get stats from logstash server.")
	)
	flag.Parse()

	exporter := NewExporter(fmt.Sprintf("%s:%d", *logstashHost, *logstashPort), *timeout)
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.Handler())

	log.Infoln("Listening on", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
