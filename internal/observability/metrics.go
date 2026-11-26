package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry     *prometheus.Registry
	requests     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	cacheRequest *prometheus.CounterVec
}

// New creates a new Prometheus metrics collector.
func New() *Metrics {

	registry := prometheus.NewRegistry()

	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_http_requests_total",
			Help: "Total number of HTTP requests handled by the gateway.",
		},
		[]string{"method", "route", "status"},
	)

	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	cacheRequest := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_cache_requests_total",
			Help: "Number of cache hits and misses.",
		},
		[]string{"result"},
	)

	registry.MustRegister(
		requests,
		duration,
		cacheRequest,
	)

	return &Metrics{
		registry:     registry,
		requests:     requests,
		duration:     duration,
		cacheRequest: cacheRequest,
	}
}

// routeName prevents high-cardinality URL labels.
func routeName(path string) string {

	switch {

	case path == "/api/users" ||
		strings.HasPrefix(path, "/api/users/"):
		return "users"

	case path == "/api/products" ||
		strings.HasPrefix(path, "/api/products/"):
		return "products"

	case path == "/healthz":
		return "health"

	case path == "/metrics":
		return "metrics"

	default:
		return "other"
	}
}

// responseWriter records the HTTP status.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {

	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {

	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Middleware records request metrics.
func (m *Metrics) Middleware(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		recorder := &responseWriter{
			ResponseWriter: w,
		}

		next.ServeHTTP(recorder, r)

		status := recorder.status

		if status == 0 {
			status = http.StatusOK
		}

		route := routeName(r.URL.Path)

		m.requests.WithLabelValues(
			r.Method,
			route,
			strconv.Itoa(status),
		).Inc()

		m.duration.WithLabelValues(
			r.Method,
			route,
		).Observe(time.Since(start).Seconds())

		cacheResult := recorder.Header().Get("X-Cache")

		if cacheResult == "HIT" {
			m.cacheRequest.WithLabelValues("hit").Inc()
		} else if cacheResult == "MISS" {
			m.cacheRequest.WithLabelValues("miss").Inc()
		}
	})
}

// Handler exposes metrics in Prometheus format.
func (m *Metrics) Handler() http.Handler {

	return promhttp.HandlerFor(
		m.registry,
		promhttp.HandlerOpts{},
	)
}
