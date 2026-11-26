package main

import (
	"context"
	"log"
	"time"

	"example.com/distributed-api-gateway/internal/redisclient"
)

func main() {
	// Create Redis client.
	client := redisclient.NewClient()
	defer client.Close()

	// Set a 3-second timeout.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()

	// Send PING to Redis.
	err := client.Ping(ctx).Err()

	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}

	log.Println("Successfully connected to Redis!")
	log.Println("Redis response: PONG")
}
