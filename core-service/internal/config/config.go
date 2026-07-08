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
}

func Load() (Config, error) {
	cfg := Config{
		BotToken:    os.Getenv("BOT_TOKEN"),
		HTTPAddr:    getenv("CORE_HTTP_ADDR", ":8080"),
		GRPCAddr:    getenv("CORE_GRPC_ADDR", ":9090"),
		DBDSN:       os.Getenv("CORE_DB_DSN"),
		RedisAddr:   getenv("REDIS_ADDR", "localhost:6379"),
		CheckinAddr: getenv("CHECKIN_GRPC_ADDR", "localhost:9091"),
	}
	if cfg.BotToken == "" {
		return Config{}, fmt.Errorf("BOT_TOKEN is required")
	}
	if cfg.DBDSN == "" {
		return Config{}, fmt.Errorf("CORE_DB_DSN is required")
	}
	raw := getenv("AUTH_TTL", "24h")
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl <= 0 {
		return Config{}, fmt.Errorf("invalid AUTH_TTL %q", raw)
	}
	cfg.AuthTTL = ttl
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
