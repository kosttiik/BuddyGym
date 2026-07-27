package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "buddygym-core"

var ErrInvalidToken = errors.New("invalid token")

const (
	AuthMethodPassword = "password"
	AuthMethodSession  = "session"
	AuthMethodTelegram = "telegram"
)

type AccessToken struct {
	UserID     int64
	AuthMethod string
}

type accessClaims struct {
	jwt.RegisteredClaims
	AuthMethod string `json:"amr,omitempty"`
}

func IssueToken(secret []byte, userID int64, ttl time.Duration, now time.Time) (string, error) {
	return IssueTokenWithMethod(secret, userID, "", ttl, now)
}

func IssueTokenWithMethod(
	secret []byte,
	userID int64,
	authMethod string,
	ttl time.Duration,
	now time.Time,
) (string, error) {
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		AuthMethod: authMethod,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

func VerifyToken(secret []byte, token string, now time.Time) (int64, error) {
	access, err := VerifyAccessToken(secret, token, now)
	return access.UserID, err
}

func VerifyAccessToken(secret []byte, token string, now time.Time) (AccessToken, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return AccessToken{}, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return AccessToken{}, ErrInvalidToken
	}
	return AccessToken{UserID: userID, AuthMethod: claims.AuthMethod}, nil
}
