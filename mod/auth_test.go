package websocket

import (
	"errors"
	"net/http"
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
	w.WriteHeader(200)
	_, _ = w.Write([]byte("{}"))

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

	auth := NewWorkerAuthProvider(logger, metrics, worker, "/auth", 1024, 100, "secret")

	client := &Client{ID: "test", Headers: make(http.Header)}

	res := auth.Authorize(client, "private-test")
	if !res.Allowed {
		t.Error("Expected allow")
	}

	worker.SetFail(true)

	for i := 0; i < CBThreshold; i++ {
		res = auth.Authorize(client, "private-fail")
		if res.Allowed {
			t.Error("Expected deny on worker failure")
		}
	}

	worker.Reset()

	res = auth.Authorize(client, "private-fail")
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

	auth := NewWorkerAuthProvider(logger, metrics, worker, "/auth", 1024, 2, "secret")
	client := &Client{ID: "test", Headers: make(http.Header)}

	var wg sync.WaitGroup
	results := make(chan bool, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := auth.Authorize(client, "private-test")
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
