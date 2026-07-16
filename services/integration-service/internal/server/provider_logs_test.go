package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeProviderLogsHandlesVercelAndRailwayShapes(t *testing.T) {
	body := []byte(`{"data":{"deploymentLogs":[{"message":"database timeout","timestamp":"2026-07-13T10:00:00Z","severity":"error"}]}}`)
	content := normalizeProviderLogs(body, 10)
	if !strings.Contains(content, "ERROR") || !strings.Contains(content, "database timeout") {
		t.Fatalf("unexpected normalized logs: %q", content)
	}

	build := []byte(`[{"type":"stderr","created":1783936800000,"payload":{"text":"build failed"}}]`)
	content = normalizeProviderLogs(build, 10)
	if !strings.Contains(content, "build failed") {
		t.Fatalf("Vercel event text was not normalized: %q", content)
	}
}

func TestFetchVercelLogsUsesBearerAndEscapedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vercel-token" {
			t.Fatalf("missing bearer authentication: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/projects/project%20id/deployments/deploy%2Fid/runtime-logs" && r.URL.EscapedPath() != "/v1/projects/project%20id/deployments/deploy%2Fid/runtime-logs" {
			t.Fatalf("identifiers were not safely escaped: path=%q escaped=%q", r.URL.Path, r.URL.EscapedPath())
		}
		if r.URL.Query().Get("teamId") != "team-1" {
			t.Fatal("team scope was not forwarded")
		}
		_, _ = io.WriteString(w, `{"message":"runtime failure","level":"error","timestampInMs":1783936800000}`)
	}))
	defer server.Close()
	old := vercelAPIBaseURL
	vercelAPIBaseURL = server.URL
	defer func() { vercelAPIBaseURL = old }()

	content, err := fetchVercelLogs(context.Background(), "vercel-token", logProviderMetadata{
		ProjectID: "project id", DeploymentID: "deploy/id", ScopeID: "team-1",
	}, "runtime", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "runtime failure") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestFetchRailwayLogsSupportsProjectToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Project-Access-Token") != "railway-token" || r.Header.Get("Authorization") != "" {
			t.Fatal("Railway project token used the wrong header")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "deploymentLogs") || !strings.Contains(string(body), "deployment-1") {
			t.Fatalf("unexpected GraphQL request: %s", body)
		}
		_, _ = io.WriteString(w, `{"data":{"deploymentLogs":[{"message":"service crashed","timestamp":"2026-07-13T10:00:00Z","severity":"error"}]}}`)
	}))
	defer server.Close()
	old := railwayGraphQLURL
	railwayGraphQLURL = server.URL
	defer func() { railwayGraphQLURL = old }()

	content, err := fetchRailwayLogs(context.Background(), providerCredential{
		Token: "railway-token", TokenKind: "project_token",
	}, logProviderMetadata{DeploymentID: "deployment-1"}, "runtime", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "service crashed") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestProviderErrorsNeverIncludeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"secret-provider-details"}`)
	}))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := executeAndNormalizeProviderRequest(request, 10)
	if err == nil || strings.Contains(err.Error(), "secret-provider-details") {
		t.Fatalf("provider error leaked its response body: %v", err)
	}
}
