package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMalformed   = errors.New("initdata malformed")
	ErrInvalidHash = errors.New("initdata hash mismatch")
	ErrExpired     = errors.New("initdata expired")
)

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
}

// Validate checks Telegram Mini App initData per
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func Validate(initData, botToken string, maxAge time.Duration, now time.Time) (TelegramUser, error) {
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, ErrMalformed
	}
	gotHash := vals.Get("hash")
	if gotHash == "" {
		return TelegramUser{}, ErrMalformed
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+vals.Get(k))
	}
	dataCheck := strings.Join(lines, "\n")

	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	want := hex.EncodeToString(hmacSHA256(secret, []byte(dataCheck)))
	if !hmac.Equal([]byte(want), []byte(gotHash)) {
		return TelegramUser{}, ErrInvalidHash
	}

	authDate, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil {
		return TelegramUser{}, ErrMalformed
	}
	if now.Sub(time.Unix(authDate, 0)) > maxAge {
		return TelegramUser{}, ErrExpired
	}

	var user TelegramUser
	if err := json.Unmarshal([]byte(vals.Get("user")), &user); err != nil || user.ID == 0 {
		return TelegramUser{}, ErrMalformed
	}
	return user, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
