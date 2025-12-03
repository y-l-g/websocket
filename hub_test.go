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
	hub := NewHub("test-app", logger, ctx, metrics, nil, nil, &MockBroker{})

	// Test case: Ensure the hashing algorithm is deterministic
	key1 := "private-user.1"
	shard1 := hub.getShard(key1)

	key2 := "private-user.1"
	shard2 := hub.getShard(key2)

	if shard1 != shard2 {
		t.Errorf("Sharding is not deterministic. Key '%s' went to shard %d then %d", key1, shard1.id, shard2.id)
	}

	key3 := "private-user.2"
	shard3 := hub.getShard(key3)

	// This check is probabilistic but likely to be true for different keys
	if shard1 == shard3 && shard1.id == 0 {
		// It's possible they collide, but if EVERYTHING goes to 0, that's a bug.
		// Let's check distribution over many keys.
	}

	// Distribution Test
	distribution := make(map[int]int)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("channel.%d", i)
		s := hub.getShard(key)
		distribution[s.id]++
	}

	// With 32 shards and 1000 keys, each should have ~31 keys.
	// We just want to ensure we don't have empty shards or one overloaded shard.
	for i := 0; i < NumShards; i++ {
		count := distribution[i]
		if count == 0 {
			t.Logf("Warning: Shard %d has 0 keys (might be chance)", i)
		}
	}

	if len(distribution) < NumShards/2 {
		t.Errorf("Poor distribution: only used %d/%d shards", len(distribution), NumShards)
	}
}
