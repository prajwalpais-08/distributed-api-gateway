package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {

	port := os.Getenv("PORT")
	if port == "" {
		port = "9001"
	}

	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName == "" {
		serviceName = "users-service"
	}

	// Enable intentional failures when configured.
	failRequests := os.Getenv("FAIL_REQUESTS") == "true"

	mux := http.NewServeMux()

	// Health endpoint always remains healthy.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"service": serviceName,
		})
	})

	// Main service endpoint.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/json")

		// Simulate application failure.
		if failRequests {
			w.WriteHeader(http.StatusInternalServerError)

			json.NewEncoder(w).Encode(map[string]string{
				"service": serviceName,
				"message": "Simulated backend failure",
				"status":  "error",
			})

			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"service": serviceName,
			"message": "Hello from backend service!",
			"path":    r.URL.Path,
			"method":  r.Method,
		})
	})

	address := ":" + port

	log.Printf("%s running on port %s", serviceName, port)

	if failRequests {
		log.Println("FAILURE MODE ENABLED")
	}

	log.Fatal(http.ListenAndServe(address, mux))
}
