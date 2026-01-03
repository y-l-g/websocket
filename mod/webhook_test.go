package websocket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestWebhookManager_Notify(t *testing.T) {
	secret := "super-secret-key"
	received := make(chan *WebhookPayload, 1)
	signatureHeader := make(chan string, 1)

	// 1. Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture Signature
		signatureHeader <- r.Header.Get("X-Pusher-Signature")

		// Capture Body
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		received <- &payload

		w.WriteHeader(200)
	}))
	defer ts.Close()

	// 2. Init Manager
	logger := zap.NewNop()
	wm := NewWebhookManager(logger, ts.URL, secret)

	// 3. Trigger Notification
	wm.Notify("channel_occupied", "presence-test")

	// 4. Verify
	select {
	case payload := <-received:
		if len(payload.Events) != 1 {
			t.Errorf("Expected 1 event, got %d", len(payload.Events))
		}
		if payload.Events[0].Name != "channel_occupied" {
			t.Errorf("Expected event channel_occupied, got %s", payload.Events[0].Name)
		}
		if payload.Events[0].Channel != "presence-test" {
			t.Errorf("Expected channel presence-test, got %s", payload.Events[0].Channel)
		}

		// Check TimeMs (sanity check)
		if payload.TimeMs == 0 {
			t.Error("Expected TimeMs to be set")
		}

	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for webhook")
	}
}

func TestWebhookSignature(t *testing.T) {
	// Precise test for signature logic
	secret := "my-secret"
	rawBody := make(chan []byte, 1)
	rawSig := make(chan string, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rawBody <- body
		rawSig <- r.Header.Get("X-Pusher-Signature")
	}))
	defer ts.Close()

	wm := NewWebhookManager(zap.NewNop(), ts.URL, secret)
	wm.Notify("test_event", "test_channel")

	select {
	case body := <-rawBody:
		sig := <-rawSig

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))

		if sig != expected {
			t.Errorf("Signature mismatch.\nGot: %s\nExp: %s", sig, expected)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout")
	}
}
