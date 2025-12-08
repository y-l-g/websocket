package websocket

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func newTestSubManager() *SubscriptionManager {
	return NewSubscriptionManager(
		zap.NewNop(),
		NewMetrics(prometheus.NewRegistry()),
		nil,
	)
}

func TestPresenceParsing(t *testing.T) {
	sm := newTestSubManager()

	client := &Client{
		ID:         "socket-id-1",
		send:       make(chan any, 10), // Updated
		PingPeriod: DefaultPingPeriod,
		WriteWait:  DefaultWriteWait,
		PongWait:   DefaultPongWait,
	}

	channel := "presence-test"

	authResp := map[string]string{
		"auth":         "signature",
		"channel_data": `{"user_id": "123", "user_info": {"name": "Alice"}}`,
	}
	authBytes, _ := json.Marshal(authResp)

	sm.Subscribe(client, channel, authBytes)

	if _, ok := sm.presence[channel]["123"]; !ok {
		t.Error("User 123 not added to presence map")
	}

	select {
	case msg := <-client.send:
		if bytesMsg, ok := msg.([]byte); ok {
			var parsed map[string]interface{}
			if err := json.Unmarshal(bytesMsg, &parsed); err != nil {
				t.Fatalf("Failed to parse subscription success message: %v", err)
			}
			if parsed["event"] != "pusher_internal:subscription_succeeded" {
				t.Errorf("Expected subscription_succeeded, got %v", parsed["event"])
			}
		}
	default:
		t.Error("No success message sent")
	}

	client2 := &Client{ID: "socket-id-2", send: make(chan any, 10)}
	badAuth := []byte(`{"auth":"sig"}`)

	sm.Subscribe(client2, channel, badAuth)

	if _, ok := sm.clients[client2][channel]; !ok {
		t.Error("Expected client2 to be in clients map even with invalid auth")
	}
}

func TestMemberTracking(t *testing.T) {
	sm := newTestSubManager()
	channel := "presence-room"

	c1 := &Client{ID: "c1", send: make(chan any, 10)}
	c2 := &Client{ID: "c2", send: make(chan any, 10)}

	authA := []byte(`{"channel_data": "{\"user_id\":\"A\"}"}`)
	sm.Subscribe(c1, channel, authA)
	<-c1.send

	authB := []byte(`{"channel_data": "{\"user_id\":\"B\"}"}`)
	sm.Subscribe(c2, channel, authB)

	select {
	case msg := <-c1.send:
		switch v := msg.(type) {
		case []byte:
			var parsed map[string]interface{}
			if err := json.Unmarshal(v, &parsed); err != nil {
				t.Fatalf("Failed to parse member_added message: %v", err)
			}
			if parsed["event"] != "pusher_internal:member_added" {
				t.Errorf("Expected member_added, got %v", parsed["event"])
			}
		default:
			t.Log("Received prepared message")
		}
	default:
		t.Error("C1 did not receive member_added event")
	}

	sm.Unsubscribe(c2, channel)

	select {
	case msg := <-c1.send:
		switch v := msg.(type) {
		case []byte:
			var parsed map[string]interface{}
			if err := json.Unmarshal(v, &parsed); err != nil {
				t.Fatalf("Failed to parse member_removed message: %v", err)
			}
			if parsed["event"] != "pusher_internal:member_removed" {
				t.Errorf("Expected member_removed, got %v", parsed["event"])
			}
		default:
			t.Log("Received prepared message")
		}
	default:
		t.Error("C1 did not receive member_removed event")
	}
}
