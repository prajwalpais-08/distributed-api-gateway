package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	responsecache "example.com/distributed-api-gateway/internal/cache"
	"example.com/distributed-api-gateway/internal/circuitbreaker"
	"example.com/distributed-api-gateway/internal/loadbalancer"
	"example.com/distributed-api-gateway/internal/observability"
	"example.com/distributed-api-gateway/internal/ratelimiter"
	"example.com/distributed-api-gateway/internal/redisclient"
)

func main() {

	// 1. Connect to Redis.
	client := redisclient.NewClient()
	defer client.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)

	err := client.Ping(ctx).Err()
	cancel()

	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}

	log.Println("Connected to Redis successfully")

	// 2. Create distributed rate limiter.
	rateLimiter, err := ratelimiter.NewRedisLimiter(
		client,
		5,
		0.2,
	)

	if err != nil {
		log.Fatal(err)
	}

	// 3. Create response cache.
	cache, err := responsecache.New(
		client,
		30*time.Second,
	)

	if err != nil {
		log.Fatal(err)
	}

	// 4. Initialize Prometheus metrics.
	gatewayMetrics := observability.New()

	log.Println("Prometheus metrics initialized")

	// 5. Configure Users backend.
	usersTarget, err := url.Parse("http://localhost:9001")
	if err != nil {
		log.Fatal(err)
	}

	usersProxy := httputil.NewSingleHostReverseProxy(usersTarget)

	// 6. Configure Products backend 1.
	productsTarget1, err := url.Parse("http://localhost:9002")
	if err != nil {
		log.Fatal(err)
	}

	productsProxy1 := httputil.NewSingleHostReverseProxy(productsTarget1)

	// 7. Configure Products backend 2.
	productsTarget2, err := url.Parse("http://localhost:9003")
	if err != nil {
		log.Fatal(err)
	}

	productsProxy2 := httputil.NewSingleHostReverseProxy(productsTarget2)

	// 8. Create separate Circuit Breakers.
	breaker1, err := circuitbreaker.New(
		3,
		10*time.Second,
	)

	if err != nil {
		log.Fatal(err)
	}

	breaker2, err := circuitbreaker.New(
		3,
		10*time.Second,
	)

	if err != nil {
		log.Fatal(err)
	}

	// Apply Circuit Breakers to individual backends.
	protectedBackend1 := breaker1.Middleware(productsProxy1)
	protectedBackend2 := breaker2.Middleware(productsProxy2)

	// 9. Create Round Robin load balancer.
	productsLB, err := loadbalancer.NewWithHandlers(

		loadbalancer.BackendConfig{
			Handler:   protectedBackend1,
			HealthURL: "http://localhost:9002/health",
		},

		loadbalancer.BackendConfig{
			Handler:   protectedBackend2,
			HealthURL: "http://localhost:9003/health",
		},
	)

	if err != nil {
		log.Fatal(err)
	}

	// 10. Start automatic backend health checks.
	healthCtx, stopHealthChecks := context.WithCancel(
		context.Background(),
	)
	defer stopHealthChecks()

	productsLB.StartHealthChecks(
		healthCtx,
		5*time.Second,
	)

	log.Println("Products load balancer initialized")
	log.Println("Individual Circuit Breakers initialized")

	// 11. Create router.
	mux := http.NewServeMux()

	// Users: Metrics -> Rate Limiter -> Cache -> Backend.
	usersCached := cache.Middleware(usersProxy)
	usersRateLimited := rateLimiter.Middleware(usersCached)
	usersHandler := gatewayMetrics.Middleware(usersRateLimited)

	// Products: Metrics -> Rate Limiter -> Cache
	// -> Load Balancer -> Circuit Breakers -> Backend.
	productsCached := cache.Middleware(productsLB)
	productsRateLimited := rateLimiter.Middleware(productsCached)
	productsHandler := gatewayMetrics.Middleware(productsRateLimited)

	// Register API routes.
	mux.Handle("/api/users", usersHandler)
	mux.Handle("/api/users/", usersHandler)

	mux.Handle("/api/products", productsHandler)
	mux.Handle("/api/products/", productsHandler)

	// 12. Prometheus metrics endpoint.
	mux.Handle("/metrics", gatewayMetrics.Handler())

	// 13. Gateway health check.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("API Gateway is healthy"))
	})

	address := os.Getenv("GATEWAY_ADDR")

if address == "" {
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    address = ":" + port
}

	log.Printf("API Gateway running on %s", address)

	err = http.ListenAndServe(address, mux)

	if err != nil {
		log.Fatal(err)
	}
}
