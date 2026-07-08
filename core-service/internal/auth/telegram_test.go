package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const testToken = "7000000000:AAtest-token-for-unit-tests"

// Sign builds initData the same way Telegram does.
func sign(t *testing.T, fields map[string]string, botToken string) string {
	t.Helper()
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secretMac.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	fields["hash"] = hex.EncodeToString(mac.Sum(nil))

	q := url.Values{}
	for k, v := range fields {
		q.Set(k, v)
	}
	return q.Encode()
}

func validFields(now time.Time) map[string]string {
	return map[string]string{
		"auth_date": fmt.Sprintf("%d", now.Unix()),
		"query_id":  "AAF03QwqAAAAAPTdDCrh8yLZ",
		"user":      `{"id":42,"first_name":"Kostya","username":"kostik","photo_url":"https://t.me/i/userpic/42.jpg"}`,
	}
}

func TestValidateOK(t *testing.T) {
	now := time.Now()
	initData := sign(t, validFields(now), testToken)
	u, err := Validate(initData, testToken, 24*time.Hour, now)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if u.ID != 42 || u.Username != "kostik" || u.FirstName != "Kostya" {
		t.Errorf("unexpected user: %+v", u)
	}
}

func TestValidateWrongToken(t *testing.T) {
	now := time.Now()
	initData := sign(t, validFields(now), "1:other-token")
	if _, err := Validate(initData, testToken, 24*time.Hour, now); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("err = %v, want ErrInvalidHash", err)
	}
}

func TestValidateTampered(t *testing.T) {
	now := time.Now()
	fields := validFields(now)
	initData := sign(t, fields, testToken)
	tampered := strings.Replace(initData, "id%22%3A42", "id%22%3A43", 1)
	if tampered == initData {
		t.Fatal("tampering failed to change payload")
	}
	if _, err := Validate(tampered, testToken, 24*time.Hour, now); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("err = %v, want ErrInvalidHash", err)
	}
}

func TestValidateExpired(t *testing.T) {
	now := time.Now()
	fields := validFields(now.Add(-25 * time.Hour))
	initData := sign(t, fields, testToken)
	if _, err := Validate(initData, testToken, 24*time.Hour, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestValidateMalformed(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"empty":       "",
		"no hash":     "auth_date=1&user=%7B%7D",
		"bad query":   "%zz",
		"no user":     sign(t, map[string]string{"auth_date": fmt.Sprintf("%d", now.Unix())}, testToken),
		"no authdate": sign(t, map[string]string{"user": `{"id":42,"first_name":"K"}`}, testToken),
	}
	for name, initData := range cases {
		if _, err := Validate(initData, testToken, 24*time.Hour, now); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}
