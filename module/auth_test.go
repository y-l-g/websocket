package websocket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type MockWorker struct {
	mu         sync.Mutex
	ShouldFail bool
	Calls      int
	Delay      time.Duration
	AppKey     string
	Secret     string
}

func TestAuthenticateUserValidatesAppKeyAndSecret(t *testing.T) {
	auth := NewWorkerAuthProvider(zap.NewNop(), NewMetrics(prometheus.NewRegistry()), nil, "test-key", "/auth", 1024, 100, "app-secret")
	client := &Client{ID: "1.1"}
	userData := `{"id":"123"}`
	toSign := fmt.Sprintf("%s::user::%s", client.ID, userData)
	mac := hmac.New(sha256.New, []byte("app-secret"))
	mac.Write([]byte(toSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	if res := auth.AuthenticateUser(client, "test-key:"+signature, userData); !res.Allowed {
		t.Fatal("Expected user auth to be allowed")
	}
	if res := auth.AuthenticateUser(client, "other-key:"+signature, userData); res.Allowed {
		t.Fatal("Expected user auth with wrong app key to be denied")
	}
}

func TestAuthorizeProvidedChannelSignature(t *testing.T) {
	auth := NewWorkerAuthProvider(zap.NewNop(), NewMetrics(prometheus.NewRegistry()), nil, "test-key", "/auth", 1024, 100, "app-secret")
	client := &Client{ID: "1.1"}
	channel := "presence-room"
	channelData := `{"user_id":"123"}`
	toSign := channelStringToSign(client.ID, channel, channelData)
	mac := hmac.New(sha256.New, []byte("app-secret"))
	mac.Write([]byte(toSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	res := auth.Authorize(client, channel, "test-key:"+signature, channelData)
	if !res.Allowed {
		t.Fatal("Expected provided channel signature to be allowed")
	}

	res = auth.Authorize(client, channel, "test-key:"+signature, "")
	if res.Allowed {
		t.Fatal("Expected presence auth without channel_data to be denied")
	}
}

func TestAuthorizeWithoutSignatureRequiresWorker(t *testing.T) {
	auth := NewWorkerAuthProvider(zap.NewNop(), NewMetrics(prometheus.NewRegistry()), nil, "test-key", "", 1024, 100, "secret")
	client := &Client{ID: "1.1"}

	if res := auth.Authorize(client, "private-test", "", ""); res.Allowed {
		t.Fatal("Expected missing signature without worker to be denied")
	}
}

func (m *MockWorker) SendRequest(w http.ResponseWriter, r *http.Request) error {
	if m.Delay > 0 {
		time.Sleep(m.Delay)
	}

	m.mu.Lock()
	m.Calls++
	shouldFail := m.ShouldFail
	m.mu.Unlock()

	if shouldFail {
		return errors.New("worker crashed")
	}
	body, _ := io.ReadAll(r.Body)
	var payload map[string]string
	_ = json.Unmarshal(body, &payload)
	appKey := m.AppKey
	if appKey == "" {
		appKey = "test-key"
	}
	secret := m.Secret
	if secret == "" {
		secret = "secret"
	}
	socketID := payload["socket_id"]
	channel := payload["channel_name"]
	response := map[string]string{}
	channelData := ""
	if strings.HasPrefix(channel, "presence-") {
		channelData = `{"user_id":"1","user_info":{"name":"Test User"}}`
		response["channel_data"] = channelData
	}
	toSign := channelStringToSign(socketID, channel, channelData)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(toSign))
	response["auth"] = appKey + ":" + hex.EncodeToString(mac.Sum(nil))

	w.WriteHeader(200)
	_ = json.NewEncoder(w).Encode(response)

	return nil
}

func (m *MockWorker) SetFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldFail = fail
}

func (m *MockWorker) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = 0
	m.ShouldFail = false
}

func (m *MockWorker) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls
}

func TestCircuitBreakerIntegration(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewMetrics(prometheus.NewRegistry())
	worker := &MockWorker{}

	auth := NewWorkerAuthProvider(logger, metrics, worker, "test-key", "/auth", 1024, 100, "secret")

	client := &Client{ID: "test", Headers: make(http.Header)}

	res := auth.Authorize(client, "private-test", "", "")
	if !res.Allowed {
		t.Error("Expected allow")
	}

	worker.SetFail(true)

	for i := 0; i < CBThreshold; i++ {
		res = auth.Authorize(client, "private-fail", "", "")
		if res.Allowed {
			t.Error("Expected deny on worker failure")
		}
	}

	worker.Reset()

	res = auth.Authorize(client, "private-fail", "", "")
	if res.Allowed {
		t.Error("Expected deny from breaker")
	}

	if worker.GetCalls() > 0 {
		t.Error("Breaker should short-circuit and not call worker")
	}
}

func TestAuthConcurrencyLimit(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewMetrics(prometheus.NewRegistry())

	worker := &MockWorker{
		Delay: 100 * time.Millisecond,
	}

	auth := NewWorkerAuthProvider(logger, metrics, worker, "test-key", "/auth", 1024, 2, "secret")
	client := &Client{ID: "test", Headers: make(http.Header)}

	var wg sync.WaitGroup
	results := make(chan bool, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := auth.Authorize(client, "private-test", "", "")
			results <- res.Allowed
		}()
	}

	wg.Wait()
	close(results)

	success := 0
	denied := 0
	for allowed := range results {
		if allowed {
			success++
		} else {
			denied++
		}
	}

	if success != 2 {
		t.Errorf("Expected 2 successful auths, got %d", success)
	}
	if denied != 1 {
		t.Errorf("Expected 1 denied auth (concurrency limit), got %d", denied)
	}
}

func TestWorkerAuthRejectsInvalidSignature(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewMetrics(prometheus.NewRegistry())
	worker := &MockWorker{Secret: "wrong-secret"}
	auth := NewWorkerAuthProvider(logger, metrics, worker, "test-key", "/auth", 1024, 100, "secret")
	client := &Client{ID: "test", Headers: make(http.Header)}

	res := auth.Authorize(client, "private-test", "", "")
	if res.Allowed {
		t.Fatal("Expected worker response with invalid signature to be denied")
	}
}
