package circuitbreaker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Test 1: Circuit opens after reaching failure threshold.
func TestCircuitOpensAfterFailures(t *testing.T) {

	cb, err := New(3, 10*time.Second)

	if err != nil {
		t.Fatal(err)
	}

	failingBackend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	handler := cb.Middleware(failingBackend)

	// First three requests fail.
	for i := 0; i < 3; i++ {

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(
			recorder,
			httptest.NewRequest("GET", "/", nil),
		)

		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", recorder.Code)
		}
	}

	// Fourth request should be rejected immediately.
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		httptest.NewRequest("GET", "/", nil),
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected HTTP 503, got %d", recorder.Code)
	}

	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("Expected Retry-After header")
	}
}

// Test 2: Circuit closes after successful recovery.
func TestCircuitRecovery(t *testing.T) {

	cb, err := New(1, 50*time.Millisecond)

	if err != nil {
		t.Fatal(err)
	}

	failingBackend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	failingHandler := cb.Middleware(failingBackend)

	// Cause the circuit to open.
	first := httptest.NewRecorder()

	failingHandler.ServeHTTP(
		first,
		httptest.NewRequest("GET", "/", nil),
	)

	if first.Code != http.StatusInternalServerError {
		t.Fatalf("Expected HTTP 500, got %d", first.Code)
	}

	// Wait for the open timeout.
	time.Sleep(70 * time.Millisecond)

	// Send a recovery probe to a healthy backend.
	healthyBackend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	)

	healthyHandler := cb.Middleware(healthyBackend)

	probe := httptest.NewRecorder()

	healthyHandler.ServeHTTP(
		probe,
		httptest.NewRequest("GET", "/", nil),
	)

	if probe.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", probe.Code)
	}

	// The circuit should now be closed.
	next := httptest.NewRecorder()

	healthyHandler.ServeHTTP(
		next,
		httptest.NewRequest("GET", "/", nil),
	)

	if next.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200 after recovery, got %d", next.Code)
	}
}

// Test 3: Circuit remains open while recovery timeout is active.
func TestCircuitRejectsWhileOpen(t *testing.T) {

	cb, err := New(1, time.Second)

	if err != nil {
		t.Fatal(err)
	}

	failingBackend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	handler := cb.Middleware(failingBackend)

	first := httptest.NewRecorder()

	handler.ServeHTTP(
		first,
		httptest.NewRequest("GET", "/", nil),
	)

	second := httptest.NewRecorder()

	handler.ServeHTTP(
		second,
		httptest.NewRequest("GET", "/", nil),
	)

	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected HTTP 503, got %d", second.Code)
	}
}
