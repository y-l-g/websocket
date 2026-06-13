package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/y-l-g/websocket/module/internal/protocol"
	"go.uber.org/zap"
)

type MockBroker struct {
	published chan *BroadcastMessage
	delay     time.Duration
	err       error
	scope     string
}

func (m *MockBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return m.err
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
func (m *MockBroker) PublishScope() string {
	if m.scope != "" {
		return m.scope
	}
	return fmt.Sprintf("mock:%p", m)
}

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

type SubscribeFailBroker struct{}

func (m *SubscribeFailBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	return nil
}
func (m *SubscribeFailBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	return nil, errors.New("subscribe failed")
}
func (m *SubscribeFailBroker) Close() error { return nil }

func TestHubPublishRecordsHotPathMetrics(t *testing.T) {
	t.Setenv("POGO_WS_HOT_PATH_METRICS", "true")

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	broker := &MockBroker{published: make(chan *BroadcastMessage, 1), delay: 5 * time.Millisecond}
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, broker, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	data, err := json.Marshal(map[string]float64{"sentAt": 1})
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
}

func TestHubSubscribeFailureMarksUnhealthy(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &SubscribeFailBroker{}, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	go hub.Run()
	hub.Wait()

	if hub.IsHealthy() {
		t.Fatal("Expected hub to be unhealthy after broker subscribe failure")
	}
	if hub.HealthError() != "broker_subscribe_failed" {
		t.Fatalf("HealthError = %q, want broker_subscribe_failed", hub.HealthError())
	}
}

func TestHubShutdownTimeoutBoundsWait(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())

	metrics := NewMetrics(prometheus.NewRegistry())
	delivery := DefaultDeliveryConfig()
	delivery.ShutdownTimeout = 10 * time.Millisecond
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000, 1, DefaultPingPeriod, delivery)
	go hub.Run()

	client := &Client{
		ID:     "stuck-client",
		hub:    hub,
		conn:   NewMockWSConnection(),
		send:   make(chan any, 1),
		ctx:    ctx,
		cancel: cancel,
	}
	if !hub.Register(client) {
		t.Fatal("Expected client registration to succeed")
	}

	start := time.Now()
	cancel()
	hub.Wait()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Shutdown wait was not bounded, elapsed=%s", elapsed)
	}
}

func TestHubRegisterEnforcesMaxConnectionsConcurrently(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	metrics := NewMetrics(prometheus.NewRegistry())
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 1, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	const totalClients = 20
	type registration struct {
		client   *Client
		conn     *MockWSConnection
		accepted bool
	}

	registrations := make([]registration, totalClients)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < totalClients; i++ {
		clientCtx, clientCancel := context.WithCancel(ctx)
		t.Cleanup(clientCancel)

		conn := NewMockWSConnection()
		client := &Client{
			ID:     fmt.Sprintf("client-%02d", i),
			hub:    hub,
			conn:   conn,
			send:   make(chan any, 1),
			ctx:    clientCtx,
			cancel: clientCancel,
		}
		registrations[i].client = client
		registrations[i].conn = conn

		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			registrations[i].accepted = hub.Register(registrations[i].client)
		}(i)
	}

	close(start)
	wg.Wait()

	acceptedCount := 0
	var acceptedClient *Client
	for i, registration := range registrations {
		if registration.accepted {
			acceptedCount++
			acceptedClient = registration.client
			continue
		}
		if !registration.conn.CloseCalled {
			t.Fatalf("rejected client %d connection was not closed", i)
		}
	}

	if acceptedCount != 1 {
		t.Fatalf("accepted %d clients, want 1", acceptedCount)
	}
	if got := hub.conns.Load(); got != 1 {
		t.Fatalf("hub.conns = %d, want 1", got)
	}
	if ids := hub.ConnectionIDs(); len(ids) != 1 {
		t.Fatalf("ConnectionIDs length = %d, want 1: %v", len(ids), ids)
	}

	hub.Unregister(acceptedClient)
	if got := hub.conns.Load(); got != 0 {
		t.Fatalf("hub.conns after unregister = %d, want 0", got)
	}
}

