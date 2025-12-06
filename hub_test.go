package websocket

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// MockBroker implementation for testing
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
	// Update: Pass maxConnections (10000) to NewHub
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{}, 10000)

	// Test case: Ensure the hashing algorithm is deterministic for CHANNELS
	channel1 := "private-user.1"
	shard1 := hub.getShard(channel1)

	channel2 := "private-user.1"
	shard2 := hub.getShard(channel2)

	if shard1 != shard2 {
		t.Errorf("Sharding is not deterministic. Channel '%s' went to shard %d then %d", channel1, shard1.id, shard2.id)
	}

	// Distribution Test
	distribution := make(map[int]int)
	for i := 0; i < 1000; i++ {
		// Use channel names
		channel := fmt.Sprintf("presence-room.%d", i)
		s := hub.getShard(channel)
		distribution[s.id]++
	}

	for i := 0; i < NumShards; i++ {
		count := distribution[i]
		if count == 0 {
			t.Logf("Warning: Shard %d has 0 channels (might be chance)", i)
		}
	}

	if len(distribution) < NumShards/2 {
		t.Errorf("Poor distribution: only used %d/%d shards", len(distribution), NumShards)
	}
}

func TestClientShardMask(t *testing.T) {
	c := &Client{
		ID:         "test-client",
		PingPeriod: DefaultPingPeriod,
		WriteWait:  DefaultWriteWait,
		PongWait:   DefaultPongWait,
	}

	// 1. Initially empty
	if c.HasShard(0) {
		t.Error("Client should not have shard 0 set initially")
	}

	// 2. Set Shard 5
	c.AddShard(5)
	if !c.HasShard(5) {
		t.Error("Client should have shard 5 set")
	}
	if c.HasShard(0) {
		t.Error("Client should not have shard 0 set")
	}

	// 3. Set Shard 31 (Max)
	c.AddShard(31)
	if !c.HasShard(31) {
		t.Error("Client should have shard 31 set")
	}

	// 4. Test Out of Bounds (Should be ignored)
	c.AddShard(32)
	c.AddShard(-1)

	// 5. Verify persistence
	if !c.HasShard(5) || !c.HasShard(31) {
		t.Error("Bitmask lost state")
	}
}
