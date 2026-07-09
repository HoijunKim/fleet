// Package auth implements fleet's server-side identity: GitHub OAuth (native
// loopback + PKCE), JWT access tokens, and rotating hashed refresh tokens.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IssueAccess mints an HS256 access token with sub=userID, expiring now+ttl.
func IssueAccess(signingKey []byte, userID string, ttl time.Duration, now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey)
}

// VerifyAccess validates an HS256 token and returns its subject (user id).
func VerifyAccess(signingKey []byte, token string) (string, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return signingKey, nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid || claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
