package ratelimiter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Test 1: A client cannot exceed bucket capacity.
func TestAllowEnforcesCapacity(t *testing.T) {

	limiter := NewLimiter(2, 0.2)

	for i, expectedRemaining := range []int{1, 0} {
		allowed, remaining, _ := limiter.allow("client-1")

		if !allowed {
			t.Fatalf("Request %d should be allowed", i+1)
		}

		if remaining != expectedRemaining {
			t.Fatalf("Expected %d tokens, got %d",
				expectedRemaining, remaining)
		}
	}

	// The third request should be rejected.
	allowed, remaining, retryAfter := limiter.allow("client-1")

	if allowed {
		t.Fatal("Expected request to be rejected")
	}

	if remaining != 0 {
		t.Fatalf("Expected 0 remaining tokens, got %d", remaining)
	}

	if retryAfter < 1 {
		t.Fatal("Expected a positive Retry-After value")
	}
}

// Test 2: Tokens are replenished over time.
func TestTokenRefill(t *testing.T) {

	limiter := NewLimiter(2, 1)

	limiter.allow("client-1")
	limiter.allow("client-1")

	// Simulate one second passing.
	limiter.mu.Lock()
	limiter.clients["client-1"].lastRefill =
		time.Now().Add(-time.Second)
	limiter.mu.Unlock()

	allowed, _, _ := limiter.allow("client-1")

	if !allowed {
		t.Fatal("Expected request to be allowed after refill")
	}
}

// Test 3: Different clients have separate buckets.
func TestSeparateClientBuckets(t *testing.T) {

	limiter := NewLimiter(1, 0.2)

	// First client consumes its token.
	allowed, _, _ := limiter.allow("client-1")

	if !allowed {
		t.Fatal("First client should be allowed")
	}

	// Second client should have its own token.
	allowed, _, _ = limiter.allow("client-2")

	if !allowed {
		t.Fatal("Second client should have its own bucket")
	}

	// First client should now be rejected.
	allowed, _, _ = limiter.allow("client-1")

	if allowed {
		t.Fatal("First client should be rate limited")
	}
}

// Test 4: HTTP middleware returns 429.
func TestMiddlewareRateLimiting(t *testing.T) {

	limiter := NewLimiter(1, 0.1)

	handler := limiter.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	// First request.
	req1 := httptest.NewRequest("GET", "/api/users", nil)
	req1.RemoteAddr = "192.0.2.10:1234"

	res1 := httptest.NewRecorder()

	handler.ServeHTTP(res1, req1)

	if res1.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", res1.Code)
	}

	// Second request from the same client.
	req2 := httptest.NewRequest("GET", "/api/users", nil)
	req2.RemoteAddr = "192.0.2.10:1234"

	res2 := httptest.NewRecorder()

	handler.ServeHTTP(res2, req2)

	if res2.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected HTTP 429, got %d", res2.Code)
	}

	// Verify error response.
	if !strings.Contains(res2.Body.String(), "rate_limit_exceeded") {
		t.Fatal("Expected rate limit error message")
	}

	if res2.Header().Get("Retry-After") == "" {
		t.Fatal("Expected Retry-After header")
	}
}
