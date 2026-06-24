package config

import (
	"fmt"
	"os"
)

type Config struct {
	GatewayPort            string
	Env                    string
	ClerkSecretKey         string
	AuthServiceAddr        string
	WorkspaceServiceAddr   string
	ProjectServiceAddr     string
	IntegrationServiceAddr string
}

func Load() (*Config, error) {
	cfg := &Config{
		GatewayPort:            getEnv("GATEWAY_PORT", "8080"),
		Env:                    getEnv("GATEWAY_ENV", "development"),
		ClerkSecretKey:         getEnv("CLERK_SECRET_KEY", ""),
		AuthServiceAddr:        getEnv("AUTH_SERVICE_ADDR", "localhost:9001"),
		WorkspaceServiceAddr:   getEnv("WORKSPACE_SERVICE_ADDR", "localhost:9002"),
		ProjectServiceAddr:     getEnv("PROJECT_SERVICE_ADDR", "localhost:9003"),
		IntegrationServiceAddr: getEnv("INTEGRATION_SERVICE_ADDR", "localhost:9004"),
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
