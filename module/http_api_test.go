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
	appID, action, ok := pusherAPIPath("/apps/app-id/events")
	if !ok || appID != "app-id" || action != "events" {
		t.Fatalf("pusherAPIPath returned %q, %q, %v", appID, action, ok)
	}
	if _, _, ok := pusherAPIPath("/app/app-id"); ok {
		t.Fatal("pusherAPIPath accepted websocket path")
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

	return &WebsocketModule{
			AppID:     "test-app",
			AppKey:    "test-key",
			AppSecret: "secret",
			hub:       hub,
		}, broker, func() {
			UnregisterHub("test-app", hub)
			cancel()
		}
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
	params := map[string]string{
		"auth_key":       key,
		"auth_timestamp": "1",
		"auth_version":   "1.0",
	}
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
	query := url.Values{}
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
		query.Set(key, params[key])
	}

	toSign := strings.Join([]string{http.MethodPost, path, strings.Join(pairs, "&")}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(toSign))
	query.Set("auth_signature", fmt.Sprintf("%x", mac.Sum(nil)))

	return path + "?" + query.Encode()
}
