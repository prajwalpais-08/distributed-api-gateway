package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsMiddleware(t *testing.T) {

	metrics := New()

	backend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			w.Header().Set("X-Cache", "MISS")

			w.WriteHeader(http.StatusOK)
		},
	)

	handler := metrics.Middleware(backend)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/products",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	// Request the Prometheus metrics endpoint.
	metricsRecorder := httptest.NewRecorder()

	metrics.Handler().ServeHTTP(
		metricsRecorder,
		httptest.NewRequest("GET", "/metrics", nil),
	)

	body := metricsRecorder.Body.String()

	expectedMetrics := []string{
		"gateway_http_requests_total",
		"gateway_http_request_duration_seconds",
		"gateway_cache_requests_total",
		`result="miss"`,
		`status="200"`,
	}

	for _, expected := range expectedMetrics {

		if !strings.Contains(body, expected) {
			t.Errorf("Expected metric %q not found", expected)
		}
	}
}
