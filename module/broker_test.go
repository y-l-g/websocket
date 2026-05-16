package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestMemoryBrokerPublishQueuesWhenCapacityAvailable(t *testing.T) {
	broker := NewMemoryBroker(zap.NewNop(), nil, 1)
	defer func() { _ = broker.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := broker.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := broker.Publish(ctx, &BroadcastMessage{Channel: "test", Event: "event"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	select {
	case <-stream:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for broker message")
	}
}

func TestMemoryBrokerPublishFailsWhenQueueFull(t *testing.T) {
	broker := NewMemoryBroker(zap.NewNop(), nil, 1)
	defer func() { _ = broker.Close() }()

	ctx := context.Background()
	if err := broker.Publish(ctx, &BroadcastMessage{Channel: "test", Event: "event"}); err != nil {
		t.Fatalf("First publish returned error: %v", err)
	}
	if err := broker.Publish(ctx, &BroadcastMessage{Channel: "test", Event: "event"}); !errors.Is(err, ErrBrokerQueueFull) {
		t.Fatalf("Expected ErrBrokerQueueFull, got %v", err)
	}
}

func TestMemoryBrokerPublishHonorsContextCancellation(t *testing.T) {
	broker := NewMemoryBroker(zap.NewNop(), nil)
	defer func() { _ = broker.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := broker.Publish(ctx, &BroadcastMessage{Channel: "test", Event: "event"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled, got %v", err)
	}
}
