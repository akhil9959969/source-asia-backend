// source-asia-backend
// Single HTTP service implementing two APIs:
//
//   Part 1 — Rate-limited request API
//     POST /request
//     GET  /stats
//
//   Part 2 — Product catalog with media
//     POST /products
//     GET  /products
//     GET  /products/{id}
//     POST /products/{id}/media
//
// Built with Go 1.22 standard library only (no external dependencies).
// Run:  go run main.go
// Port: 8080 (override with PORT env var)
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/source-asia/backend/internal/product"
	"github.com/source-asia/backend/internal/ratelimit"
)

func main() {
	mux := http.NewServeMux()

	// ── Part 1: Rate-limited request API ────────────────────────────────────
	rlStore := ratelimit.NewStore()
	rlHandler := ratelimit.NewHandler(rlStore)

	mux.HandleFunc("POST /request", rlHandler.HandleRequest)
	mux.HandleFunc("GET /stats", rlHandler.HandleStats)

	// ── Part 2: Product catalog ──────────────────────────────────────────────
	productStore := product.NewStore()
	productHandler := product.NewHandler(productStore)

	mux.HandleFunc("POST /products", productHandler.Create)
	mux.HandleFunc("GET /products", productHandler.List)
	mux.HandleFunc("GET /products/{id}", productHandler.GetByID)
	mux.HandleFunc("POST /products/{id}/media", productHandler.AddMedia)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
