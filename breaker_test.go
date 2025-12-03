package websocket

import (
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker()

	// 1. Initially Closed (Allowed)
	if !cb.Allow() {
		t.Error("Circuit should be initially closed (Allow=true)")
	}

	// 2. Report Failures up to threshold
	for i := 0; i < CBThreshold; i++ {
		cb.Report(false)
	}

	// 3. Should be Open (Not Allowed)
	if cb.Allow() {
		t.Error("Circuit should be open after threshold reached")
	}

	// 4. Test Reset Timeout
	// Manually inject a past time to simulate timeout expiry
	cb.mu.Lock()
	cb.lastFailure = time.Now().Add(-CBResetTimeout - 1*time.Second)
	cb.mu.Unlock()

	if !cb.Allow() {
		t.Error("Circuit should be half-open (Allow=true) after timeout")
	}

	// 5. Success should reset failures
	cb.Report(true)

	cb.mu.RLock()
	failures := cb.failures
	cb.mu.RUnlock()

	if failures != 0 {
		t.Errorf("Report(true) should reset failures to 0, got %d", failures)
	}
}
