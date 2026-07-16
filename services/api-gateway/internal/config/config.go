package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	GatewayPort            string
	Env                    string
	ClerkSecretKey         string
	AuthServiceAddr        string
	WorkspaceServiceAddr   string
	ProjectServiceAddr     string
	IntegrationServiceAddr string
	AIWorkerURL            string
	AIWorkerInternalToken  string
	FrontendURL            string
	AllowedOrigins         []string
	RateLimitRequests      int
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
		AIWorkerURL:            getEnv("AI_WORKER_URL", "http://localhost:8090"),
		AIWorkerInternalToken:  getEnv("AI_WORKER_INTERNAL_TOKEN", ""),
		FrontendURL:            getEnv("FRONTEND_URL", "http://localhost:3000"),
		AllowedOrigins:         splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),
	}

	if cfg.ClerkSecretKey == "" {
		return nil, fmt.Errorf("CLERK_SECRET_KEY is required")
	}
	rateLimit, err := strconv.Atoi(getEnv("RATE_LIMIT_REQUESTS_PER_MINUTE", "120"))
	if err != nil || rateLimit < 10 || rateLimit > 10000 {
		return nil, fmt.Errorf("RATE_LIMIT_REQUESTS_PER_MINUTE must be between 10 and 10000")
	}
	cfg.RateLimitRequests = rateLimit
	if len(cfg.AllowedOrigins) == 0 {
		return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS must contain at least one origin")
	}
	for _, origin := range cfg.AllowedOrigins {
		parsed, parseErr := url.Parse(origin)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.Contains(origin, "*") {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS must contain exact http(s) origins")
		}
	}
	frontendURL, err := url.Parse(cfg.FrontendURL)
	if err != nil || (frontendURL.Scheme != "http" && frontendURL.Scheme != "https") || frontendURL.Host == "" || frontendURL.Path != "" {
		return nil, fmt.Errorf("FRONTEND_URL must be an http(s) origin without a path")
	}

	return cfg, nil
}

func splitCSV(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, strings.TrimRight(trimmed, "/"))
		}
	}
	return result
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
