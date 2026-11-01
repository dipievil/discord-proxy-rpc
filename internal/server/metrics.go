package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsNamespace = "discord_proxy"

// Metrics holds the Prometheus collectors exposed on /metrics. Gauges read
// their values from the live hub, rate limiter and IPC state getter at scrape
// time, so no counting logic is duplicated outside those components.
type Metrics struct {
	registry      *prometheus.Registry
	presenceSends prometheus.Counter
}

// NewMetrics builds the collector set on a private registry, leaving the
// process-global default registry untouched so tests can construct multiple
// servers without duplicate-registration panics.
func NewMetrics(hub *Hub, getIPCState func() string, rl *RateLimiter) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		presenceSends: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "presence_coalesced_sends_total",
			Help:      "Number of coalesced presence updates flushed to subscribers.",
		}),
	}

	rejections := func() float64 {
		if rl == nil {
			return 0
		}
		return float64(rl.Rejections())
	}

	registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ws_connected_clients",
			Help:      "Number of WebSocket clients currently connected to the hub.",
		}, func() float64 {
			return float64(hub.ClientCount())
		}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ipc_connected",
			Help:      "Whether the Discord IPC connection is up (1) or down (0).",
		}, func() float64 {
			if getIPCState() == string(StateConnected) {
				return 1
			}
			return 0
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "rate_limit_rejections_total",
			Help:      "Number of WebSocket upgrade requests rejected by the rate limiter.",
		}, rejections),
		m.presenceSends,
	)

	return m
}

// RecordPresenceSend increments the coalesced presence sends counter. Callers
// invoke it from the presence subscription callback, which fires once per
// coalesced flush.
func (m *Metrics) RecordPresenceSend() {
	m.presenceSends.Inc()
}

// Handler returns the Prometheus exposition handler serving this metrics set.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// WithMetrics exposes the metrics on the /metrics route. The route is
// registered here instead of in registerRoutes to keep the metrics change
// isolated from http.go; ServeMux routes /metrics by longest-pattern match
// regardless of registration order, so it overrides the dashboard catch-all.
func WithMetrics(m *Metrics) ServerOption {
	return func(s *Server) {
		s.mux.Handle("/metrics", m.Handler())
	}
}
