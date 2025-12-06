package websocket

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// Helper to create a dummy subscription manager
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
		send:       make(chan []byte, 10),
		PingPeriod: DefaultPingPeriod,
		WriteWait:  DefaultWriteWait,
		PongWait:   DefaultPongWait,
	}

	channel := "presence-test"

	// Case 1: Valid Auth Response
	authResp := map[string]string{
		"auth":         "signature",
		"channel_data": `{"user_id": "123", "user_info": {"name": "Alice"}}`,
	}
	authBytes, _ := json.Marshal(authResp)

	sm.Subscribe(client, channel, authBytes)

	// Check internal state
	if _, ok := sm.presence[channel]["123"]; !ok {
		t.Error("User 123 not added to presence map")
	}

	// Verify SubscriptionSucceeded message
	select {
	case msg := <-client.send:
		var parsed map[string]interface{}
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("Failed to parse subscription success message: %v", err)
		}
		if parsed["event"] != "pusher_internal:subscription_succeeded" {
			t.Errorf("Expected subscription_succeeded, got %v", parsed["event"])
		}
	default:
		t.Error("No success message sent")
	}

	// Case 2: Invalid Auth (Missing channel_data)
	client2 := &Client{ID: "socket-id-2", send: make(chan []byte, 10)}
	badAuth := []byte(`{"auth":"sig"}`) // No channel_data

	sm.Subscribe(client2, channel, badAuth)

	if _, ok := sm.clients[client2][channel]; !ok {
		t.Error("Expected client2 to be in clients map even with invalid auth")
	}

	for uid := range sm.presence[channel] {
		if uid == "" {
			t.Error("Empty User ID added to presence map")
		}
	}
}

func TestMemberTracking(t *testing.T) {
	sm := newTestSubManager()
	channel := "presence-room"

	c1 := &Client{ID: "c1", send: make(chan []byte, 10)}
	c2 := &Client{ID: "c2", send: make(chan []byte, 10)}

	// 1. Subscribe C1 (User A)
	authA := []byte(`{"channel_data": "{\"user_id\":\"A\"}"}`)
	sm.Subscribe(c1, channel, authA)
	// Drain C1 queue
	<-c1.send

	// 2. Subscribe C2 (User B)
	authB := []byte(`{"channel_data": "{\"user_id\":\"B\"}"}`)
	sm.Subscribe(c2, channel, authB)

	// C1 should receive member_added for B
	select {
	case msg := <-c1.send:
		var parsed map[string]interface{}
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("Failed to parse member_added message: %v", err)
		}
		if parsed["event"] != "pusher_internal:member_added" {
			t.Errorf("Expected member_added, got %v", parsed["event"])
		}
		dataStr := parsed["data"].(string)
		if !json.Valid([]byte(dataStr)) {
			t.Error("member_added data is not valid JSON string")
		}
	default:
		t.Error("C1 did not receive member_added event")
	}

	// 3. Unsubscribe C2
	sm.Unsubscribe(c2, channel)

	// C1 should receive member_removed for B
	select {
	case msg := <-c1.send:
		var parsed map[string]interface{}
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("Failed to parse member_removed message: %v", err)
		}
		if parsed["event"] != "pusher_internal:member_removed" {
			t.Errorf("Expected member_removed, got %v", parsed["event"])
		}
	default:
		t.Error("C1 did not receive member_removed event")
	}

	// Verify Presence Map cleanup
	if _, ok := sm.presence[channel]["B"]; ok {
		t.Error("User B still in presence map after unsubscribe")
	}
}
