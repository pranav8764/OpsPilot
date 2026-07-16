package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opspilot/integration-service/internal/config"
)

func TestPublishFixPRCreatesOnlyNewDraftBranch(t *testing.T) {
	original := []byte("before\n")
	digest := sha256.Sum256(original)
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/main"):
			_, _ = io.WriteString(w, `{"object":{"sha":"base-commit"}}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/base-commit"):
			_, _ = io.WriteString(w, `{"sha":"base-commit","tree":{"sha":"base-tree"}}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/src/app.txt"):
			_ = json.NewEncoder(w).Encode(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(original)})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = io.WriteString(w, `{"sha":"blob-new"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			_, _ = io.WriteString(w, `{"sha":"tree-new"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = io.WriteString(w, `{"sha":"commit-new","tree":{"sha":"tree-new"}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "refs/heads/main") || !strings.Contains(string(body), "refs/heads/opspilot/fix-") {
				t.Fatalf("unsafe branch creation payload: %s", body)
			}
			_, _ = io.WriteString(w, `{"ref":"refs/heads/opspilot/fix-proposal1"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["draft"] != true {
				t.Fatal("fix pull request must always be a draft")
			}
			if payload["base"] != "main" {
				t.Fatalf("unexpected PR base: %v", payload["base"])
			}
			_, _ = io.WriteString(w, `{"html_url":"https://github.test/acme/api/pull/7","number":7}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	previousBaseURL := githubAPIBaseURL
	previousClient := githubHTTPClient
	githubAPIBaseURL = server.URL
	githubHTTPClient = server.Client()
	defer func() {
		githubAPIBaseURL = previousBaseURL
		githubHTTPClient = previousClient
	}()

	integrationServer := &IntegrationServer{cfg: &config.Config{GitHubAPIVersion: "2026-03-10"}}
	record := &fixApprovalRecord{
		ApprovalID: "approval-1", ProposalID: "proposal-1", IncidentID: "incident-1",
		IncidentTitle: "Fix the failure", Owner: "acme", Repository: "api",
		BaseBranch: "main", BaseCommitSHA: "base-commit", VerificationJSON: []byte(`{"verified_fix":false}`),
		Changes: []fixProposalChange{{
			Path: "src/app.txt", OriginalContentHash: hex.EncodeToString(digest[:]), NewContent: "after\n", Reason: "test",
		}},
	}
	result, err := integrationServer.publishFixPRWithToken(context.Background(), record, "installation-token")
	if err != nil {
		t.Fatalf("publishFixPRWithToken failed: %v", err)
	}
	if result.Number != 7 || !strings.Contains(result.Branch, "opspilot/fix-") {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "PATCH ") {
			t.Fatalf("adapter must never update an existing ref: %s", call)
		}
	}
}
