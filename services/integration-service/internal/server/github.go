package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var githubHTTPClient = &http.Client{Timeout: 30 * time.Second}
var githubAPIBaseURL = "https://api.github.com"

// ErrInstallationNotFound reports that GitHub does not recognise an installation
// id as belonging to the App this service authenticates as. An installation id is
// only meaningful to the App that created it, so rotating GITHUB_APP_ID turns
// every previously stored installation into a 404 at the token endpoint. Callers
// must branch on this with errors.Is instead of treating it as a transient
// outage: retrying can never succeed, only reconnecting under the current App can.
var ErrInstallationNotFound = errors.New("github installation is not accessible to the configured github app")

type githubInstallationMeta struct {
	ID int64 `json:"id"`
	// AppID binds the installation to the GitHub App that created it. Persisting
	// it is what lets the service detect an App rotation locally, before it spends
	// a round trip discovering the binding is stale.
	AppID   int64 `json:"app_id"`
	Account struct {
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

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opspilot-integration-service")
	req.Header.Set("X-GitHub-Api-Version", s.cfg.GitHubAPIVersion)

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 404 is the App-binding signal, not a generic failure: GitHub hides
	// installations that belong to a different App behind "not found". Surface it
	// as a sentinel so callers can revoke locally instead of retrying forever.
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrInstallationNotFound
	}
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

	url := fmt.Sprintf("%s/app/installations/%d", githubAPIBaseURL, installationID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opspilot-integration-service")
	req.Header.Set("X-GitHub-Api-Version", s.cfg.GitHubAPIVersion)

	resp, err := githubHTTPClient.Do(req)
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

// listInstallationRepositories returns every repository the installation grants
// access to, following GitHub's pagination. Bound to a caller-supplied context so
// a request path can cap how long it waits on GitHub rather than inheriting only
// the client-wide timeout.
func (s *IntegrationServer) listInstallationRepositories(ctx context.Context, token string) ([]githubRepoMeta, error) {
	var repositories []githubRepoMeta
	for page := 1; page <= 100; page++ {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", githubAPIBaseURL, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "opspilot-integration-service")
		req.Header.Set("X-GitHub-Api-Version", s.cfg.GitHubAPIVersion)

		resp, err := githubHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status code fetching repositories: %d", resp.StatusCode)
		}

		var res githubReposResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		repositories = append(repositories, res.Repositories...)
		if len(res.Repositories) < 100 || len(repositories) >= res.TotalCount {
			return repositories, nil
		}
	}
	return nil, fmt.Errorf("repository pagination exceeded safety limit")
}
