package config

import "testing"

func setRequired(t *testing.T) {
	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("CORE_DB_DSN", "postgres://localhost/core_db")
	t.Setenv("JWT_SECRET", "test-secret-with-at-least-32-bytes!!")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GRPCAddr != ":9090" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.AuthTTL.Hours() != 24 {
		t.Errorf("AuthTTL = %v, want 24h", cfg.AuthTTL)
	}
}

func TestLoadMissingBotToken(t *testing.T) {
	setRequired(t)
	t.Setenv("BOT_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("want error for missing BOT_TOKEN")
	}
}

func TestLoadShortJWTSecret(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("want error for short JWT_SECRET")
	}
	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("want error for missing JWT_SECRET")
	}
}

func TestLoadBadJWTTTL(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_TTL", "zzz")
	if _, err := Load(); err == nil {
		t.Fatal("want error for bad JWT_TTL")
	}
}

func TestLoadMissingDSN(t *testing.T) {
	setRequired(t)
	t.Setenv("CORE_DB_DSN", "")
	if _, err := Load(); err == nil {
		t.Fatal("want error for missing CORE_DB_DSN")
	}
}

func TestLoadBadTTL(t *testing.T) {
	setRequired(t)
	t.Setenv("AUTH_TTL", "nope")
	if _, err := Load(); err == nil {
		t.Fatal("want error for bad AUTH_TTL")
	}
	t.Setenv("AUTH_TTL", "-1h")
	if _, err := Load(); err == nil {
		t.Fatal("want error for negative AUTH_TTL")
	}
}

func TestLoadOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("CORE_HTTP_ADDR", ":1234")
	t.Setenv("AUTH_TTL", "1h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":1234" || cfg.AuthTTL.Hours() != 1 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}
