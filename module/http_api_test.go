package websocket

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func TestPusherAPIEventPublishesWithSocketExclusion(t *testing.T) {
	module, broker, cleanup := newHTTPAPITestModule(t)
	defer cleanup()

	body := []byte(`{"name":"order.created","data":"{\"id\":1}","channel":"private-orders","socket_id":"1.1"}`)
	req := httptest.NewRequest(http.MethodPost, signedPusherURL("/apps/test-app/events", body, "test-key", "secret"), bytes.NewReader(body))
	rr := httptest.NewRecorder()

	module.servePusherAPI(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	select {
	case msg := <-broker.published:
		if msg.Channel != "private-orders" {
			t.Fatalf("Channel = %q, want private-orders", msg.Channel)
		}
		if msg.Event != "order.created" {
			t.Fatalf("Event = %q, want order.created", msg.Event)
		}
		if msg.ExceptSocketID != "1.1" {
			t.Fatalf("ExceptSocketID = %q, want 1.1", msg.ExceptSocketID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for publish")
	}
}

func TestPusherAPIBatchPublishesItems(t *testing.T) {
	module, broker, cleanup := newHTTPAPITestModule(t)
	defer cleanup()

	body := []byte(`{"batch":[{"name":"one","data":"{\"n\":1}","channel":"public-a"},{"name":"two","data":"{\"n\":2}","channel":"public-b","socket_id":"2.2"}]}`)
	req := httptest.NewRequest(http.MethodPost, signedPusherURL("/apps/test-app/batch_events", body, "test-key", "secret"), bytes.NewReader(body))
	rr := httptest.NewRecorder()

	module.servePusherAPI(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	first := readPublishedMessage(t, broker.published)
	second := readPublishedMessage(t, broker.published)
	if first.Channel != "public-a" || second.Channel != "public-b" {
		t.Fatalf("published channels = %q, %q; want public-a, public-b", first.Channel, second.Channel)
	}
	if second.ExceptSocketID != "2.2" {
		t.Fatalf("ExceptSocketID = %q, want 2.2", second.ExceptSocketID)
	}

	var response map[string]map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("response JSON invalid: %v", err)
	}
	if _, ok := response["batch"]; !ok {
		t.Fatal("response missing batch object")
	}
}

func TestPusherAPIRejectsInvalidSignature(t *testing.T) {
	module, _, cleanup := newHTTPAPITestModule(t)
	defer cleanup()

	body := []byte(`{"name":"event","data":"{}","channel":"public-a"}`)
	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/events?auth_key=test-key&auth_signature=nope", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	module.servePusherAPI(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestPusherAPIManagementEndpoints(t *testing.T) {
	module, _, cleanup := newHTTPAPITestModule(t)
	defer cleanup()

	first, firstConn, firstCancel := newManagementClient(module.hub, "1.1")
	defer firstCancel()
	second, _, secondCancel := newManagementClient(module.hub, "2.2")
	defer secondCancel()

	if !module.hub.Register(first) || !module.hub.Register(second) {
		t.Fatal("failed to register management test clients")
	}
	defer module.hub.Unregister(first)
	defer module.hub.Unregister(second)
	first.SetUserID("signed-user")

	module.hub.shards[0].withSubscriptions(func(sm *SubscriptionManager) {
		sm.Subscribe(first, "public-room", nil)
		sm.Subscribe(first, "presence-room", []byte(`{"channel_data":"{\"user_id\":\"u1\"}"}`))
		sm.Subscribe(second, "presence-room", []byte(`{"channel_data":"{\"user_id\":\"u2\"}"}`))
	})

	response := performSignedPusherRequest(t, module, http.MethodGet, "/apps/test-app/connections", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("connections status = %d, body = %s", response.Code, response.Body.String())
	}
	var connections map[string]int
	if err := json.Unmarshal(response.Body.Bytes(), &connections); err != nil {
		t.Fatalf("connections JSON invalid: %v", err)
	}
	if connections["connections"] != 2 {
		t.Fatalf("connections = %d, want 2", connections["connections"])
	}

	response = performSignedPusherRequest(t, module, http.MethodGet, "/apps/test-app/channels?info=user_count", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("channels status = %d, body = %s", response.Code, response.Body.String())
	}
	var channels struct {
		Channels map[string]map[string]int `json:"channels"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &channels); err != nil {
		t.Fatalf("channels JSON invalid: %v", err)
	}
	if _, ok := channels.Channels["public-room"]; !ok {
		t.Fatalf("public-room missing from channels response: %s", response.Body.String())
	}
	if channels.Channels["presence-room"]["user_count"] != 2 {
		t.Fatalf("presence user_count = %d, want 2", channels.Channels["presence-room"]["user_count"])
	}

	response = performSignedPusherRequest(t, module, http.MethodGet, "/apps/test-app/channels/public-room?info=subscription_count", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("channel status = %d, body = %s", response.Code, response.Body.String())
	}
	var channel map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &channel); err != nil {
		t.Fatalf("channel JSON invalid: %v", err)
	}
	if channel["occupied"] != true || int(channel["subscription_count"].(float64)) != 1 {
		t.Fatalf("unexpected public channel response: %v", channel)
	}

	eventBody := []byte(`{"name":"server-event","data":"{}","channel":"public-room","info":"subscription_count"}`)
	response = performSignedPusherRequest(t, module, http.MethodPost, "/apps/test-app/events", eventBody)
	if response.Code != http.StatusOK {
		t.Fatalf("event info status = %d, body = %s", response.Code, response.Body.String())
	}
	var eventInfo struct {
		Channels map[string]map[string]int `json:"channels"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &eventInfo); err != nil {
		t.Fatalf("event info JSON invalid: %v", err)
	}
	if eventInfo.Channels["public-room"]["subscription_count"] != 1 {
		t.Fatalf("event info subscription_count = %d, want 1", eventInfo.Channels["public-room"]["subscription_count"])
	}

	batchBody := []byte(`{"batch":[{"name":"one","data":"{}","channel":"public-room"},{"name":"two","data":"{}","channel":"public-room","info":"subscription_count"}]}`)
	response = performSignedPusherRequest(t, module, http.MethodPost, "/apps/test-app/batch_events", batchBody)
	if response.Code != http.StatusOK {
		t.Fatalf("batch info status = %d, body = %s", response.Code, response.Body.String())
	}
	var batchInfo struct {
		Batch []map[string]int `json:"batch"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &batchInfo); err != nil {
		t.Fatalf("batch info JSON invalid: %v", err)
	}
	if len(batchInfo.Batch) != 2 || len(batchInfo.Batch[0]) != 0 || batchInfo.Batch[1]["subscription_count"] != 1 {
		t.Fatalf("unexpected batch info response: %v", batchInfo.Batch)
	}

	response = performSignedPusherRequest(t, module, http.MethodGet, "/apps/test-app/channels/presence-room/users", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("channel users status = %d, body = %s", response.Code, response.Body.String())
	}
	var users struct {
		Users []map[string]string `json:"users"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &users); err != nil {
		t.Fatalf("users JSON invalid: %v", err)
	}
	if len(users.Users) != 2 || users.Users[0]["id"] != "u1" || users.Users[1]["id"] != "u2" {
		t.Fatalf("unexpected users response: %v", users.Users)
	}

	response = performSignedPusherRequest(t, module, http.MethodGet, "/apps/test-app/channels/public-room/users", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("non-presence users status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	response = performSignedPusherRequest(t, module, http.MethodPost, "/apps/test-app/users/signed-user/terminate_connections", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("terminate status = %d, body = %s", response.Code, response.Body.String())
	}
	if !firstConn.CloseCalled {
		t.Fatal("terminate did not close signed-in user's connection")
	}
}

func TestSubscriptionManagerBroadcastToChannelExcludesSocketID(t *testing.T) {
	sm := NewSubscriptionManager(zap.NewNop(), nil, nil)
	first := &Client{ID: "1.1", send: make(chan any, 1)}
	second := &Client{ID: "2.2", send: make(chan any, 1)}
	sm.channels["public-test"] = map[*Client]bool{
		first:  true,
		second: true,
	}

	sm.BroadcastToChannel(&BroadcastMessage{
		Channel:        "public-test",
		Event:          "event",
		Data:           json.RawMessage(`{}`),
		ExceptSocketID: "1.1",
	})

	if len(first.send) != 0 {
		t.Fatalf("excluded client received %d messages", len(first.send))
	}
	if len(second.send) != 1 {
		t.Fatalf("second client received %d messages, want 1", len(second.send))
	}
}

func TestPusherAPIPathParsing(t *testing.T) {
	request, ok := pusherAPIPath("/apps/app-id/events")
	if !ok || request.AppID != "app-id" || request.Action != "events" {
		t.Fatalf("pusherAPIPath returned %#v, %v", request, ok)
	}

	request, ok = pusherAPIPath("/apps/app-id/channels/presence-room/users")
	if !ok || request.Action != "channel_users" || request.Channel != "presence-room" {
		t.Fatalf("pusherAPIPath users route returned %#v, %v", request, ok)
	}

	request, ok = pusherAPIPath("/apps/app-id/users/42/terminate_connections")
	if !ok || request.Action != "users_terminate" || request.UserID != "42" {
		t.Fatalf("pusherAPIPath terminate route returned %#v, %v", request, ok)
	}
	if _, ok := pusherAPIPath("/app/app-id"); ok {
		t.Fatal("pusherAPIPath accepted websocket path")
	}
}

func TestPusherAPIMethodChecks(t *testing.T) {
	module, _, cleanup := newHTTPAPITestModule(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/apps/test-app/events", nil)
	rr := httptest.NewRecorder()

	module.servePusherAPI(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
	if rr.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", rr.Header().Get("Allow"))
	}
}

func newHTTPAPITestModule(t *testing.T) (*WebsocketModule, *MockBroker, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	broker := &MockBroker{published: make(chan *BroadcastMessage, 4)}
	metrics := NewMetrics(prometheus.NewRegistry())
	hub := NewHub("test-app", zap.NewNop(), ctx, metrics, nil, nil, broker, 100, 1, DefaultPingPeriod, DefaultDeliveryConfig())
	if err := RegisterHub("test-app", hub); err != nil {
		cancel()
		t.Fatalf("RegisterHub failed: %v", err)
	}
	go hub.Run()

	return &WebsocketModule{
			AppID:     "test-app",
			AppKey:    "test-key",
			AppSecret: "secret",
			hub:       hub,
		}, broker, func() {
			UnregisterHub("test-app", hub)
			cancel()
			hub.Wait()
		}
}

func newManagementClient(hub *Hub, id string) (*Client, *MockWSConnection, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := NewMockWSConnection()
	return &Client{
		ID:     id,
		hub:    hub,
		conn:   conn,
		send:   make(chan any, 16),
		ctx:    ctx,
		cancel: cancel,
	}, conn, cancel
}

func performSignedPusherRequest(t *testing.T, module *WebsocketModule, method string, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, signedPusherURLWithMethod(method, path, body, "test-key", "secret"), bytes.NewReader(body))
	rr := httptest.NewRecorder()
	module.servePusherAPI(rr, req)
	return rr
}

func readPublishedMessage(t *testing.T, ch <-chan *BroadcastMessage) *BroadcastMessage {
	t.Helper()

	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for publish")
		return nil
	}
}

func signedPusherURL(path string, body []byte, key string, secret string) string {
	return signedPusherURLWithMethod(http.MethodPost, path, body, key, secret)
}

func signedPusherURLWithMethod(method string, rawPath string, body []byte, key string, secret string) string {
	parsed, err := url.Parse(rawPath)
	if err != nil {
		panic(err)
	}

	query := parsed.Query()
	params := map[string]string{}
	for key, values := range query {
		params[key] = strings.Join(values, ",")
	}
	params["auth_key"] = key
	params["auth_timestamp"] = "1"
	params["auth_version"] = "1.0"

	if len(body) > 0 {
		sum := md5.Sum(body)
		params["body_md5"] = hex.EncodeToString(sum[:])
	}

	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	query = url.Values{}
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
		query.Set(key, params[key])
	}

	toSign := strings.Join([]string{method, parsed.Path, strings.Join(pairs, "&")}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(toSign))
	query.Set("auth_signature", fmt.Sprintf("%x", mac.Sum(nil)))

	return parsed.Path + "?" + query.Encode()
}
