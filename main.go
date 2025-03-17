package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	newMux := http.NewServeMux()
	cfg := &apiConfig{}

	newMux.HandleFunc("/healthz", healthHandler)

	fileServer := http.FileServer(http.Dir("."))
	newMux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", fileServer)))

	newMux.HandleFunc("/metrics", cfg.metricsHandler)

	newMux.HandleFunc("/reset", cfg.resetHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: newMux,
	}

	fmt.Println("Server starting on :8080...")

	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("Server error:", err)
	}

}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	hits := cfg.fileserverHits.Load()
	fmt.Fprintf(w, "Hits: %d", hits)
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hits counter reset"))
}
