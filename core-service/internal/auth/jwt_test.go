package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("test-secret-at-least-32-bytes-long!!")

func TestTokenRoundtrip(t *testing.T) {
	now := time.Now()
	token, err := IssueToken(jwtSecret, 42, time.Hour, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	id, err := VerifyToken(jwtSecret, token, now.Add(30*time.Minute))
	if err != nil || id != 42 {
		t.Fatalf("verify: id=%d err=%v", id, err)
	}
}

func TestTokenExpired(t *testing.T) {
	now := time.Now()
	token, _ := IssueToken(jwtSecret, 42, time.Hour, now)
	if _, err := VerifyToken(jwtSecret, token, now.Add(2*time.Hour)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestTokenWrongSecret(t *testing.T) {
	now := time.Now()
	token, _ := IssueToken(jwtSecret, 42, time.Hour, now)
	other := []byte("another-secret-also-32-bytes-long!!!")
	if _, err := VerifyToken(other, token, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong secret accepted: %v", err)
	}
}

func TestTokenWrongAlgAndGarbage(t *testing.T) {
	now := time.Now()
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	token, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyToken(jwtSecret, token, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("alg=none accepted: %v", err)
	}
	if _, err := VerifyToken(jwtSecret, "not.a.jwt", now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("garbage accepted: %v", err)
	}
}

func TestTokenWrongIssuerAndSubject(t *testing.T) {
	now := time.Now()
	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	token, _ := foreign.SignedString(jwtSecret)
	if _, err := VerifyToken(jwtSecret, token, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong issuer accepted: %v", err)
	}

	badSub := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   "abc",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	token, _ = badSub.SignedString(jwtSecret)
	if _, err := VerifyToken(jwtSecret, token, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("non-numeric subject accepted: %v", err)
	}
}
