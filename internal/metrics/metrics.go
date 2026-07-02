package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type M struct {
	reg             *prometheus.Registry
	IngestTotal     *prometheus.CounterVec
	ConvergeErrors  prometheus.Counter
	ActiveIncidents prometheus.Gauge
	Degraded        prometheus.Gauge
}

func New() *M {
	reg := prometheus.NewRegistry()
	m := &M{
		reg:             reg,
		IngestTotal:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "charon_ingest_total", Help: "events ingested"}, []string{"status"}),
		ConvergeErrors:  prometheus.NewCounter(prometheus.CounterOpts{Name: "charon_converge_errors_total", Help: "reconcile failures"}),
		ActiveIncidents: prometheus.NewGauge(prometheus.GaugeOpts{Name: "charon_active_incidents", Help: "incidents on the board"}),
		Degraded:        prometheus.NewGauge(prometheus.GaugeOpts{Name: "charon_discord_degraded", Help: "1 when the discord path is failing"}),
	}
	reg.MustRegister(m.IngestTotal, m.ConvergeErrors, m.ActiveIncidents, m.Degraded)
	return m
}

func (m *M) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}
