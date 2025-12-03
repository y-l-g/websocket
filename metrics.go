package websocket

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Create a dedicated registry for our module
var registry = prometheus.NewRegistry()

var (
	// Define metrics attached to OUR registry
	metricConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "frankenphp_ws_connections_active",
		Help: "Current number of active WebSocket connections",
	})

	metricMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "frankenphp_ws_messages_total",
		Help: "Total number of messages published to the Hub",
	})

	metricSubscriptions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "frankenphp_ws_subscriptions_total",
		Help: "Total number of channel subscriptions",
	})

	metricAuthDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "frankenphp_ws_auth_seconds",
		Help:    "Duration of PHP Worker authentication requests",
		Buckets: prometheus.DefBuckets,
	})

	metricBreakerTripped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "frankenphp_ws_circuit_breaker_open_total",
		Help: "Total number of auth requests rejected because the Circuit Breaker was open",
	})

	metricAuthFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "frankenphp_ws_auth_failures_total",
		Help: "Total number of failed auth requests (timeouts, 500s)",
	})
)

func init() {
	// Register everything to our private registry
	registry.MustRegister(metricConnections)
	registry.MustRegister(metricMessages)
	registry.MustRegister(metricSubscriptions)
	registry.MustRegister(metricAuthDuration)
	registry.MustRegister(metricBreakerTripped)
	registry.MustRegister(metricAuthFailures)

	// Initialize to 0
	metricConnections.Set(0)
	metricMessages.Add(0)
	metricSubscriptions.Add(0)
	metricBreakerTripped.Add(0)
	metricAuthFailures.Add(0)
}

// StartMetricsServer starts a dedicated HTTP server for metrics on port 9090
func StartMetricsServer() {
	go func() {
		fmt.Println("📢 METRICS: Starting dedicated server on :9090")
		http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		if err := http.ListenAndServe(":9090", nil); err != nil {
			fmt.Printf("Error starting metrics server: %v\n", err)
		}
	}()
}

// Keep the old function for compatibility but make it no-op or log
func RegisterMetrics(reg prometheus.Registerer) {
	fmt.Println("⚠️ METRICS: Ignoring Caddy Registry (Using standalone :9090)")
}
