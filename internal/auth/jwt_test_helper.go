package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtTestToken is a test helper that signs a JWT with HS256.
func jwtTestToken(t *testing.T, secret, sub, tier string, exp time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"tier": tier,
		"exp":  exp.Unix(),
		"iat":  time.Now().Unix(),
	})
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}
