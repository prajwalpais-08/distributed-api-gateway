package ratelimiter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Creates a real Redis client and Redis-backed limiter for testing.
func newRedisTestLimiter(
	t *testing.T,
	capacity int,
	refillRate float64,
) (*redis.Client, *RedisLimiter, string) {

	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
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

	clientID := fmt.Sprintf(
		"test:%s:%d",
		strings.ReplaceAll(t.Name(), "/", "_"),
		time.Now().UnixNano(),
	)

	limiter, err := NewRedisLimiter(client, capacity, refillRate)

	if err != nil {
		client.Close()
		t.Fatalf("Failed to create Redis limiter: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			2*time.Second,
		)
		defer cleanupCancel()

		client.Del(cleanupCtx, "rate_limit:{"+clientID+"}")
		client.Close()
	})

	return client, limiter, clientID
}

// Test 1: Redis limiter enforces bucket capacity.
func TestRedisLimiterEnforcesCapacity(t *testing.T) {

	_, limiter, clientID := newRedisTestLimiter(t, 2, 0.001)

	ctx := context.Background()

	for i, expectedRemaining := range []int{1, 0} {

		allowed, remaining, _, err := limiter.Allow(ctx, clientID)

		if err != nil {
			t.Fatal(err)
		}

		if !allowed {
			t.Fatalf("Request %d should be allowed", i+1)
		}

		if remaining != expectedRemaining {
			t.Fatalf(
				"Expected %d tokens, got %d",
				expectedRemaining,
				remaining,
			)
		}
	}

	// Third request should be rejected.
	allowed, remaining, retryAfter, err := limiter.Allow(ctx, clientID)

	if err != nil {
		t.Fatal(err)
	}

	if allowed {
		t.Fatal("Expected request to be rejected")
	}

	if remaining != 0 {
		t.Fatalf("Expected 0 tokens, got %d", remaining)
	}

	if retryAfter <= 0 {
		t.Fatal("Expected a positive retry duration")
	}
}

// Test 2: Different clients have independent Redis buckets.
func TestRedisLimiterSeparateClients(t *testing.T) {

	client, limiter, clientID := newRedisTestLimiter(t, 1, 0.001)

	otherID := clientID + ":other"

	t.Cleanup(func() {
		client.Del(context.Background(), "rate_limit:{"+otherID+"}")
	})

	ctx := context.Background()

	// First client consumes its token.
	allowed, _, _, err := limiter.Allow(ctx, clientID)

	if err != nil || !allowed {
		t.Fatalf("First client should be allowed: %v", err)
	}

	// First client should now be rejected.
	allowed, _, _, err = limiter.Allow(ctx, clientID)

	if err != nil {
		t.Fatal(err)
	}

	if allowed {
		t.Fatal("First client should be rate limited")
	}

	// Second client gets its own bucket.
	allowed, _, _, err = limiter.Allow(ctx, otherID)

	if err != nil {
		t.Fatal(err)
	}

	if !allowed {
		t.Fatal("Second client should have its own bucket")
	}
}

// Test 3: Redis limiter replenishes tokens over time.
func TestRedisLimiterRefill(t *testing.T) {

	_, limiter, clientID := newRedisTestLimiter(t, 5, 1.0)

	ctx := context.Background()

	// Consume all five tokens.
	for i := 0; i < 5; i++ {

		allowed, _, _, err := limiter.Allow(ctx, clientID)

		if err != nil {
			t.Fatal(err)
		}

		if !allowed {
			t.Fatalf("Request %d should be allowed", i+1)
		}
	}

	// Wait for one token to refill.
	time.Sleep(1200 * time.Millisecond)

	allowed, remaining, _, err := limiter.Allow(ctx, clientID)

	if err != nil {
		t.Fatal(err)
	}

	if !allowed {
		t.Fatal("Expected a request to be allowed after refill")
	}

	t.Logf("Remaining tokens after refill: %d", remaining)
}

// Test 4: Concurrent requests share one Redis bucket
// across two independent Redis clients.
func TestRedisLimiterConcurrentRequests(t *testing.T) {

	_, limiter1, clientID := newRedisTestLimiter(
		t,
		5,
		0.0001,
	)

	// Create a second independent Redis client.
	client2 := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	defer client2.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()

	if err := client2.Ping(ctx).Err(); err != nil {
		t.Fatalf("Second Redis client failed: %v", err)
	}

	limiter2, err := NewRedisLimiter(client2, 5, 0.0001)

	if err != nil {
		t.Fatal(err)
	}

	const totalRequests = 40

	var wg sync.WaitGroup
	var mu sync.Mutex

	allowedCount := 0
	errorCount := 0

	for i := 0; i < totalRequests; i++ {

		// Alternate between two independent Redis clients.
		limiter := limiter1

		if i%2 == 1 {
			limiter = limiter2
		}

		wg.Add(1)

		go func(l *RedisLimiter) {
			defer wg.Done()

			requestCtx, requestCancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer requestCancel()

			allowed, _, _, err := l.Allow(
				requestCtx,
				clientID,
			)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errorCount++
				t.Errorf("Rate limiter error: %v", err)
				return
			}

			if allowed {
				allowedCount++
			}
		}(limiter)
	}

	wg.Wait()

	t.Logf("Total requests: %d", totalRequests)
	t.Logf("Allowed requests: %d", allowedCount)
	t.Logf("Rejected requests: %d", totalRequests-allowedCount)

	if errorCount != 0 {
		t.Fatalf("Encountered %d request errors", errorCount)
	}

	if allowedCount != 5 {
		t.Fatalf(
			"Expected exactly 5 allowed requests, got %d",
			allowedCount,
		)
	}
}
