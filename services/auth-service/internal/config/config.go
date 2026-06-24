package config

import (
	"fmt"
	"os"
)

type Config struct {
	GRPCPort    string
	HealthPort  string
	DatabaseURL string
	ClerkSecretKey string
	Env         string
}

func Load() (*Config, error) {
	cfg := &Config{
		GRPCPort:       getEnv("AUTH_SERVICE_PORT", "9001"),
		HealthPort:     getEnv("AUTH_SERVICE_HEALTH_PORT", "8081"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		ClerkSecretKey: getEnv("CLERK_SECRET_KEY", ""),
		Env:            getEnv("GATEWAY_ENV", "development"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.ClerkSecretKey == "" {
		return nil, fmt.Errorf("CLERK_SECRET_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
