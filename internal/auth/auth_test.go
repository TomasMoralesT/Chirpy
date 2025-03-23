package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestValidateJWT(t *testing.T) {
	t.Run("Valid Token", func(t *testing.T) {

		tokenSecret := "supersecret"
		userID := uuid.New()

		expiresIn := time.Hour
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
			Issuer:    "chirpy",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
		})
		tokenString, err := token.SignedString([]byte(tokenSecret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}

		returnedID, err := ValidateJWT(tokenString, tokenSecret)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if returnedID != userID {
			t.Errorf("expected userID %v, got %v", userID, returnedID)
		}
	})

	t.Run("Expired Token", func(t *testing.T) {
		tokenSecret := "supersecret"
		userID := uuid.New()

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
			Issuer:    "chirpy",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		})
		tokenString, err := token.SignedString([]byte(tokenSecret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}
		returnedID, err := ValidateJWT(tokenString, tokenSecret)
		if err == nil {
			t.Errorf("expected an error for expired token, but got none")
		}
		if returnedID != uuid.Nil {
			t.Errorf("expected uuid.Nil for expired token, got %v", returnedID)
		}
	})

	t.Run("Invalid Signature", func(t *testing.T) {
		tokenSecret := "supersecret"
		wrongSecret := "wrongsecret"
		userID := uuid.New()

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
			Issuer:    "chirpy",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		})
		tokenString, err := token.SignedString([]byte(wrongSecret))
		if err != nil {
			t.Fatalf("failed to sign token with wrong secret: %v", err)
		}

		returnedID, err := ValidateJWT(tokenString, tokenSecret)
		if err == nil {
			t.Errorf("expected an error for invalid signature, but got none")
		}
		if returnedID != uuid.Nil {
			t.Errorf("expected uuid.Nil for invalid signature, got %v", returnedID)
		}
	})

	t.Run("Malformed Token", func(t *testing.T) {
		tokenSecret := "supersecret"
		malformedToken := "this-is-not-a-valid-token"

		returnedID, err := ValidateJWT(malformedToken, tokenSecret)
		if err == nil {
			t.Errorf("expected an error for malformed token, but got none")
		}
		if returnedID != uuid.Nil {
			t.Errorf("expected uuid.Nil for malformed token, got %v", returnedID)
		}
	})

	t.Run("Missing Claims", func(t *testing.T) {
		tokenSecret := "supersecret"

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
			Issuer:    "chirpy",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		})
		tokenString, err := token.SignedString([]byte(tokenSecret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}

		returnedID, err := ValidateJWT(tokenString, tokenSecret)
		if err == nil {
			t.Errorf("expected an error for missing claims, but got none")
		}
		if returnedID != uuid.Nil {
			t.Errorf("expected uuid.Nil for missing claims, got %v", returnedID)
		}
	})
}
