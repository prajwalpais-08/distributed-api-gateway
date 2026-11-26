package redisclient

import (
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a Redis client.
func NewClient() *redis.Client {
	redisURL := os.Getenv("REDIS_URL")

	if redisURL != "" {
		options, err := redis.ParseURL(redisURL)
		if err != nil {
			log.Fatalf("Invalid REDIS_URL: %v", err)
		}

		return redis.NewClient(options)
	}

	address := os.Getenv("REDIS_ADDR")

	if address == "" {
		address = "localhost:6379"
	}

	return redis.NewClient(&redis.Options{
		Addr: address,
		DB:   0,
	})
}
