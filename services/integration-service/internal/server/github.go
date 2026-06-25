package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type githubInstallationMeta struct {
	ID        int64 `json:"id"`
	Account   struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"`
	} `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
}

type githubRepoMeta struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type githubReposResponse struct {
	TotalCount   int              `json:"total_count"`
	Repositories []githubRepoMeta `json:"repositories"`
}

type installationTokenResponse struct {
	Token string `json:"token"`
}

func (s *IntegrationServer) getPrivateKey() ([]byte, error) {
	if s.cfg.GitHubAppPrivateKeyB64 != "" {
		dec, err := base64.StdEncoding.DecodeString(s.cfg.GitHubAppPrivateKeyB64)
		if err == nil {
			return dec, nil
		}
	}
	if s.cfg.GitHubAppPrivateKeyPath != "" {
		dec, err := os.ReadFile(s.cfg.GitHubAppPrivateKeyPath)
		if err == nil {
			return dec, nil
		}
	}
	return nil, fmt.Errorf("no github app private key configured")
}

func (s *IntegrationServer) generateAppJWT() (string, error) {
	pkBytes, err := s.getPrivateKey()
	if err != nil {
		return "", err
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(pkBytes)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // backdate for clock drift
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": s.cfg.GitHubAppID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}

	return tokenString, nil
}

func (s *IntegrationServer) getInstallationToken(installationID int64) (string, error) {
	appJWT, err := s.generateAppJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opspilot-integration-service")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status code fetching token: %d", resp.StatusCode)
	}

	var res installationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	return res.Token, nil
}

func (s *IntegrationServer) fetchInstallationMetadata(installationID int64) (*githubInstallationMeta, error) {
	appJWT, err := s.generateAppJWT()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d", installationID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opspilot-integration-service")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching installation: %d", resp.StatusCode)
	}

	var res githubInstallationMeta
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return &res, nil
}

func (s *IntegrationServer) fetchInstallationRepositories(token string) ([]githubRepoMeta, error) {
	url := "https://api.github.com/installation/repositories"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opspilot-integration-service")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching repositories: %d", resp.StatusCode)
	}

	var res githubReposResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return res.Repositories, nil
}
