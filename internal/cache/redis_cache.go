package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache manages HTTP response caching using Redis.
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// CachedResponse stores an HTTP response.
type CachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// New creates a Redis response cache.
func New(client *redis.Client, ttl time.Duration) (*Cache, error) {
	if client == nil {
		return nil, fmt.Errorf("Redis client cannot be nil")
	}

	if ttl <= 0 {
		return nil, fmt.Errorf("cache TTL must be positive")
	}

	return &Cache{
		client: client,
		ttl:    ttl,
	}, nil
}

// generateKey creates a unique key for each request.
func generateKey(r *http.Request) string {
	// Use method and URL path instead of host.
	// This allows multiple gateway instances
	// to share the same cached response.

	value := r.Method + ":" + r.URL.RequestURI()

	hash := sha256.Sum256([]byte(value))

	return "http_cache:" + hex.EncodeToString(hash[:])
}

// copyHeaders copies response headers.
func copyHeaders(destination, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}

// Middleware implements Redis response caching.
func (c *Cache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Only cache GET requests.
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		// Do not share responses belonging to authenticated requests.
		if r.Header.Get("Authorization") != "" ||
			r.Header.Get("Cookie") != "" {
			next.ServeHTTP(w, r)
			return
		}

		key := generateKey(r)

		// Look for a cached response in Redis.
		data, err := c.client.Get(r.Context(), key).Bytes()

		if err == nil {
			var cached CachedResponse

			if json.Unmarshal(data, &cached) == nil &&
				cached.StatusCode > 0 {

				copyHeaders(w.Header(), cached.Header)
				w.Header().Set("X-Cache", "HIT")

				w.WriteHeader(cached.StatusCode)
				_, _ = w.Write(cached.Body)

				return
			}
		} else if err != redis.Nil {
			log.Printf("Redis cache read failed: %v", err)
		}

		// Cache miss: execute the backend handler.
		recorder := httptest.NewRecorder()

		next.ServeHTTP(recorder, r)

		status := recorder.Code
		headers := recorder.Header().Clone()
		body := recorder.Body.Bytes()

		// Only cache safe, successful responses.
		cacheControl := strings.ToLower(headers.Get("Cache-Control"))

		cacheable := status == http.StatusOK &&
			headers.Get("Set-Cookie") == "" &&
			headers.Get("Vary") == "" &&
			!strings.Contains(cacheControl, "no-store") &&
			!strings.Contains(cacheControl, "no-cache") &&
			!strings.Contains(cacheControl, "private")

		if cacheable {
			cached := CachedResponse{
				StatusCode: status,
				Header:     headers,
				Body:       body,
			}

			encoded, marshalErr := json.Marshal(cached)

			if marshalErr == nil {
				if setErr := c.client.Set(
					r.Context(),
					key,
					encoded,
					c.ttl,
				).Err(); setErr != nil {
					log.Printf("Redis cache write failed: %v", setErr)
				}
			}
		}

		// Return the backend response.
		copyHeaders(w.Header(), headers)
		w.Header().Set("X-Cache", "MISS")

		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}
