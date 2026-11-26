package loadbalancer

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"
)

// BackendConfig defines a backend and its health endpoint.
type BackendConfig struct {
	Handler   http.Handler
	HealthURL string
}

type backend struct {
	handler   http.Handler
	healthURL string
	healthy   atomic.Bool
}

type LoadBalancer struct {
	backends []*backend
	next     uint64
	client   *http.Client
}

// New creates a load balancer without health-check URLs.
func New(handlers ...http.Handler) (*LoadBalancer, error) {

	configs := make([]BackendConfig, 0, len(handlers))

	for _, handler := range handlers {
		configs = append(configs, BackendConfig{
			Handler: handler,
		})
	}

	return NewWithHandlers(configs...)
}

// NewWithHandlers accepts handlers with optional health endpoints.
func NewWithHandlers(configs ...BackendConfig) (*LoadBalancer, error) {

	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one backend is required")
	}

	lb := &LoadBalancer{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}

	for i, config := range configs {

		if config.Handler == nil {
			return nil, fmt.Errorf("backend %d is nil", i)
		}

		if config.HealthURL == "" &&
			config.Handler != nil {

			b := &backend{
				handler: config.Handler,
			}

			b.healthy.Store(true)
			lb.backends = append(lb.backends, b)
			continue
		}

		b := &backend{
			handler:   config.Handler,
			healthURL: config.HealthURL,
		}

		// Health-checked backends start as unhealthy
		// until the first successful check.
		b.healthy.Store(false)

		lb.backends = append(lb.backends, b)
	}

	return lb, nil
}

// NewWithURLs creates a load balancer directly from backend URLs.
func NewWithURLs(targets ...*url.URL) (*LoadBalancer, error) {

	configs := make([]BackendConfig, 0, len(targets))

	for i, target := range targets {

		if target == nil ||
			target.Scheme == "" ||
			target.Host == "" {

			return nil, fmt.Errorf("invalid backend URL at index %d", i)
		}

		targetCopy := *target

		proxy := httputil.NewSingleHostReverseProxy(&targetCopy)

		healthTarget := targetCopy
		healthTarget.Path = "/health"
		healthTarget.RawPath = ""
		healthTarget.RawQuery = ""
		healthTarget.Fragment = ""

		configs = append(configs, BackendConfig{
			Handler:   proxy,
			HealthURL: healthTarget.String(),
		})
	}

	return NewWithHandlers(configs...)
}

// StartHealthChecks checks all backend health endpoints periodically.
func (lb *LoadBalancer) StartHealthChecks(
	ctx context.Context,
	interval time.Duration,
) {

	if interval <= 0 {
		interval = 5 * time.Second
	}

	lb.checkHealth()

	go func() {

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				lb.checkHealth()
			}
		}
	}()
}

// checkHealth checks each backend.
func (lb *LoadBalancer) checkHealth() {

	for _, b := range lb.backends {

		if b.healthURL == "" {
			continue
		}

		response, err := lb.client.Get(b.healthURL)

		healthy := false

		if err == nil {
			healthy = response.StatusCode >= 200 &&
				response.StatusCode < 300

			response.Body.Close()
		}

		previous := b.healthy.Swap(healthy)

		if previous != healthy {
			log.Printf(
				"Backend %s health changed: %v",
				b.healthURL,
				healthy,
			)
		}
	}
}

// ServeHTTP distributes requests across healthy backends.
func (lb *LoadBalancer) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {

	start := atomic.AddUint64(&lb.next, 1) - 1

	total := uint64(len(lb.backends))

	for i := uint64(0); i < total; i++ {

		index := (start + i) % total

		b := lb.backends[index]

		if b.healthy.Load() {
			b.handler.ServeHTTP(w, r)
			return
		}
	}

	http.Error(
		w,
		"All backend services are unavailable",
		http.StatusServiceUnavailable,
	)
}
