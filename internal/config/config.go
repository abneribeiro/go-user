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
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
}

func Load() (Config, error) {
	cfg := Config{
		Env: str("APP_ENV", "development"),
		Port: str("PORT", "8080"),
		DatabaseURL: str("DATABASE_URL", ""),
		ReadTimeout: dur("READ_TIMEOUT", 5*time.Second),
		WriteTimeout: dur("WRITE_TIMEOUT", 10*time.Second),
		ShutdownTimeout: dur("SHUTDOWN_TIMEOUT", 15*time.Second),
		MaxBodyBytes: num("MAX_BODY_BYTES", 1<<20), // 1MB
	}

	//without database there is no service
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	return cfg, nil
}


func str(key, def string) string  {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return  def
}

func num(key string, def int64) int64 {
	v, err := strconv.ParseInt(os.Getenv(key), 10, 64)

	if err != nil {
		return def
	}
	return v
}

func dur(key string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(os.Getenv(key)) // "5s", "1m30s"

	if err != nil {
		return def
	}

	return  d
}