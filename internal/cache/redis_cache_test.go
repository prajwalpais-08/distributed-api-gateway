package cache

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Creates a Redis cache for integration testing.
func newTestCache(
	t *testing.T,
	ttl time.Duration,
) (*Cache, string) {

	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("Redis unavailable: %v", err)
	}

	c, err := New(client, ttl)

	if err != nil {
		client.Close()
		t.Fatal(err)
	}

	// Generate a unique URL for this test.
	targetURL := fmt.Sprintf(
		"http://cache.test/%s-%d",
		strings.ReplaceAll(t.Name(), "/", "_"),
		time.Now().UnixNano(),
	)

	key := generateKey(
		httptest.NewRequest(http.MethodGet, targetURL, nil),
	)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			2*time.Second,
		)
		defer cleanupCancel()

		client.Del(cleanupCtx, key)
		client.Close()
	})

	return c, targetURL
}

// Test 1: First request MISS, second request HIT.
func TestCacheMissThenHit(t *testing.T) {

	cache, targetURL := newTestCache(t, time.Minute)

	backendCalls := 0

	backend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			backendCalls++

			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Backend", "called")

			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte("Product information"))
		},
	)

	handler := cache.Middleware(backend)

	// First request.
	first := httptest.NewRecorder()

	handler.ServeHTTP(
		first,
		httptest.NewRequest(http.MethodGet, targetURL, nil),
	)

	if first.Header().Get("X-Cache") != "MISS" {
		t.Fatalf(
			"Expected MISS, got %s",
			first.Header().Get("X-Cache"),
		)
	}

	if first.Body.String() != "Product information" {
		t.Fatal("Unexpected first response body")
	}

	// Second identical request.
	second := httptest.NewRecorder()

	handler.ServeHTTP(
		second,
		httptest.NewRequest(http.MethodGet, targetURL, nil),
	)

	if second.Header().Get("X-Cache") != "HIT" {
		t.Fatalf(
			"Expected HIT, got %s",
			second.Header().Get("X-Cache"),
		)
	}

	if second.Body.String() != "Product information" {
		t.Fatal("Unexpected cached response body")
	}

	// Backend should only be called once.
	if backendCalls != 1 {
		t.Fatalf(
			"Expected 1 backend call, got %d",
			backendCalls,
		)
	}

	t.Log("First request: MISS")
	t.Log("Second request: HIT")
	t.Logf("Backend calls: %d", backendCalls)
}

// Test 2: Expired cache entries become MISS.
func TestCacheExpiration(t *testing.T) {

	cache, targetURL := newTestCache(
		t,
		100*time.Millisecond,
	)

	backendCalls := 0

	backend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			backendCalls++

			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte("Fresh data"))
		},
	)

	handler := cache.Middleware(backend)

	// First request.
	first := httptest.NewRecorder()

	handler.ServeHTTP(
		first,
		httptest.NewRequest(http.MethodGet, targetURL, nil),
	)

	if first.Header().Get("X-Cache") != "MISS" {
		t.Fatal("Expected initial cache MISS")
	}

	// Wait for Redis expiration.
	time.Sleep(250 * time.Millisecond)

	// Second request after expiration.
	second := httptest.NewRecorder()

	handler.ServeHTTP(
		second,
		httptest.NewRequest(http.MethodGet, targetURL, nil),
	)

	if second.Header().Get("X-Cache") != "MISS" {
		t.Fatal("Expected MISS after cache expiration")
	}

	if backendCalls != 2 {
		t.Fatalf(
			"Expected 2 backend calls, got %d",
			backendCalls,
		)
	}

	t.Log("Cache expiration verified")
}

// Test 3: POST requests should bypass the cache.
func TestCacheSkipsPostRequests(t *testing.T) {

	cache, targetURL := newTestCache(t, time.Minute)

	backendCalls := 0

	backend := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			backendCalls++

			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte("POST successful"))
		},
	)

	handler := cache.Middleware(backend)

	for i := 0; i < 2; i++ {

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodPost,
				targetURL,
				nil,
			),
		)

		if recorder.Header().Get("X-Cache") != "" {
			t.Fatal("POST request should bypass cache")
		}
	}

	if backendCalls != 2 {
		t.Fatalf(
			"Expected 2 backend calls, got %d",
			backendCalls,
		)
	}

	t.Log("POST requests correctly bypass cache")
}
