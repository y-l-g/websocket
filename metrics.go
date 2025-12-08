package websocket

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Connections     prometheus.Gauge
	Messages        prometheus.Counter
	Subscriptions   prometheus.Counter
	AuthDuration    prometheus.Histogram
	BreakerTripped  prometheus.Counter
	AuthFailures    *prometheus.CounterVec
	DroppedMessages prometheus.Counter
	BrokerDropped   prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "pogo_websocket",
			Name:      "connections_active",
			Help:      "Current number of active WebSocket connections",
		}),
		Messages: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "messages_total",
			Help:      "Total number of messages published to the Hub",
		}),
		Subscriptions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "subscriptions_total",
			Help:      "Total number of channel subscriptions",
		}),
		AuthDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "auth_duration_seconds",
			Help:      "Duration of PHP Worker authentication requests",
			Buckets:   prometheus.DefBuckets,
		}),
		BreakerTripped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "circuit_breaker_open_total",
			Help:      "Total number of auth requests rejected because the Circuit Breaker was open",
		}),
		AuthFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "auth_failures_total",
			Help:      "Total number of failed auth requests",
		}, []string{"reason"}),
		DroppedMessages: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "client_dropped_messages_total",
			Help:      "Number of messages dropped due to slow client consumers",
		}),
		BrokerDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "broker_dropped_messages_total",
			Help:      "Number of messages dropped by the internal broker due to backpressure",
		}),
	}

	if reg != nil {
		_ = reg.Register(m.Connections)
		_ = reg.Register(m.Messages)
		_ = reg.Register(m.Subscriptions)
		_ = reg.Register(m.AuthDuration)
		_ = reg.Register(m.BreakerTripped)
		_ = reg.Register(m.AuthFailures)
		_ = reg.Register(m.DroppedMessages)
		_ = reg.Register(m.BrokerDropped)
	}

	return m
}
