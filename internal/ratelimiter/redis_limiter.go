package ratelimiter

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis Lua script for atomic Token Bucket operations.
const tokenBucketLua = `
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])

-- Use Redis server time so all gateway instances
-- share the same clock.
local time = redis.call('TIME')
local now_ms = tonumber(time[1]) * 1000
    + math.floor(tonumber(time[2]) / 1000)

-- Read the existing bucket.
local values = redis.call(
    'HMGET', KEYS[1], 'tokens', 'last_refill_ms'
)

local tokens = tonumber(values[1])
local last_refill = tonumber(values[2])

-- Initialize a new client with a full bucket.
if tokens == nil then
    tokens = capacity
    last_refill = now_ms
end

-- Avoid moving the timestamp backwards.
if now_ms < last_refill then
    now_ms = last_refill
end

-- Refill tokens according to elapsed time.
local elapsed = (now_ms - last_refill) / 1000

tokens = math.min(
    capacity,
    tokens + elapsed * refill_rate
)

-- Check whether this request is allowed.
local allowed = 0

if tokens >= 1 then
    tokens = tokens - 1
    allowed = 1
end

local remaining = math.floor(tokens)
local retry_ms = 0

if allowed == 0 then
    retry_ms = math.ceil(
        ((1 - tokens) / refill_rate) * 1000
    )
end

-- Save the updated bucket.
redis.call(
    'HSET',
    KEYS[1],
    'tokens', tokens,
    'last_refill_ms', now_ms
)

-- Remove inactive buckets after they would be full again.
local ttl_ms = math.ceil(
    (capacity / refill_rate) * 1000
)

redis.call('PEXPIRE', KEYS[1], ttl_ms)

return {allowed, remaining, retry_ms}
`

var tokenBucketScript = redis.NewScript(tokenBucketLua)

// RedisLimiter stores rate-limit state in Redis.
type RedisLimiter struct {
	client     *redis.Client
	capacity   int
	refillRate float64
}

// NewRedisLimiter creates a Redis-backed rate limiter.
func NewRedisLimiter(
	client *redis.Client,
	capacity int,
	refillRate float64,
) (*RedisLimiter, error) {

	if client == nil {
		return nil, fmt.Errorf("Redis client cannot be nil")
	}

	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive")
	}

	if refillRate <= 0 ||
		math.IsNaN(refillRate) ||
		math.IsInf(refillRate, 0) {
		return nil, fmt.Errorf("refill rate must be positive and finite")
	}

	return &RedisLimiter{
		client:     client,
		capacity:   capacity,
		refillRate: refillRate,
	}, nil
}

// Allow checks whether a client can make a request.
func (l *RedisLimiter) Allow(
	ctx context.Context,
	clientID string,
) (bool, int, time.Duration, error) {

	if strings.TrimSpace(clientID) == "" {
		return false, 0, 0, fmt.Errorf("client ID cannot be empty")
	}

	key := "rate_limit:{" + clientID + "}"

	result, err := tokenBucketScript.Run(
		ctx,
		l.client,
		[]string{key},
		l.capacity,
		l.refillRate,
	).Result()

	if err != nil {
		return false, 0, 0, err
	}

	values, ok := result.([]interface{})

	if !ok || len(values) != 3 {
		return false, 0, 0,
			fmt.Errorf("unexpected Redis script response: %T", result)
	}

	allowedValue, err := redisInt(values[0])
	if err != nil {
		return false, 0, 0, err
	}

	remainingValue, err := redisInt(values[1])
	if err != nil {
		return false, 0, 0, err
	}

	retryValue, err := redisInt(values[2])
	if err != nil {
		return false, 0, 0, err
	}

	return allowedValue == 1,
		int(remainingValue),
		time.Duration(retryValue) * time.Millisecond,
		nil
}

// redisInt converts a Redis Lua result into an integer.
func redisInt(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf(
			"unexpected Redis value type: %T", value,
		)
	}
}

// Middleware applies Redis-based rate limiting.
func (l *RedisLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)

		if err != nil {
			clientIP = r.RemoteAddr
		}

		allowed, remaining, retryAfter, err := l.Allow(
			r.Context(),
			clientIP,
		)

		if err != nil {
			// Fail closed if Redis is unavailable.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(
				`{"error":"rate_limiter_unavailable"}`,
			))
			return
		}

		w.Header().Set(
			"X-RateLimit-Limit",
			strconv.Itoa(l.capacity),
		)

		w.Header().Set(
			"X-RateLimit-Remaining",
			strconv.Itoa(remaining),
		)

		if !allowed {
			retrySeconds := int(math.Ceil(retryAfter.Seconds()))

			if retrySeconds < 1 {
				retrySeconds = 1
			}

			w.Header().Set(
				"Retry-After",
				strconv.Itoa(retrySeconds),
			)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)

			_, _ = w.Write([]byte(
				`{"error":"rate_limit_exceeded","message":"Too many requests. Please try again later."}`,
			))

			return
		}

		next.ServeHTTP(w, r)
	})
}
