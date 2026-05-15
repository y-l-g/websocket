package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseBenchSentAtFromPusherStringData(t *testing.T) {
	raw := json.RawMessage(`"{\"id\":1,\"sentAt\":1234.5,\"payload\":\"xxx\"}"`)

	got, err := parseBenchSentAt(raw)
	if err != nil {
		t.Fatalf("parseBenchSentAt returned error: %v", err)
	}
	if got != 1234.5 {
		t.Fatalf("sentAt = %v, want 1234.5", got)
	}
}

func TestParseBenchSentAtFromObjectData(t *testing.T) {
	raw := json.RawMessage(`{"id":1,"sentAt":9876,"payload":"xxx"}`)

	got, err := parseBenchSentAt(raw)
	if err != nil {
		t.Fatalf("parseBenchSentAt returned error: %v", err)
	}
	if got != 9876 {
		t.Fatalf("sentAt = %v, want 9876", got)
	}
}

func TestParseBenchSentAtRejectsMissingSentAt(t *testing.T) {
	if _, err := parseBenchSentAt(json.RawMessage(`{"id":1}`)); err == nil {
		t.Fatal("parseBenchSentAt returned nil error for missing sentAt")
	}
}

func TestPercentile(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}

	tests := map[string]struct {
		q    float64
		want float64
	}{
		"p50": {q: 0.50, want: 30},
		"p90": {q: 0.90, want: 46},
		"p95": {q: 0.95, want: 48},
		"p99": {q: 0.99, want: 49.6},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := percentile(values, tt.q)
			if got != tt.want {
				t.Fatalf("percentile(%v) = %v, want %v", tt.q, got, tt.want)
			}
		})
	}
}

func TestPercentileEmpty(t *testing.T) {
	if got := percentile(nil, 0.95); got != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", got)
	}
}

func TestDecorateDialErrorIncludesStatusAndBody(t *testing.T) {
	err := decorateDialError(io.ErrUnexpectedEOF, &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader("Too Many Requests\n")),
	})

	got := err.Error()
	if !strings.Contains(got, "status=429") {
		t.Fatalf("decorated error %q does not include status", got)
	}
	if !strings.Contains(got, "Too Many Requests") {
		t.Fatalf("decorated error %q does not include body", got)
	}
}
