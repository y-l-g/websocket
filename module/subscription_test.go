package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func newTestSubManager() *SubscriptionManager {
	return NewSubscriptionManager(
		zap.NewNop(),
		NewMetrics(prometheus.NewRegistry()),
		nil,
		DefaultFanoutBackpressureThreshold,
		DefaultFanoutBackpressureMaxWait,
		DefaultFanoutMode,
		DefaultFanoutRoundSize,
		DefaultFanoutRoundYield,
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

func TestBroadcastToChannelWaitsForQueuedClients(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil, DefaultFanoutBackpressureThreshold, DefaultFanoutBackpressureMaxWait, DefaultFanoutMode, DefaultFanoutRoundSize, DefaultFanoutRoundYield)
	client := &Client{ID: "c1", send: make(chan any, DefaultFanoutBackpressureThreshold+1)}
	for i := 0; i < DefaultFanoutBackpressureThreshold; i++ {
		client.send <- []byte("queued")
	}
	sm.channels["public-test"] = map[*Client]bool{client: true}

	go func() {
		time.Sleep(2 * time.Millisecond)
		for len(client.send) > 0 {
			<-client.send
		}
	}()

	start := time.Now()
	sm.BroadcastToChannel(&BroadcastMessage{
		Channel: "public-test",
		Event:   "bench.event",
		Data:    json.RawMessage(`{}`),
	})

	if elapsed := time.Since(start); elapsed < time.Millisecond {
		t.Fatalf("Expected broadcast to wait for client queue capacity, only waited %s", elapsed)
	}

	select {
	case msg := <-client.send:
		if _, ok := msg.(*websocket.PreparedMessage); !ok {
			t.Fatalf("Expected prepared broadcast message, got %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for broadcast message")
	}
}

func TestBroadcastToChannelGivesUpOnQueuedClients(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil, DefaultFanoutBackpressureThreshold, DefaultFanoutBackpressureMaxWait, DefaultFanoutMode, DefaultFanoutRoundSize, DefaultFanoutRoundYield)
	client := &Client{ID: "c1", send: make(chan any, DefaultFanoutBackpressureThreshold+1)}
	for i := 0; i < DefaultFanoutBackpressureThreshold; i++ {
		client.send <- []byte("queued")
	}
	sm.channels["public-test"] = map[*Client]bool{client: true}

	start := time.Now()
	sm.BroadcastToChannel(&BroadcastMessage{
		Channel: "public-test",
		Event:   "bench.event",
		Data:    json.RawMessage(`{}`),
	})

	elapsed := time.Since(start)
	if elapsed < DefaultFanoutBackpressureMaxWait {
		t.Fatalf("Expected broadcast to wait until max wait, only waited %s", elapsed)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Expected bounded wait, waited %s", elapsed)
	}

	if len(client.send) != DefaultFanoutBackpressureThreshold+1 {
		t.Fatalf("Expected broadcast to enqueue after bounded wait, queue depth is %d", len(client.send))
	}
}

func TestBroadcastToChannelHonorsConfiguredBackpressure(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil, 2, 3*time.Millisecond, DefaultFanoutMode, DefaultFanoutRoundSize, DefaultFanoutRoundYield)
	client := &Client{ID: "c1", send: make(chan any, 3)}
	for i := 0; i < 2; i++ {
		client.send <- []byte("queued")
	}
	sm.channels["public-test"] = map[*Client]bool{client: true}

	start := time.Now()
	sm.BroadcastToChannel(&BroadcastMessage{
		Channel: "public-test",
		Event:   "bench.event",
		Data:    json.RawMessage(`{}`),
	})

	elapsed := time.Since(start)
	if elapsed < 3*time.Millisecond {
		t.Fatalf("Expected broadcast to wait for configured max wait, only waited %s", elapsed)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Expected bounded wait, waited %s", elapsed)
	}
}

func TestBroadcastToChannelHonorsPacedFanout(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil, DefaultFanoutBackpressureThreshold, DefaultFanoutBackpressureMaxWait, fanoutModePaced, 2, time.Millisecond)
	clients := make(map[*Client]bool)
	for i := 0; i < 5; i++ {
		client := &Client{ID: "c", send: make(chan any, 1)}
		clients[client] = true
	}
	sm.channels["public-test"] = clients

	start := time.Now()
	sm.BroadcastToChannel(&BroadcastMessage{
		Channel: "public-test",
		Event:   "bench.event",
		Data:    json.RawMessage(`{}`),
	})

	if elapsed := time.Since(start); elapsed < 2*time.Millisecond {
		t.Fatalf("Expected paced fanout to yield between rounds, only took %s", elapsed)
	}
	for client := range clients {
		if len(client.send) != 1 {
			t.Fatalf("Expected client to receive one message, got queue depth %d", len(client.send))
		}
	}
}
