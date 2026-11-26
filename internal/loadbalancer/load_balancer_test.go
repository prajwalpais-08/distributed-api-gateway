package loadbalancer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRoundRobinDistribution(t *testing.T) {

	backend1 := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "products-service-1")
		},
	)

	backend2 := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "products-service-2")
		},
	)

	lb, err := New(backend1, backend2)

	if err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"products-service-1",
		"products-service-2",
		"products-service-1",
		"products-service-2",
		"products-service-1",
		"products-service-2",
	}

	for i, expectedBackend := range expected {

		req := httptest.NewRequest(
			http.MethodGet,
			"/api/products",
			nil,
		)

		recorder := httptest.NewRecorder()

		lb.ServeHTTP(recorder, req)

		if recorder.Body.String() != expectedBackend {
			t.Fatalf(
				"Request %d: expected %s, got %s",
				i+1,
				expectedBackend,
				recorder.Body.String(),
			)
		}
	}
}

func TestLoadBalancerRequiresBackend(t *testing.T) {

	_, err := New()

	if err == nil {
		t.Fatal("Expected an error when no backends are provided")
	}
}

func TestConcurrentRoundRobin(t *testing.T) {

	var count1 uint64
	var count2 uint64

	backend1 := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddUint64(&count1, 1)
			w.WriteHeader(http.StatusOK)
		},
	)

	backend2 := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddUint64(&count2, 1)
			w.WriteHeader(http.StatusOK)
		},
	)

	lb, err := New(backend1, backend2)

	if err != nil {
		t.Fatal(err)
	}

	const totalRequests = 100

	var wg sync.WaitGroup

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/products",
				nil,
			)

			recorder := httptest.NewRecorder()

			lb.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Errorf("Unexpected status: %d", recorder.Code)
			}
		}()
	}

	wg.Wait()

	got1 := atomic.LoadUint64(&count1)
	got2 := atomic.LoadUint64(&count2)

	if got1+got2 != totalRequests {
		t.Fatalf(
			"Expected %d requests, got %d",
			totalRequests,
			got1+got2,
		)
	}

	if got1 != 50 || got2 != 50 {
		t.Fatalf(
			"Expected 50 requests per backend, got %d and %d",
			got1,
			got2,
		)
	}

	t.Logf("Backend 1 received: %d requests", got1)
	t.Logf("Backend 2 received: %d requests", got2)
}
