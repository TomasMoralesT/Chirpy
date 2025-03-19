package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/TomasMoralesT/Chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	queries        *database.Queries
	platform       string
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file:", err)
	}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Fatal("DB_URL is not set in .env file")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Error opening database connection:", err)
	}

	dbQueries := database.New(db)

	cfg := &apiConfig{
		fileserverHits: atomic.Int32{},
		queries:        dbQueries,
		platform:       os.Getenv("PLATFORM"),
	}

	newMux := http.NewServeMux()

	newMux.HandleFunc("GET /api/healthz", healthHandler)

	fileServer := http.FileServer(http.Dir("."))
	newMux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", fileServer)))

	newMux.HandleFunc("GET /admin/metrics", cfg.metricsHandler)

	newMux.HandleFunc("POST /admin/reset", cfg.resetHandler)

	newMux.HandleFunc("POST /api/users", cfg.createUser)

	newMux.HandleFunc("/api/validate_chirp", cfg.validateChirp)

	server := &http.Server{
		Addr:    ":8080",
		Handler: newMux,
	}

	fmt.Println("Server starting on :8080...")

	err = server.ListenAndServe()
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

	w.Header().Set("Content-Type", "text/html")
	htmlResponse := fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, hits)

	fmt.Fprint(w, htmlResponse)
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "This endpoint is only available in development mode")
		return
	}

	err := cfg.queries.DeleteAllUsers(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to reset database")
		return
	}

	w.WriteHeader(http.StatusOK)

}

func (cfg *apiConfig) createUser(w http.ResponseWriter, r *http.Request) {

	type parameters struct {
		Email string `json:"email"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error decoding request body")
		return
	}

	user, err := cfg.queries.CreateUser(r.Context(), params.Email)
	if err != nil {
		log.Printf("Error creating user: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating user")
		return
	}

	responseUser := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created

	json.NewEncoder(w).Encode(responseUser)
}

func (cfg *apiConfig) validateChirp(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Invalid request method")
		return
	}

	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error decoding request body")
		return
	}

	if len(params.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleanedBody := cleanProfanity(params.Body)

	response := map[string]string{"cleaned_body": cleanedBody}
	respondWithJSON(w, http.StatusOK, response)
}

func cleanProfanity(msg string) string {
	if len(msg) < 1 {
		return msg
	}
	splittedMsg := strings.Split(msg, " ")

	var wordList []string

	for _, word := range splittedMsg {
		if strings.ToLower(word) == "kerfuffle" ||
			strings.ToLower(word) == "sharbert" ||
			strings.ToLower(word) == "fornax" {
			word = "****"
		}
		wordList = append(wordList, word)
	}

	return strings.Join(wordList, " ")
}