func TestHubRegistryKeepsMultipleActiveHubsPerApp(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	hub1 := NewHub("reload-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	hub2 := NewHub("reload-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	t.Cleanup(func() {
		UnregisterHub("reload-app", hub1)
		UnregisterHub("reload-app", hub2)
	})

	if err := RegisterHub("reload-app", hub1); err != nil {
		t.Fatalf("RegisterHub hub1 failed: %v", err)
	}
	if err := RegisterHub("reload-app", hub2); err != nil {
		t.Fatalf("RegisterHub hub2 failed: %v", err)
	}

	hubs := GetHubs("reload-app")
	if len(hubs) != 2 {
		t.Fatalf("GetHubs returned %d hubs, want 2", len(hubs))
	}

	UnregisterHub("reload-app", hub1)
	hubs = GetHubs("reload-app")
	if len(hubs) != 1 || hubs[0] != hub2 {
		t.Fatalf("UnregisterHub removed wrong hub; got %#v", hubs)
	}
}

func TestPublishToActiveHubsPublishesAllDistinctScopes(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	broker1 := &MockBroker{published: make(chan *BroadcastMessage, 1), scope: "scope-1"}
	broker2 := &MockBroker{published: make(chan *BroadcastMessage, 1), scope: "scope-2"}
	hub1 := NewHub("reload-app", logger, ctx, metrics, nil, nil, broker1, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	hub2 := NewHub("reload-app", logger, ctx, metrics, nil, nil, broker2, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	if status := publishToActiveHubs([]*Hub{hub1, hub2}, "test-channel", "test-event", `{"ok":true}`); status != PublishOK {
		t.Fatalf("publishToActiveHubs status = %d, want %d", status, PublishOK)
	}

	for name, ch := range map[string]<-chan *BroadcastMessage{"broker1": broker1.published, "broker2": broker2.published} {
		select {
		case msg := <-ch:
			if msg.Channel != "test-channel" {
				t.Fatalf("%s received channel %q", name, msg.Channel)
			}
		case <-time.After(time.Second):
			t.Fatalf("Timed out waiting for %s publish", name)
		}
	}
}

func TestPublishToActiveHubsDeduplicatesSharedScopes(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	broker1 := &MockBroker{published: make(chan *BroadcastMessage, 1), scope: "redis-scope"}
	broker2 := &MockBroker{published: make(chan *BroadcastMessage, 1), scope: "redis-scope"}
	hub1 := NewHub("reload-app", logger, ctx, metrics, nil, nil, broker1, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	hub2 := NewHub("reload-app", logger, ctx, metrics, nil, nil, broker2, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	if status := publishToActiveHubs([]*Hub{hub1, hub2}, "test-channel", "test-event", `{"ok":true}`); status != PublishOK {
		t.Fatalf("publishToActiveHubs status = %d, want %d", status, PublishOK)
	}

	select {
	case <-broker1.published:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for first scoped publish")
	}
	select {
	case <-broker2.published:
		t.Fatal("Expected shared scope to be published once")
	default:
	}
}

func TestHubPublishStatusCodes(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{err: errors.New("boom")}, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())

	tests := []struct {
		name    string
		channel string
		event   string
		data    string
		want    PublishStatus
	}{
		{name: "channel too long", channel: strings.Repeat("c", protocol.MaxChannelLength+1), event: "event", data: `{}`, want: PublishChannelTooLong},
		{name: "event too long", channel: "channel", event: strings.Repeat("e", protocol.MaxEventLength+1), data: `{}`, want: PublishEventTooLong},
		{name: "payload too large", channel: "channel", event: "event", data: `"` + strings.Repeat("d", protocol.MaxDataSize+1) + `"`, want: PublishPayloadTooLarge},
		{name: "invalid json", channel: "channel", event: "event", data: `{`, want: PublishInvalidPayloadJSON},
		{name: "broker failed", channel: "channel", event: "event", data: `{}`, want: PublishBrokerFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hub.publish(tt.channel, tt.event, tt.data); got != tt.want {
				t.Fatalf("publish status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHubPublishStatusBrokerQueueFull(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	broker := NewMemoryBroker(logger, metrics, 1)
	defer func() { _ = broker.Close() }()

	if err := broker.Publish(ctx, &BroadcastMessage{Channel: "held", Event: "event"}); err != nil {
		t.Fatalf("Seed publish failed: %v", err)
	}

	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, broker, 10000, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	if got := hub.publish("channel", "event", `{}`); got != PublishBrokerQueueFull {
		t.Fatalf("publish status = %d, want %d", got, PublishBrokerQueueFull)
	}
}

func TestHubPublishStatusShardQueueFull(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())
	delivery := DefaultDeliveryConfig()
	delivery.ShardQueueSize = 1
	broker := NewMemoryBroker(logger, metrics, 2)
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, broker, 10000, 1, DefaultPingPeriod, delivery)
	hub.shards[0] = NewHubShard(0, "test-app", logger, ctx, metrics, nil, 1)
	hub.shards[0].broadcast <- &BroadcastMessage{Channel: "held", Event: "event"}

	go hub.Run()

	if got := hub.publish("channel", "event", `{}`); got != PublishShardQueueFull {
		t.Fatalf("publish status = %d, want %d", got, PublishShardQueueFull)
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
	shard := NewHubShard(0, "test-app", logger, ctx, metrics, nil, DefaultShardQueueSize)
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
