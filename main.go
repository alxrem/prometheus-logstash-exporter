// Copyright 2017-2020 Alexey Remizov
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
	"os"
	"os/signal"
	"syscall"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		log.Println("ERROR:", err)
	} else {
		e.collectMetrics(stats, ch)
	}
	ch <- e.up
}

func (e *Exporter) collectMetrics(stats *Stats, ch chan<- prometheus.Metric) {
	for _, k := range []string{"jvm", "events", "process", "reloads"} {
		if tree, ok := (*stats)[k]; ok {
			e.collectTree(k, tree, prometheus.Labels{}, ch)
		}
	}

	if pipelines, ok := (*stats)["pipelines"]; ok {
		for pipelineName, data := range pipelines.(map[string]interface{}) {
			e.collectPipeline(pipelineName, data, ch)
		}
	} else {
		e.collectPipeline("", (*stats)["pipeline"], ch)
	}
}

func (e *Exporter) collectTree(name string, data interface{}, labels prometheus.Labels, ch chan<- prometheus.Metric) {
	if v, ok := parseData(data); ok {
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
			if k == "patterns_per_field" {
				e.collectFields(name+"_"+k, d, labels, ch)
			} else {
				e.collectTree(name+"_"+k, d, labels, ch)
			}
		}
	}
}

func (e *Exporter) collectFields(name string, data interface{}, labels prometheus.Labels, ch chan<- prometheus.Metric) {
	fields, ok := data.(map[string]interface{})
	if !ok || len(fields) == 0 {
		return
	}

	labelNames := make([]string, 0)
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	labelsCopy := prometheus.Labels{}
	for k, v := range labels {
		labelsCopy[k] = v
	}

	for field, v := range fields {
		if v, ok := v.(float64); ok {
			vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      name,
			}, append(labelNames, "field"))
			labelsCopy["field"] = field
			vec.With(labelsCopy).Set(v)
			vec.Collect(ch)
		}
	}
}

func (e *Exporter) collectPipeline(pipelineName string, data interface{}, ch chan<- prometheus.Metric) {
	stats, ok := data.(map[string]interface{})
	if !ok {
		log.Println("ERROR: Wrong format of pipeline statistics")
		return
	}

	labels := prometheus.Labels{}
	if pipelineName != "" {
		labels["pipeline"] = pipelineName
	}

	for _, k := range []string{"events", "reloads", "queue", "dead_letter_queue"} {
		e.collectTree("pipeline_"+k, stats[k], labels, ch)
	}

	for _, k := range []string{"inputs", "filters", "outputs"} {
		e.collectPlugins("pipeline_plugins", k, stats["plugins"], pipelineName, ch)
	}
}

func (e *Exporter) collectPlugins(name string, section string, data interface{}, pipelineName string, ch chan<- prometheus.Metric) {
	stats := data.(map[string]interface{})
	plugins := stats[section].([]interface{})
	for _, p := range plugins {
		plugin := p.(map[string]interface{})
		labels := prometheus.Labels{"id": "", "name": ""}

		if id, exists := plugin["id"]; exists {
			labels["id"] = id.(string)
			delete(plugin, "id")
		}
		if name, exists := plugin["name"]; exists {
			labels["name"] = name.(string)
			delete(plugin, "name")
		}
		if pipelineName != "" {
			labels["pipeline"] = pipelineName
		}
		e.collectTree(name+"_"+section, plugin, labels, ch)
	}
}

func parseData(data interface{}) (float64, bool) {
	if value, ok := data.(float64); ok {
		return value, ok
	}

	if v, ok := data.(string); ok {
		if timestamp, err := time.Parse(time.RFC3339, v); err == nil {
			return float64(timestamp.Unix()), true
		}
	}

	return 0, false
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("ERROR:", err)
		}
	}()

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

func servePong(w http.ResponseWriter, r *http.Request) {
	// return text for debugging
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("... pong (HTTP200)"))
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

	go func() {
		intChan := make(chan os.Signal)
		termChan := make(chan os.Signal)

		signal.Notify(intChan, syscall.SIGINT)
		signal.Notify(termChan, syscall.SIGTERM)

		select {
		case <-intChan:
			log.Println("INFO: Received SIGINT, exiting")
			os.Exit(0)
		case <-termChan:
			log.Println("INFO: Received SIGTERM, exiting")
			os.Exit(0)
		}
	}()

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/-/ping", servePong)

	log.Println("INFO: Listening on", *listenAddress)
	log.Println("FATAL:", http.ListenAndServe(*listenAddress, nil))
}
