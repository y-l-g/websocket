package websocket

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type MockBroker struct{}

func (m *MockBroker) Publish(ctx context.Context, msg *BroadcastMessage) error { return nil }
func (m *MockBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	return make(chan *BroadcastMessage), nil
}
func (m *MockBroker) Close() error { return nil }

func TestHubSharding(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics(prometheus.NewRegistry())

	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000, 32, DefaultPingPeriod)

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
