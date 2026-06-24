package config

import (
	"fmt"
	"os"
)

type Config struct {
	GRPCPort    string
	HealthPort  string
	DatabaseURL string
}

func Load() (*Config, error) {
	cfg := &Config{
		GRPCPort:    getEnv("WORKSPACE_SERVICE_PORT", "9002"),
		HealthPort:  getEnv("WORKSPACE_SERVICE_HEALTH_PORT", "8082"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
