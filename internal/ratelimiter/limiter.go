package ratelimiter

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// bucket represents the rate limit state of one client.
type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// Limiter manages request limits for different clients.
type Limiter struct {
	mu         sync.Mutex
	capacity   float64
	refillRate float64
	clients    map[string]*bucket
}

// NewLimiter creates a new rate limiter.
func NewLimiter(capacity int, refillRate float64) *Limiter {
	return &Limiter{
		capacity:   float64(capacity),
		refillRate: refillRate,
		clients:    make(map[string]*bucket),
	}
}

// allow checks whether a client can make another request.
func (l *Limiter) allow(clientID string) (bool, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	b, exists := l.clients[clientID]

	// Give new clients a full bucket.
	if !exists {
		b = &bucket{
			tokens:     l.capacity,
			lastRefill: now,
		}

		l.clients[clientID] = b
	} else {
		// Refill tokens according to elapsed time.
		elapsed := now.Sub(b.lastRefill).Seconds()

		b.tokens += elapsed * l.refillRate

		if b.tokens > l.capacity {
			b.tokens = l.capacity
		}

		b.lastRefill = now
	}

	// Allow request if a token is available.
	if b.tokens >= 1 {
		b.tokens--

		return true, int(math.Floor(b.tokens)), 0
	}

	// Calculate when the next token will become available.
	retryAfter := int(math.Ceil(
		(1 - b.tokens) / l.refillRate,
	))

	if retryAfter < 1 {
		retryAfter = 1
	}

	return false, int(math.Floor(b.tokens)), retryAfter
}

// Middleware applies rate limiting to an HTTP handler.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Identify the client by its IP address.
		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)

		if err != nil {
			clientIP = r.RemoteAddr
		}

		allowed, remaining, retryAfter := l.allow(clientIP)

		w.Header().Set(
			"X-RateLimit-Limit",
			strconv.Itoa(int(l.capacity)),
		)

		w.Header().Set(
			"X-RateLimit-Remaining",
			strconv.Itoa(remaining),
		)

		// Reject requests when no tokens remain.
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(
				"Retry-After",
				strconv.Itoa(retryAfter),
			)

			w.WriteHeader(http.StatusTooManyRequests)

			w.Write([]byte(
				`{"error":"rate_limit_exceeded","message":"Too many requests. Please try again later."}`,
			))

			return
		}

		// Continue to the backend service.
		next.ServeHTTP(w, r)
	})
}
