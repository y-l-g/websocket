package websocket

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Connections    prometheus.Gauge
	Messages       prometheus.Counter
	Subscriptions  prometheus.Counter
	AuthDuration   prometheus.Histogram
	BreakerTripped prometheus.Counter
	AuthFailures   prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "frankenphp_websocket",
			Name:      "connections_active",
			Help:      "Current number of active WebSocket connections",
		}),
		Messages: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "frankenphp_websocket",
			Name:      "messages_total",
			Help:      "Total number of messages published to the Hub",
		}),
		Subscriptions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "frankenphp_websocket",
			Name:      "subscriptions_total",
			Help:      "Total number of channel subscriptions",
		}),
		AuthDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "frankenphp_websocket",
			Name:      "auth_duration_seconds",
			Help:      "Duration of PHP Worker authentication requests",
			Buckets:   prometheus.DefBuckets,
		}),
		BreakerTripped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "frankenphp_websocket",
			Name:      "circuit_breaker_open_total",
			Help:      "Total number of auth requests rejected because the Circuit Breaker was open",
		}),
		AuthFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "frankenphp_websocket",
			Name:      "auth_failures_total",
			Help:      "Total number of failed auth requests (timeouts, 500s)",
		}),
	}

	if reg != nil {
		// Best effort registration.
		// If we encounter duplicates during a reload (though Caddy usually handles this),
		// we ignore the error so the module still functions.
		_ = reg.Register(m.Connections)
		_ = reg.Register(m.Messages)
		_ = reg.Register(m.Subscriptions)
		_ = reg.Register(m.AuthDuration)
		_ = reg.Register(m.BreakerTripped)
		_ = reg.Register(m.AuthFailures)
	}

	return m
}
