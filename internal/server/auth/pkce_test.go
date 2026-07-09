package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !VerifyPKCE(challenge, verifier) {
		t.Fatal("valid verifier rejected")
	}
	if VerifyPKCE(challenge, "wrong-verifier") {
		t.Fatal("invalid verifier accepted")
	}
}
