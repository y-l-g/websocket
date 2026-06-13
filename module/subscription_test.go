package websocket

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

func newTestSubManager() *SubscriptionManager {
	return NewSubscriptionManager(
		zap.NewNop(),
		NewMetrics(prometheus.NewRegistry()),
		nil,
	)
}

func drainClientMessage(t *testing.T, client *Client) {
	t.Helper()

	select {
	case <-client.send:
	default:
		t.Fatal("Expected client message")
	}
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

	if chans, ok := sm.clients[client2]; ok && chans[channel] {
		t.Error("Expected client2 not to be subscribed with invalid auth")
	}
	if sm.channels[channel][client2] {
		t.Error("Expected client2 not to be in channel map with invalid auth")
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

func TestPresenceStateRemovedAfterLastUnsubscribe(t *testing.T) {
	sm := newTestSubManager()
	channel := "presence-room"
	client := &Client{ID: "c1", send: make(chan any, 10)}
	auth := []byte(`{"channel_data": "{\"user_id\":\"A\"}"}`)

	if !sm.Subscribe(client, channel, auth) {
		t.Fatal("Expected presence subscribe to succeed")
	}
	drainClientMessage(t, client)

	sm.Unsubscribe(client, channel)

	if _, ok := sm.channels[channel]; ok {
		t.Fatal("Expected channel subscription map to be removed")
	}
	if _, ok := sm.presence[channel]; ok {
		t.Fatal("Expected presence map to be removed")
	}
	if _, ok := sm.clientToUser[channel]; ok {
		t.Fatal("Expected client-to-user map to be removed")
	}
	if chans, ok := sm.clients[client]; ok && chans[channel] {
		t.Fatal("Expected client channel subscription to be removed")
	}
}

func TestPresenceSameUserStateRemainsUntilLastConnectionLeaves(t *testing.T) {
	sm := newTestSubManager()
	channel := "presence-room"
	c1 := &Client{ID: "c1", send: make(chan any, 10)}
	c2 := &Client{ID: "c2", send: make(chan any, 10)}
	auth := []byte(`{"channel_data": "{\"user_id\":\"A\"}"}`)

	if !sm.Subscribe(c1, channel, auth) {
		t.Fatal("Expected first presence subscribe to succeed")
	}
	drainClientMessage(t, c1)
	if !sm.Subscribe(c2, channel, auth) {
		t.Fatal("Expected second presence subscribe to succeed")
	}
	drainClientMessage(t, c2)

	sm.Unsubscribe(c1, channel)

	if _, ok := sm.presence[channel]["A"]; !ok {
		t.Fatal("Expected user A to remain present after first connection leaves")
	}
	if clientMap, ok := sm.clientToUser[channel]; !ok || len(clientMap) != 1 || clientMap[c2] != "A" {
		t.Fatalf("Expected only second connection to remain in client-to-user map, got %#v", clientMap)
	}

	sm.Unsubscribe(c2, channel)

	if _, ok := sm.presence[channel]; ok {
		t.Fatal("Expected presence map to be removed after last connection leaves")
	}
	if _, ok := sm.clientToUser[channel]; ok {
		t.Fatal("Expected client-to-user map to be removed after last connection leaves")
	}
}

func TestBroadcastToChannelFanoutSendsToAllClients(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil)
	clients := make(map[*Client]bool)
	for i := 0; i < 5; i++ {
		client := &Client{ID: "c", send: make(chan any, 1)}
		clients[client] = true
	}
	sm.channels["public-test"] = clients

	sm.BroadcastToChannel(&BroadcastMessage{
		Channel: "public-test",
		Event:   "bench.event",
		Data:    json.RawMessage(`{}`),
	})

	for client := range clients {
		if len(client.send) != 1 {
			t.Fatalf("Expected client to receive one message, got queue depth %d", len(client.send))
		}
	}
}

func TestSubscriptionGaugeTracksActiveSubscriptions(t *testing.T) {
	metrics := NewMetrics(prometheus.NewRegistry())
	sm := NewSubscriptionManager(zap.NewNop(), metrics, nil)
	client := &Client{ID: "c1", send: make(chan any, 4)}

	sm.Subscribe(client, "public-test", nil)
	if got := gaugeValue(t, metrics.Subscriptions); got != 1 {
		t.Fatalf("active subscriptions = %v, want 1", got)
	}

	sm.Subscribe(client, "public-test", nil)
	if got := gaugeValue(t, metrics.Subscriptions); got != 1 {
		t.Fatalf("active subscriptions after duplicate subscribe = %v, want 1", got)
	}

	sm.Unsubscribe(client, "public-test")
	if got := gaugeValue(t, metrics.Subscriptions); got != 0 {
		t.Fatalf("active subscriptions after unsubscribe = %v, want 0", got)
	}
}

func gaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()

	var metric dto.Metric
	if err := gauge.Write(&metric); err != nil {
		t.Fatalf("Gauge Write failed: %v", err)
	}
	return metric.GetGauge().GetValue()
}
