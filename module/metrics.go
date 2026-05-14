package websocket

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Connections       prometheus.Gauge
	Messages          prometheus.Counter
	Subscriptions     prometheus.Counter
	AuthDuration      prometheus.Histogram
	BreakerTripped    prometheus.Counter
	AuthFailures      *prometheus.CounterVec
	DroppedMessages   prometheus.Counter
	BrokerDropped     prometheus.Counter
	FanoutDuration    prometheus.Histogram
	FanoutSubscribers prometheus.Histogram
	ClientQueueDepth  prometheus.Histogram
	WriteDuration     *prometheus.HistogramVec
	WriteFailures     *prometheus.CounterVec
	HotPathEnabled    bool
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HotPathEnabled: hotPathMetricsEnabled(),
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
		FanoutDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "fanout_duration_seconds",
			Help:      "Duration spent enqueueing one broadcast to subscribed clients",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 16),
		}),
		FanoutSubscribers: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "fanout_subscribers",
			Help:      "Number of subscribers targeted by each broadcast fanout",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		}),
		ClientQueueDepth: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "client_queue_depth",
			Help:      "Client outbound queue depth sampled when messages are enqueued",
			Buckets:   []float64{0, 1, 2, 4, 8, 16, 32, 64, 128, 192, 256},
		}),
		WriteDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "write_duration_seconds",
			Help:      "Duration spent writing websocket messages by message kind",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 16),
		}, []string{"kind"}),
		WriteFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "write_failures_total",
			Help:      "Total websocket write failures by message kind",
		}, []string{"kind"}),
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
		_ = reg.Register(m.FanoutDuration)
		_ = reg.Register(m.FanoutSubscribers)
		_ = reg.Register(m.ClientQueueDepth)
		_ = reg.Register(m.WriteDuration)
		_ = reg.Register(m.WriteFailures)
	}

	return m
}

func hotPathMetricsEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("POGO_WS_HOT_PATH_METRICS")))
	return value == "1" || value == "true" || value == "on"
}
