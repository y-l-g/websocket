package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestMemoryBrokerPublishWaitsForSubscriber(t *testing.T) {
	broker := NewMemoryBroker(zap.NewNop(), nil)
	defer func() { _ = broker.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := broker.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- broker.Publish(ctx, &BroadcastMessage{Channel: "test", Event: "event"})
	}()

	select {
	case err := <-done:
		t.Fatalf("Publish returned before subscriber received message: %v", err)
	case <-time.After(5 * time.Millisecond):
	}

	select {
	case <-stream:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for broker message")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Publish returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish did not complete after subscriber received message")
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
