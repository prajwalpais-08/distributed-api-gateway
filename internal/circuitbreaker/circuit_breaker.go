package circuitbreaker

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type CircuitBreaker struct {
	mu            sync.Mutex
	state         State
	failures      int
	threshold     int
	openTimeout   time.Duration
	openedAt      time.Time
	probeInFlight bool
}

// New creates a Circuit Breaker.
func New(threshold int, openTimeout time.Duration) (*CircuitBreaker, error) {

	if threshold <= 0 {
		return nil, fmt.Errorf("failure threshold must be positive")
	}

	if openTimeout <= 0 {
		return nil, fmt.Errorf("open timeout must be positive")
	}

	return &CircuitBreaker{
		state:       Closed,
		threshold:   threshold,
		openTimeout: openTimeout,
	}, nil
}

// allow determines whether a request can proceed.
func (cb *CircuitBreaker) allow() (bool, time.Duration) {

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {

	case Closed:
		return true, 0

	case Open:
		remaining := cb.openTimeout - time.Since(cb.openedAt)

		if remaining > 0 {
			return false, remaining
		}

		// Timeout expired: allow one recovery probe.
		cb.state = HalfOpen
		cb.probeInFlight = true

		return true, 0

	case HalfOpen:
		// Only one recovery request is allowed.
		if cb.probeInFlight {
			return false, cb.openTimeout
		}

		cb.probeInFlight = true
		return true, 0
	}

	return false, cb.openTimeout
}

// record updates the circuit state after a request.
func (cb *CircuitBreaker) record(success bool) {

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == HalfOpen {

		cb.probeInFlight = false

		if success {
			// Backend recovered.
			cb.state = Closed
			cb.failures = 0
			cb.openedAt = time.Time{}
		} else {
			// Backend still failing.
			cb.state = Open
			cb.failures = cb.threshold
			cb.openedAt = time.Now()
		}

		return
	}

	if cb.state != Closed {
		return
	}

	if success {
		cb.failures = 0
		return
	}

	cb.failures++

	if cb.failures >= cb.threshold {
		cb.state = Open
		cb.openedAt = time.Now()
	}
}

// responseRecorder captures the HTTP status.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (rr *responseRecorder) WriteHeader(status int) {

	if rr.status != 0 {
		return
	}

	rr.status = status
	rr.ResponseWriter.WriteHeader(status)
}

func (rr *responseRecorder) Write(data []byte) (int, error) {

	if rr.status == 0 {
		rr.WriteHeader(http.StatusOK)
	}

	return rr.ResponseWriter.Write(data)
}

// Unwrap preserves access to the underlying response writer.
func (rr *responseRecorder) Unwrap() http.ResponseWriter {
	return rr.ResponseWriter
}

// Middleware protects a backend handler.
func (cb *CircuitBreaker) Middleware(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		allowed, retryAfter := cb.allow()

		if !allowed {

			seconds := int(math.Ceil(retryAfter.Seconds()))

			if seconds < 1 {
				seconds = 1
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(seconds))

			w.WriteHeader(http.StatusServiceUnavailable)

			_, _ = w.Write([]byte(
				`{"error":"circuit_breaker_open","message":"Service temporarily unavailable"}`,
			))

			return
		}

		recorder := &responseRecorder{
			ResponseWriter: w,
		}

		next.ServeHTTP(recorder, r)

		status := recorder.status

		if status == 0 {
			status = http.StatusOK
		}

		// Server errors count as backend failures.
		cb.record(status < 500)
	})
}
