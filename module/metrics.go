package websocket

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Connections          prometheus.Gauge
	Messages             prometheus.Counter
	Subscriptions        prometheus.Counter
	AuthDuration         prometheus.Histogram
	BreakerTripped       prometheus.Counter
	AuthFailures         *prometheus.CounterVec
	DroppedMessages      *prometheus.CounterVec
	BrokerDropped        *prometheus.CounterVec
	PublishFailures      *prometheus.CounterVec
	WebhookQueueDepth    prometheus.Gauge
	WebhookDropped       *prometheus.CounterVec
	PublishDuration      *prometheus.HistogramVec
	BrokerToHubDelay     prometheus.Histogram
	HubToShardDelay      prometheus.Histogram
	FanoutSubscribers    prometheus.Histogram
	ClientQueueDepth     prometheus.Histogram
	ClientQueueResidence prometheus.Histogram
	WriteDuration        *prometheus.HistogramVec
	WriteTotalDuration   *prometheus.HistogramVec
	WriteFailures        *prometheus.CounterVec
	DeliveryConfig       *prometheus.GaugeVec
	HotPathEnabled       bool
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
		DroppedMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "client_dropped_messages_total",
			Help:      "Number of messages dropped due to slow client consumers",
		}, []string{"app_id", "reason", "kind"}),
		BrokerDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "broker_dropped_messages_total",
			Help:      "Number of messages dropped by the internal broker due to backpressure",
		}, []string{"app_id", "reason"}),
		PublishFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "publish_failures_total",
			Help:      "Number of failed publish attempts by reason",
		}, []string{"app_id", "reason"}),
		WebhookQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "pogo_websocket",
			Name:      "webhook_queue_depth",
			Help:      "Current number of queued webhook notifications",
		}),
		WebhookDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "webhook_dropped_total",
			Help:      "Number of webhook notifications dropped by reason",
		}, []string{"reason"}),
		PublishDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "publish_duration_seconds",
			Help:      "Duration spent publishing one message by Hub.Publish phase",
			Buckets:   prometheus.ExponentialBuckets(0.00001, 2, 18),
		}, []string{"phase"}),
		BrokerToHubDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "broker_to_hub_delay_seconds",
			Help:      "Time from Hub.Publish message creation to broker stream delivery",
			Buckets:   prometheus.ExponentialBuckets(0.00001, 2, 18),
		}),
		HubToShardDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "hub_to_shard_delay_seconds",
			Help:      "Time from hub broker receive to shard broadcast handling",
			Buckets:   prometheus.ExponentialBuckets(0.00001, 2, 18),
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
		ClientQueueResidence: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "client_queue_residence_seconds",
			Help:      "Time outbound messages spend queued before the client write loop handles them",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 16),
		}),
		WriteDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "write_duration_seconds",
			Help:      "Duration spent writing websocket messages by message kind",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 16),
		}, []string{"kind"}),
		WriteTotalDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pogo_websocket",
			Name:      "write_total_duration_seconds",
			Help:      "Duration spent setting write deadline and writing websocket messages by message kind",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 16),
		}, []string{"kind"}),
		WriteFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pogo_websocket",
			Name:      "write_failures_total",
			Help:      "Total websocket write failures by message kind",
		}, []string{"kind"}),
		DeliveryConfig: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "pogo_websocket",
			Name:      "delivery_config",
			Help:      "Effective Pogo websocket delivery tuning configuration by key",
		}, []string{"key"}),
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
		_ = reg.Register(m.PublishFailures)
		_ = reg.Register(m.WebhookQueueDepth)
		_ = reg.Register(m.WebhookDropped)
		_ = reg.Register(m.PublishDuration)
		_ = reg.Register(m.BrokerToHubDelay)
		_ = reg.Register(m.HubToShardDelay)
		_ = reg.Register(m.FanoutSubscribers)
		_ = reg.Register(m.ClientQueueDepth)
		_ = reg.Register(m.ClientQueueResidence)
		_ = reg.Register(m.WriteDuration)
		_ = reg.Register(m.WriteTotalDuration)
		_ = reg.Register(m.WriteFailures)
		_ = reg.Register(m.DeliveryConfig)
	}

	return m
}

func (m *Metrics) SetDeliveryConfig(config DeliveryConfig) {
	if m == nil || m.DeliveryConfig == nil {
		return
	}
	m.DeliveryConfig.WithLabelValues("outbound_queue_size").Set(float64(config.OutboundQueueSize))
	m.DeliveryConfig.WithLabelValues("write_burst_size").Set(float64(config.WriteBurstSize))
	m.DeliveryConfig.WithLabelValues("broker_queue_size").Set(float64(config.BrokerQueueSize))
	m.DeliveryConfig.WithLabelValues("shard_queue_size").Set(float64(config.ShardQueueSize))
	if config.EnableCompression {
		m.DeliveryConfig.WithLabelValues("enable_compression").Set(1)
	} else {
		m.DeliveryConfig.WithLabelValues("enable_compression").Set(0)
	}
}

func hotPathMetricsEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("POGO_WS_HOT_PATH_METRICS")))
	return value == "1" || value == "true" || value == "on"
}
