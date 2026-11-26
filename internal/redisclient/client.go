package redisclient

import (
	"os"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a Redis client.
func NewClient() *redis.Client {
	address := os.Getenv("REDIS_ADDR")

	if address == "" {
		address = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr: address,
		DB:   0,
	})

	return client
}
