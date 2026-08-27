package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env             string
	Port            string
	DatabaseURL     string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	HandlerTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
}

func Load() (Config, error) {
	var err error
	cfg := Config{
		Env:         str("APP_ENV", "development"),
		Port:        str("PORT", "8080"),
		DatabaseURL: str("DATABASE_URL", ""),
	}

	// A malformed value is not the same as an unset one. Silently falling
	// back turns MAX_BODY_BYTES=abc into "the body limit is whatever the
	// default is" — a security control misconfigured without a word.
	if cfg.ReadTimeout, err = dur("READ_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = dur("WRITE_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.HandlerTimeout, err = dur("HANDLER_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = dur("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.MaxBodyBytes, err = num("MAX_BODY_BYTES", 1<<20); err != nil { // 1MB
		return Config{}, err
	}

	//without database there is no service
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	// The per-handler timeout has to fire before the server gives up writing,
	// otherwise the connection is cut before the 503 can leave.
	if cfg.HandlerTimeout >= cfg.WriteTimeout {
		return Config{}, fmt.Errorf(
			"config: HANDLER_TIMEOUT (%s) must be shorter than WRITE_TIMEOUT (%s)",
			cfg.HandlerTimeout, cfg.WriteTimeout)
	}

	return cfg, nil
}

func str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

func num(key string, def int64) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}

	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("config: %s must be a positive integer, got %q", key, raw)
	}
	return v, nil
}

func dur(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}

	d, err := time.ParseDuration(raw) // "5s", "1m30s"
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive duration, got %q", key, raw)
	}
	return d, nil
}
