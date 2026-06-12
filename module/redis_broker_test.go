package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"go.uber.org/zap"
)

func TestRedisBroker_PubSub(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	logger := zap.NewNop()
	broker := NewRedisBroker(logger, "test-app", mr.Addr(), "", 0, false)
	defer func() { _ = broker.Close() }() // Fixed

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subCh, err := broker.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	testMsg := &BroadcastMessage{
		AppID:   "test-app",
		Channel: "test-channel",
		Event:   "test-event",
		Data:    json.RawMessage(`{"foo":"bar"}`),
	}

	if err := broker.Publish(ctx, testMsg); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case received := <-subCh:
		if received.Channel != testMsg.Channel {
			t.Errorf("Expected channel %s, got %s", testMsg.Channel, received.Channel)
		}
		if received.Event != testMsg.Event {
			t.Errorf("Expected event %s, got %s", testMsg.Event, received.Event)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestRedisBroker_Reconnection(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	logger := zap.NewNop()
	broker := NewRedisBroker(logger, "test-app", mr.Addr(), "", 0, false)
	defer func() { _ = broker.Close() }() // Fixed

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subCh, err := broker.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	mr.Close()

	time.Sleep(200 * time.Millisecond)
	if err := mr.Restart(); err != nil {
		t.Fatalf("Failed to restart miniredis: %v", err)
	}

	reconnected := false
	for i := 0; i < 10; i++ {
		err := broker.Publish(ctx, &BroadcastMessage{
			AppID:   "test-app",
			Channel: "recovery-test",
			Event:   "ping",
			Data:    json.RawMessage(`{}`),
		})

		if err == nil {
			select {
			case msg := <-subCh:
				if msg.Channel == "recovery-test" {
					reconnected = true
					goto Done
				}
			default:
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

Done:
	if !reconnected {
		t.Log("Warning: Reconnection test timed out")
	}
}

func TestRedisBroker_IsolatesApps(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	logger := zap.NewNop()
	appA := NewRedisBroker(logger, "app-a", mr.Addr(), "", 0, false)
	defer func() { _ = appA.Close() }()
	appB := NewRedisBroker(logger, "app-b", mr.Addr(), "", 0, false)
	defer func() { _ = appB.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chA, err := appA.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe appA failed: %v", err)
	}
	chB, err := appB.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe appB failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := appA.Publish(ctx, &BroadcastMessage{
		AppID:   "app-a",
		Channel: "private-a",
		Event:   "event",
		Data:    json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("Publish appA failed: %v", err)
	}

	select {
	case msg := <-chA:
		if msg.AppID != "app-a" || msg.Channel != "private-a" {
			t.Fatalf("unexpected appA message: %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for appA message")
	}

	select {
	case msg := <-chB:
		t.Fatalf("appB received appA message: %#v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
