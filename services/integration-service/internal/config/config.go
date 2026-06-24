package config

import (
	"fmt"
	"os"
)

type Config struct {
	GRPCPort                string
	HealthPort              string
	DatabaseURL             string
	CredentialEncryptionKey string
	GitHubAppID             string
	GitHubAppSlug           string
	GitHubAppPrivateKeyPath string
	GitHubAppPrivateKeyB64  string
	GitHubAppWebhookSecret  string
	GitHubAppInstallURL     string
	GitHubAppCallbackURL    string
	GitHubWebhookURL        string
}

func Load() (*Config, error) {
	cfg := &Config{
		GRPCPort:                getEnv("INTEGRATION_SERVICE_PORT", "9004"),
		HealthPort:              getEnv("INTEGRATION_SERVICE_HEALTH_PORT", "8084"),
		DatabaseURL:             getEnv("DATABASE_URL", ""),
		CredentialEncryptionKey: getEnv("CREDENTIAL_ENCRYPTION_KEY", ""),
		GitHubAppID:             getEnv("GITHUB_APP_ID", ""),
		GitHubAppSlug:           getEnv("GITHUB_APP_SLUG", ""),
		GitHubAppPrivateKeyPath: getEnv("GITHUB_APP_PRIVATE_KEY_PATH", ""),
		GitHubAppPrivateKeyB64:  getEnv("GITHUB_APP_PRIVATE_KEY_BASE64", ""),
		GitHubAppWebhookSecret:  getEnv("GITHUB_APP_WEBHOOK_SECRET", ""),
		GitHubAppInstallURL:     getEnv("GITHUB_APP_INSTALL_URL", ""),
		GitHubAppCallbackURL:    getEnv("GITHUB_APP_CALLBACK_URL", ""),
		GitHubWebhookURL:        getEnv("GITHUB_WEBHOOK_URL", ""),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func (c *Config) GitHubConfigured() bool {
	hasPrivateKey := c.GitHubAppPrivateKeyPath != "" || c.GitHubAppPrivateKeyB64 != ""
	return c.GitHubAppID != "" &&
		c.GitHubAppInstallURL != "" &&
		c.GitHubAppCallbackURL != "" &&
		c.GitHubWebhookURL != "" &&
		c.GitHubAppWebhookSecret != "" &&
		hasPrivateKey
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
