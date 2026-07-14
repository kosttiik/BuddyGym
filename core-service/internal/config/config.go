// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	BotToken    string
	HTTPAddr    string
	GRPCAddr    string
	DBDSN       string
	RedisAddr   string
	CheckinAddr string
	AuthTTL     time.Duration
	JWTSecret   []byte
	JWTTTL      time.Duration
	S3          S3Config
}

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// Enabled reports whether avatar mirroring can run. Without object storage the service still
// serves everything else, users just fall back to initials.
func (c S3Config) Enabled() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != ""
}

func Load() (Config, error) {
	cfg := Config{
		BotToken:    os.Getenv("BOT_TOKEN"),
		HTTPAddr:    getenv("CORE_HTTP_ADDR", ":8080"),
		GRPCAddr:    getenv("CORE_GRPC_ADDR", ":9090"),
		DBDSN:       os.Getenv("CORE_DB_DSN"),
		RedisAddr:   getenv("REDIS_ADDR", "localhost:6379"),
		CheckinAddr: getenv("CHECKIN_GRPC_ADDR", "localhost:9091"),
		S3: S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    getenv("S3_AVATAR_BUCKET", "avatars"),
			UseSSL:    os.Getenv("S3_USE_SSL") == "true",
		},
	}
	if cfg.BotToken == "" {
		return Config{}, fmt.Errorf("BOT_TOKEN is required")
	}
	if cfg.DBDSN == "" {
		return Config{}, fmt.Errorf("CORE_DB_DSN is required")
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}
	cfg.JWTSecret = []byte(secret)

	for _, d := range []struct {
		key, def string
		dst      *time.Duration
	}{
		{"AUTH_TTL", "24h", &cfg.AuthTTL},
		{"JWT_TTL", "24h", &cfg.JWTTTL},
	} {
		raw := getenv(d.key, d.def)
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl <= 0 {
			return Config{}, fmt.Errorf("invalid %s %q", d.key, raw)
		}
		*d.dst = ttl
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
