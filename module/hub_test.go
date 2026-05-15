package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

type MockBroker struct {
	published chan *BroadcastMessage
	delay     time.Duration
}

func (m *MockBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.published != nil {
		m.published <- msg
	}
	return nil
}
func (m *MockBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	return make(chan *BroadcastMessage), nil
}
func (m *MockBroker) Close() error { return nil }

func TestHubSharding(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())

	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000, 32, DefaultPingPeriod, DefaultDeliveryConfig())

	channel1 := "private-user.1"
	shard1 := hub.getShard(channel1)

	channel2 := "private-user.1"
	shard2 := hub.getShard(channel2)

	if shard1 != shard2 {
		t.Errorf("Sharding is not deterministic. Channel '%s' went to shard %d then %d", channel1, shard1.id, shard2.id)
	}

	distribution := make(map[int]int)
	for i := 0; i < 1000; i++ {
		channel := fmt.Sprintf("presence-room.%d", i)
		s := hub.getShard(channel)
		distribution[s.id]++
	}

	for i := 0; i < 32; i++ {
		count := distribution[i]
		if count == 0 {
			t.Logf("Warning: Shard %d has 0 channels (might be chance)", i)
		}
	}

	if len(distribution) < 16 {
		t.Errorf("Poor distribution: only used %d/32 shards", len(distribution))
	}
}

func TestHubPublishRecordsHotPathMetrics(t *testing.T) {
	t.Setenv("POGO_WS_HOT_PATH_METRICS", "true")

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	broker := &MockBroker{published: make(chan *BroadcastMessage, 1), delay: 5 * time.Millisecond}
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, broker, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	phpBroadcastAt := float64(time.Now().Add(-time.Millisecond).UnixNano()) / float64(time.Millisecond)
	data, err := json.Marshal(map[string]float64{"pogoPhpBroadcastAt": phpBroadcastAt})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if !hub.Publish("bench-channel", "bench.event", string(data)) {
		t.Fatal("Publish failed")
	}

	select {
	case msg := <-broker.published:
		if msg.InternalCreatedAt.IsZero() {
			t.Fatal("Expected internal message creation timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for broker publish")
	}

	if count := metricCount(t, metrics.PublishDuration.WithLabelValues("total")); count != 1 {
		t.Fatalf("Expected publish total count 1, got %d", count)
	}
	if count := metricCount(t, metrics.PublishDuration.WithLabelValues("broker")); count != 1 {
		t.Fatalf("Expected publish broker count 1, got %d", count)
	}
	totalSum := metricHistogramSum(t, metrics.PublishDuration.WithLabelValues("total"))
	brokerSum := metricHistogramSum(t, metrics.PublishDuration.WithLabelValues("broker"))
	if totalSum < brokerSum {
		t.Fatalf("Expected publish total duration %.9f to be >= broker duration %.9f", totalSum, brokerSum)
	}
	if count := metricCount(t, metrics.PhpToGoEntryDelay); count != 1 {
		t.Fatalf("Expected php-to-go entry count 1, got %d", count)
	}
}

func TestBroadcastMessageInternalTimestampsAreNotSerialized(t *testing.T) {
	msg := &BroadcastMessage{
		Channel:           "bench-channel",
		Event:             "bench.event",
		Data:              json.RawMessage(`{"sentAt":1}`),
		InternalCreatedAt: time.Now(),
		BrokerReceivedAt:  time.Now(),
		ShardBroadcastAt:  time.Now(),
	}

	data, err := SerializeBroadcast(msg)
	if err != nil {
		t.Fatalf("SerializeBroadcast failed: %v", err)
	}

	var serialized map[string]json.RawMessage
	if err := json.Unmarshal(data, &serialized); err != nil {
		t.Fatalf("Unmarshal serialized message failed: %v", err)
	}

	for _, field := range []string{"InternalCreatedAt", "BrokerReceivedAt", "ShardBroadcastAt"} {
		if _, ok := serialized[field]; ok {
			t.Fatalf("Internal field %s appeared in serialized message: %s", field, data)
		}
	}
}

func TestHubAndShardDelayMetricsRecordNonNegativeDurations(t *testing.T) {
	t.Setenv("POGO_WS_HOT_PATH_METRICS", "true")

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	shard := NewHubShard(0, logger, ctx, metrics, nil, DefaultFanoutBackpressureThreshold, DefaultFanoutBackpressureMaxWait)
	go shard.Run()

	msg := &BroadcastMessage{
		Channel:           "bench-channel",
		Event:             "bench.event",
		Data:              json.RawMessage(`{}`),
		InternalCreatedAt: time.Now().Add(-time.Millisecond),
		BrokerReceivedAt:  time.Now().Add(-time.Millisecond),
	}

	now := time.Now()
	delay := now.Sub(msg.InternalCreatedAt)
	if delay < 0 {
		delay = 0
	}
	metrics.BrokerToHubDelay.Observe(delay.Seconds())

	select {
	case shard.broadcast <- msg:
	case <-time.After(time.Second):
		t.Fatal("Timed out sending broadcast to shard")
	}

	deadline := time.Now().Add(time.Second)
	for metricCount(t, metrics.HubToShardDelay) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Timed out waiting for hub-to-shard metric")
		}
		time.Sleep(time.Millisecond)
	}

	if count := metricCount(t, metrics.BrokerToHubDelay); count != 1 {
		t.Fatalf("Expected broker-to-hub count 1, got %d", count)
	}
}

func metricCount(t *testing.T, metric prometheus.Observer) uint64 {
	t.Helper()

	promMetric, ok := metric.(prometheus.Metric)
	if !ok {
		t.Fatalf("Metric does not implement prometheus.Metric: %T", metric)
	}

	var dtoMetric dto.Metric
	if err := promMetric.Write(&dtoMetric); err != nil {
		t.Fatalf("Metric write failed: %v", err)
	}

	if histogram := dtoMetric.GetHistogram(); histogram != nil {
		return histogram.GetSampleCount()
	}
	if counter := dtoMetric.GetCounter(); counter != nil {
		return uint64(counter.GetValue())
	}

	t.Fatalf("Unsupported metric type: %T", metric)
	return 0
}

func metricHistogramSum(t *testing.T, metric prometheus.Observer) float64 {
	t.Helper()

	promMetric, ok := metric.(prometheus.Metric)
	if !ok {
		t.Fatalf("Metric does not implement prometheus.Metric: %T", metric)
	}

	var dtoMetric dto.Metric
	if err := promMetric.Write(&dtoMetric); err != nil {
		t.Fatalf("Metric write failed: %v", err)
	}

	if histogram := dtoMetric.GetHistogram(); histogram != nil {
		return histogram.GetSampleSum()
	}

	t.Fatalf("Metric is not a histogram: %T", metric)
	return 0
}

func TestClientShardMask(t *testing.T) {
	c := &Client{
		ID:         "test-client",
		PingPeriod: DefaultPingPeriod,
		WriteWait:  DefaultWriteWait,
		PongWait:   DefaultPongWait,
	}

	if c.HasShard(0) {
		t.Error("Client should not have shard 0 set initially")
	}

	c.AddShard(5)
	if !c.HasShard(5) {
		t.Error("Client should have shard 5 set")
	}
	if c.HasShard(0) {
		t.Error("Client should not have shard 0 set")
	}

	c.AddShard(63)
	if !c.HasShard(63) {
		t.Error("Client should have shard 63 set")
	}

	c.AddShard(64)
	c.AddShard(-1)

	if !c.HasShard(5) || !c.HasShard(63) {
		t.Error("Bitmask lost state")
	}
}
