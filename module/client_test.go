package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type MockWSConnection struct {
	mu                 sync.Mutex
	ReadMsgQueue       []string
	WriteMsgs          []string
	WriteDeadlineCalls int
	CloseCalled        bool
	Closed             chan struct{}
	NewMsg             chan struct{}
}

func NewMockWSConnection() *MockWSConnection {
	return &MockWSConnection{
		ReadMsgQueue: make([]string, 0),
		WriteMsgs:    make([]string, 0),
		Closed:       make(chan struct{}),
		NewMsg:       make(chan struct{}, 1),
	}
}

func (m *MockWSConnection) QueueReadMessage(msg string) {
	m.mu.Lock()
	m.ReadMsgQueue = append(m.ReadMsgQueue, msg)
	m.mu.Unlock()
	select {
	case m.NewMsg <- struct{}{}:
	default:
	}
}

func (m *MockWSConnection) ClearReadQueue() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReadMsgQueue = nil
}

func (m *MockWSConnection) SetReadLimit(limit int64)                    {}
func (m *MockWSConnection) SetReadDeadline(t time.Time) error           { return nil }
func (m *MockWSConnection) SetPongHandler(h func(appData string) error) {}

func (m *MockWSConnection) ReadMessage() (messageType int, p []byte, err error) {
	for {
		m.mu.Lock()
		if len(m.ReadMsgQueue) > 0 {
			msg := m.ReadMsgQueue[0]
			m.ReadMsgQueue = m.ReadMsgQueue[1:]
			m.mu.Unlock()
			return websocket.TextMessage, []byte(msg), nil
		}
		m.mu.Unlock()
		select {
		case <-m.NewMsg:
			continue
		case <-m.Closed:
			return 0, nil, errors.New("Connection Closed")
		}
	}
}

func (m *MockWSConnection) WriteMessage(messageType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WriteMsgs = append(m.WriteMsgs, string(data))
	return nil
}

func (m *MockWSConnection) WritePreparedMessage(pm *websocket.PreparedMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WriteMsgs = append(m.WriteMsgs, "[PreparedMessage]")
	return nil
}

func (m *MockWSConnection) WriteControl(messageType int, data []byte, deadline time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WriteMsgs = append(m.WriteMsgs, "[Control:"+string(data)+"]")
	return nil
}

type mockWriter struct {
	m *MockWSConnection
}

func (mw *mockWriter) Write(p []byte) (n int, err error) {
	mw.m.mu.Lock()
	defer mw.m.mu.Unlock()
	mw.m.WriteMsgs = append(mw.m.WriteMsgs, string(p))
	return len(p), nil
}
func (mw *mockWriter) Close() error { return nil }

func (m *MockWSConnection) NextWriter(messageType int) (io.WriteCloser, error) {
	return &mockWriter{m: m}, nil
}

func (m *MockWSConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.CloseCalled {
		m.CloseCalled = true
		close(m.Closed)
	}
	return nil
}
func (m *MockWSConnection) SetWriteDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WriteDeadlineCalls++
	return nil
}

type MockAuthProvider struct{}

func (m *MockAuthProvider) Authorize(client *Client, channel string) AuthResult {
	if channel == "private-forbidden" {
		return AuthResult{Allowed: false}
	}
	return AuthResult{Allowed: true, UserData: json.RawMessage(`{"user_id":"1"}`)}
}
func (m *MockAuthProvider) AuthenticateUser(client *Client, authSig string, userData string) AuthResult {
	return AuthResult{Allowed: true, UserData: json.RawMessage(userData)}
}

