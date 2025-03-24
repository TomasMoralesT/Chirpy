package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/TomasMoralesT/Chirpy/internal/auth"
	"github.com/TomasMoralesT/Chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	queries        *database.Queries
	platform       string
	jwtSecret      string
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is not set in .env file")
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
		jwtSecret:      jwtSecret,
	}

	newMux := http.NewServeMux()

	newMux.HandleFunc("GET /api/healthz", healthHandler)

	fileServer := http.FileServer(http.Dir("."))
	newMux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", fileServer)))

	newMux.HandleFunc("GET /admin/metrics", cfg.metricsHandler)

	newMux.HandleFunc("POST /admin/reset", cfg.resetHandler)

	newMux.HandleFunc("POST /api/users", cfg.createUser)

	newMux.HandleFunc("POST /api/chirps", cfg.createChirp)

	newMux.HandleFunc("GET /api/chirps", cfg.getChirps)

	newMux.HandleFunc("GET /api/chirps/{chirpID}", cfg.getSingleChirp)

	newMux.HandleFunc("POST /api/login", cfg.login)

	newMux.HandleFunc("POST /api/refresh", cfg.handleRefresh)

	newMux.HandleFunc("POST /api/revoke", cfg.handleRevoke)

	newMux.HandleFunc("PUT /api/users", cfg.updateUser)

	newMux.HandleFunc("DELETE /api/chirps/{chirpID}", cfg.authMiddleware(cfg.deleteChirp))

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
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error decoding request body")
		return
	}

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating user")
		return
	}

	userParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	}

	user, err := cfg.queries.CreateUser(r.Context(), userParams)
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
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(responseUser)
}

func (cfg *apiConfig) createChirp(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Invalid request method")
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
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

	chirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: userID,
	}

	chirp, err := cfg.queries.CreateChirp(r.Context(), chirpParams)
	if err != nil {
		log.Printf("Error creating chirp: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating chirp")
		return
	}

	responseChirp := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(responseChirp)
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

func (cfg *apiConfig) getChirps(w http.ResponseWriter, r *http.Request) {

	dbChirps, err := cfg.queries.GetChirps(r.Context())
	if err != nil {
		log.Printf("Error getting chirps: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error getting chirps")
		return
	}

	chirps := []Chirp{}
	for _, dbChirp := range dbChirps {
		chirps = append(chirps, Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			UserID:    dbChirp.UserID,
			Body:      dbChirp.Body,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err = json.NewEncoder(w).Encode(chirps)
	if err != nil {
		log.Printf("Error encoding chirps: %s", err)
		respondWithError(w, http.StatusInternalServerError, "Error encoding chirps")
		return
	}
}

func (cfg *apiConfig) getSingleChirp(w http.ResponseWriter, r *http.Request) {

	chirpIDStr := r.PathValue(("chirpID"))

	chirpID, err := uuid.Parse(chirpIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
		return
	}

	dbChirp, err := cfg.queries.GetSingleChirp(r.Context(), chirpID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		}
	}

	respondWithJSON(w, http.StatusOK, Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		UserID:    dbChirp.UserID,
		Body:      dbChirp.Body,
	})
}

func (cfg *apiConfig) login(w http.ResponseWriter, r *http.Request) {

	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	type UserResponse struct {
		ID           string    `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, err := cfg.queries.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	err = auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create JWT token")
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create refresh token")
		return
	}

	expiresAt := time.Now().AddDate(0, 0, 60)

	err = cfg.queries.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:     refreshToken,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		log.Printf("Database error: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't store refresh token")
		return
	}

	respondWithJSON(w, http.StatusOK, UserResponse{
		ID:           user.ID.String(),
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		Token:        token,
		RefreshToken: refreshToken,
	})
}

func (cfg *apiConfig) handleRefresh(w http.ResponseWriter, r *http.Request) {

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}
	refreshToken := strings.TrimPrefix(authHeader, "Bearer ")

	dbToken, err := cfg.queries.GetRefreshToken(r.Context(), refreshToken)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't verify token")
		return
	}

	accessToken, err := auth.MakeJWT(dbToken.UserID, cfg.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create token")
		return
	}

	respondWithJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{
		Token: accessToken,
	})
}

func (cfg *apiConfig) handleRevoke(w http.ResponseWriter, r *http.Request) {

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}
	refreshToken := strings.TrimPrefix(authHeader, "Bearer ")

	err := cfg.queries.RevokeRefreshToken(r.Context(), refreshToken)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) updateUser(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPut {
		respondWithError(w, http.StatusMethodNotAllowed, "Invalid request method")
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		respondWithError(w, http.StatusUnauthorized, "Missing or malformed Authorization header")
		return
	}
	Token := strings.TrimPrefix(authHeader, "Bearer ")

	decoder := json.NewDecoder(r.Body)

	defer r.Body.Close()

	params := UpdateUserRequest{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if params.Email == "" {
		respondWithError(w, http.StatusBadRequest, "Email must not be empty")
		return
	}
	if params.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Password must not be empty")
		return
	}

	userID, err := auth.ValidateJWT(Token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	err = cfg.queries.UpdateUserByID(r.Context(), database.UpdateUserByIDParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
		ID:             userID,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	response := map[string]string{
		"id":    userID.String(),
		"email": params.Email,
	}
	respondWithJSON(w, http.StatusOK, response)
}

func (cfg *apiConfig) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, err.Error())
			return
		}

		userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		user, err := cfg.queries.GetUserByID(r.Context(), userID)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "Invalid user")
			return
		}

		ctx := context.WithValue(r.Context(), "user", user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		respondWithError(w, http.StatusBadRequest, "Invalid URL")
		return
	}
	chirpIDString := pathParts[3]

	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
		return
	}

	user := r.Context().Value("user").(database.User)

	chirp, err := cfg.queries.GetSingleChirp(r.Context(), chirpID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirp")
		return
	}

	if chirp.UserID != user.ID {
		respondWithError(w, http.StatusForbidden, "You can only delete your own chirps")
		return
	}

	err = cfg.queries.DeleteChirp(r.Context(), database.DeleteChirpParams{
		ID:     chirpID,
		UserID: user.ID,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete chirp")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
