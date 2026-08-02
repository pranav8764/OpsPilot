package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/opspilot/integration-service/internal/config"
)

var (
	testKeyOnce sync.Once
	testKeyB64  string
)

// testPrivateKeyB64 returns a throwaway RSA key in the same base64-PEM shape the
// service reads from GITHUB_APP_PRIVATE_KEY_BASE64. Generated once because RSA
// keygen dominates the runtime of these tests otherwise.
func testPrivateKeyB64(t *testing.T) string {
	t.Helper()
	testKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate test key: %v", err)
		}
		encoded := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		testKeyB64 = base64.StdEncoding.EncodeToString(encoded)
	})
	return testKeyB64
}

// withGitHubStub points the package-level GitHub client at a stub server for the
// duration of one test.
func withGitHubStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	stub := httptest.NewServer(handler)
	previousBaseURL := githubAPIBaseURL
	previousClient := githubHTTPClient
	githubAPIBaseURL = stub.URL
	githubHTTPClient = stub.Client()
	t.Cleanup(func() {
		githubAPIBaseURL = previousBaseURL
		githubHTTPClient = previousClient
		stub.Close()
	})
	return stub
}

func testConfig(t *testing.T, appID string) *config.Config {
	t.Helper()
	return &config.Config{
		GitHubAppID:            appID,
		GitHubAPIVersion:       "2026-03-10",
		GitHubAppPrivateKeyB64: testPrivateKeyB64(t),
	}
}

// A 404 from the token endpoint is the only way GitHub reports "this installation
// belongs to a different App". It has to be distinguishable from a transient
// failure, because retrying it can never succeed.
func TestGetInstallationTokenReturns404AsSentinel(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	server := &IntegrationServer{cfg: testConfig(t, "4464433")}
	_, err := server.getInstallationToken(142620052)
	if !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("expected ErrInstallationNotFound, got %v", err)
	}
}

func TestGetInstallationTokenKeepsOtherFailuresGeneric(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	})

	server := &IntegrationServer{cfg: testConfig(t, "4464433")}
	_, err := server.getInstallationToken(142620052)
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if errors.Is(err, ErrInstallationNotFound) {
		t.Fatal("a 401 must not be reported as a missing installation; it is transient/credential state")
	}
}

// The error surface must never carry GitHub's raw response body.
func TestGetInstallationTokenErrorLeaksNoResponseBody(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"secret-detail","token":"ghs_supersecret"}`, http.StatusInternalServerError)
	})

	server := &IntegrationServer{cfg: testConfig(t, "4464433")}
	_, err := server.getInstallationToken(142620052)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "ghs_") || strings.Contains(err.Error(), "secret-detail") {
		t.Fatalf("error leaked the GitHub response body: %v", err)
	}
}

// app_id used to be dropped at decode, which is what made the App rotation
// undetectable in the first place.
func TestFetchInstallationMetadataCapturesAppID(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": 142620052,
			"app_id": 4464433,
			"account": {"login": "pranav8764", "id": 42, "type": "User"},
			"repository_selection": "selected",
			"permissions": {"contents": "read"}
		}`)
	})

	server := &IntegrationServer{cfg: testConfig(t, "4464433")}
	meta, err := server.fetchInstallationMetadata(142620052)
	if err != nil {
		t.Fatalf("fetchInstallationMetadata failed: %v", err)
	}
	if meta.AppID != 4464433 {
		t.Fatalf("expected app_id 4464433, got %d", meta.AppID)
	}
	if meta.Account.Login != "pranav8764" {
		t.Fatalf("unexpected account login: %s", meta.Account.Login)
	}
}

func TestListInstallationRepositoriesPaginatesAndHonoursContext(t *testing.T) {
	var pages []string
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"repositories": []map[string]any{
				{"id": 1, "name": "ParkIntel", "full_name": "pranav8764/ParkIntel", "default_branch": "main"},
				{"id": 2, "name": "SentinelAPI", "full_name": "pranav8764/SentinelAPI", "default_branch": "main"},
			},
		})
	})

	server := &IntegrationServer{cfg: testConfig(t, "4464433")}
	repos, err := server.listInstallationRepositories(context.Background(), "installation-token")
	if err != nil {
		t.Fatalf("listInstallationRepositories failed: %v", err)
	}
	if len(repos) != 2 || len(pages) != 1 {
		t.Fatalf("expected 2 repositories from 1 page, got %d repositories over %d pages", len(repos), len(pages))
	}

	// A cancelled context must abort rather than block the caller on GitHub.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.listInstallationRepositories(cancelled, "installation-token"); err == nil {
		t.Fatal("expected a cancelled context to fail the listing")
	}
}