func TestClient_PingPong(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewMetrics(prometheus.NewRegistry())
	auth := &MockAuthProvider{}
	broker := &MockBroker{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub("test-app", logger, ctx, metrics, auth, nil, broker, 100, 4, DefaultPingPeriod)
	go hub.Run()

	mockConn := NewMockWSConnection()
	mockConn.QueueReadMessage(`{"event":"pusher:ping"}`)

	client := &Client{
		ID:         "test-client-1",
		hub:        hub,
		conn:       mockConn,
		send:       make(chan any, 10),
		ctx:        ctx,
		cancel:     cancel,
		PingPeriod: time.Second * 10,
		WriteWait:  time.Second,
		PongWait:   time.Second,
	}

	hub.Register(client)

	select {
	case <-client.send:
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for connection established msg")
	}

	done := make(chan struct{})
	go func() {
		client.readPump()
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	select {
	case msg := <-client.send:
		if bytesMsg, ok := msg.([]byte); ok {
			if !bytes.Contains(bytesMsg, []byte("pusher:pong")) {
				t.Errorf("Expected pong message, got: %s", bytesMsg)
			}
		}
	case <-time.After(1 * time.Second):
		t.Error("No pong message sent back")
	}

	_ = mockConn.Close() // Fixed
	<-done
}

func TestClient_Subscribe(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewMetrics(prometheus.NewRegistry())
	auth := &MockAuthProvider{}
	broker := &MockBroker{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub("test-app", logger, ctx, metrics, auth, nil, broker, 100, 4, DefaultPingPeriod)
	go hub.Run()

	mockConn := NewMockWSConnection()
	mockConn.QueueReadMessage(`{"event":"pusher:subscribe","data":{"channel":"public-test"}}`)

	client := &Client{
		ID:         "test-client-2",
		hub:        hub,
		conn:       mockConn,
		send:       make(chan any, 10),
		ctx:        ctx,
		cancel:     cancel,
		PingPeriod: time.Second * 10,
		WriteWait:  time.Second,
		PongWait:   time.Second,
	}

	hub.Register(client)
	<-client.send

	go client.readPump()

	select {
	case msg := <-client.send:
		if bytesMsg, ok := msg.([]byte); ok {
			var parsed map[string]interface{}
			_ = json.Unmarshal(bytesMsg, &parsed)
			if parsed["event"] != "pusher_internal:subscription_succeeded" {
				t.Errorf("Expected subscription_succeeded, got %v", parsed["event"])
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for sub success")
	}

	_ = mockConn.Close() // Fixed
	cancel()
}

func TestClient_WriteOutboundBurstDrainsQueuedMessagesUnderOneDeadline(t *testing.T) {
	mockConn := NewMockWSConnection()
	client := &Client{
		ID:        "test-client-burst",
		conn:      mockConn,
		send:      make(chan any, 10),
		WriteWait: time.Second,
	}

	client.send <- []byte("second")
	client.send <- []byte("third")

	if err := client.writeOutboundBurst([]byte("first")); err != nil {
		t.Fatalf("writeOutboundBurst failed: %v", err)
	}

	mockConn.mu.Lock()
	defer mockConn.mu.Unlock()

	if mockConn.WriteDeadlineCalls != 1 {
		t.Fatalf("Expected one write deadline for burst, got %d", mockConn.WriteDeadlineCalls)
	}

	expected := []string{"first", "second", "third"}
	if len(mockConn.WriteMsgs) != len(expected) {
		t.Fatalf("Expected %d writes, got %d: %v", len(expected), len(mockConn.WriteMsgs), mockConn.WriteMsgs)
	}
	for i, msg := range expected {
		if mockConn.WriteMsgs[i] != msg {
			t.Fatalf("Write %d mismatch: expected %q, got %q", i, msg, mockConn.WriteMsgs[i])
		}
	}
}

func TestClientWriteCompleteFromSentMetricRecordsBenchmarkMessages(t *testing.T) {
	t.Setenv("POGO_WS_HOT_PATH_METRICS", "true")

	metrics := NewMetrics(prometheus.NewRegistry())
	client := &Client{
		ID:        "test-client-write-complete",
		hub:       &Hub{metrics: metrics},
		conn:      NewMockWSConnection(),
		send:      make(chan any, 1),
		WriteWait: time.Second,
	}

	sentAtMs := float64(time.Now().Add(-time.Millisecond).UnixNano()) / float64(time.Millisecond)
	msg := benchmarkOutboundMessage{
		payload:  []byte("payload"),
		sentAtMs: sentAtMs,
	}

	if err := client.writeQueuedOutbound(msg, time.Now(), true); err != nil {
		t.Fatalf("writeQueuedOutbound failed: %v", err)
	}

	if count := metricCount(t, metrics.WriteCompleteFromSent); count != 1 {
		t.Fatalf("Expected write-complete count 1, got %d", count)
	}
}

func TestClientWriteCompleteFromSentMetricRequiresHotPathAndTimestamp(t *testing.T) {
	metrics := NewMetrics(prometheus.NewRegistry())
	client := &Client{
		ID:        "test-client-no-write-complete",
		hub:       &Hub{metrics: metrics},
		conn:      NewMockWSConnection(),
		send:      make(chan any, 1),
		WriteWait: time.Second,
	}

	sentAtMs := float64(time.Now().Add(-time.Millisecond).UnixNano()) / float64(time.Millisecond)
	if err := client.writeQueuedOutbound(benchmarkOutboundMessage{payload: []byte("payload"), sentAtMs: sentAtMs}, time.Now(), true); err != nil {
		t.Fatalf("writeQueuedOutbound failed: %v", err)
	}
	if count := metricCount(t, metrics.WriteCompleteFromSent); count != 0 {
		t.Fatalf("Expected disabled hot-path write-complete count 0, got %d", count)
	}

	t.Setenv("POGO_WS_HOT_PATH_METRICS", "true")
	metrics = NewMetrics(prometheus.NewRegistry())
	client.hub.metrics = metrics

	if err := client.writeQueuedOutbound([]byte("payload"), time.Now(), true); err != nil {
		t.Fatalf("writeQueuedOutbound failed: %v", err)
	}
	if count := metricCount(t, metrics.WriteCompleteFromSent); count != 0 {
		t.Fatalf("Expected non-benchmark write-complete count 0, got %d", count)
	}
}

func TestBenchmarkPayloadSentAtDetection(t *testing.T) {
	sentAt := 1234.5
	data, err := json.Marshal(map[string]any{
		"id":        1,
		"size":      10,
		"createdAt": 1234.0,
		"sentAt":    sentAt,
		"payload":   "XXXXXXXXXX",
	})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if got := benchmarkPayloadSentAt(data); got != sentAt {
		t.Fatalf("Expected sentAt %v, got %v", sentAt, got)
	}

	for _, data := range []json.RawMessage{
		json.RawMessage(`{"sentAt":1234.5}`),
		json.RawMessage(`not-json`),
	} {
		if got := benchmarkPayloadSentAt(data); got != 0 {
			t.Fatalf("Expected non-benchmark sentAt 0, got %v", got)
		}
	}
}
