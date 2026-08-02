package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
)

const (
	sqlSessionSweep  = "DELETE FROM github_install_sessions"
	sqlSessionInsert = "INSERT INTO github_install_sessions"
)

func installSessionServer(t *testing.T, db *fakeDB) *IntegrationServer {
	t.Helper()
	cfg := testConfig(t, testConfiguredAppID)
	cfg.GitHubAppWebhookSecret = "test-webhook-secret"
	cfg.GitHubAppInstallURL = "https://github.com/apps/archiatros-sre/installations/new"
	cfg.GitHubAppCallbackURL = "http://localhost:8080/api/v1/integrations/github/callback"
	cfg.GitHubWebhookURL = "http://localhost:8080/api/v1/integrations/github/webhook"
	return &IntegrationServer{db: db, cfg: cfg, logger: discardLogger()}
}

func createSessionRequest() *integrationv1.CreateGitHubInstallSessionRequest {
	return &integrationv1.CreateGitHubInstallSessionRequest{
		WorkspaceId: testWorkspaceID,
		UserId:      testUserID,
	}
}

// Abandoned install sessions are never consumed, so without a sweep they
// accumulate forever behind the partial index from migration 005.
func TestCreateGitHubInstallSessionSweepsExpiredRows(t *testing.T) {
	db := &fakeDB{rowScript: []scriptedRow{{match: sqlWorkspaceRole, values: []any{"owner"}}}}
	server := installSessionServer(t, db)

	resp, err := server.CreateGitHubInstallSession(context.Background(), createSessionRequest())
	if err != nil {
		t.Fatalf("CreateGitHubInstallSession failed: %v", err)
	}
	if !strings.HasPrefix(resp.InstallUrl, "https://github.com/apps/archiatros-sre/") {
		t.Fatalf("unexpected install url: %s", resp.InstallUrl)
	}

	sweeps := requireCallCount(t, db, sqlSessionSweep, 1)
	// Only expired and still-unconsumed rows may go. Consumed rows are the audit
	// trail of a real installation and must survive.
	if !strings.Contains(sweeps[0].sql, "consumed_at IS NULL") || !strings.Contains(sweeps[0].sql, "expires_at < NOW()") {
		t.Fatalf("sweep predicate is too broad: %s", sweeps[0].sql)
	}
	if len(sweeps[0].args) != 0 {
		t.Fatalf("sweep should take no arguments, got %v", sweeps[0].args)
	}
	requireCallCount(t, db, sqlSessionInsert, 1)
}

// Housekeeping must never block a user from starting an install.
func TestCreateGitHubInstallSessionSurvivesFailedSweep(t *testing.T) {
	db := &fakeDB{
		rowScript:  []scriptedRow{{match: sqlWorkspaceRole, values: []any{"owner"}}},
		execScript: []scriptedExec{{match: sqlSessionSweep, err: errors.New("lock timeout")}},
	}
	server := installSessionServer(t, db)

	if _, err := server.CreateGitHubInstallSession(context.Background(), createSessionRequest()); err != nil {
		t.Fatalf("a failed sweep must not fail the request: %v", err)
	}
	requireCallCount(t, db, sqlSessionInsert, 1)
}

// The sweep runs after authorization, never before, so an unauthenticated caller
// cannot use it to touch the table at all.
func TestCreateGitHubInstallSessionSweepsOnlyAfterAuthorization(t *testing.T) {
	db := &fakeDB{rowScript: []scriptedRow{{match: sqlWorkspaceRole, values: []any{"viewer"}}}}
	server := installSessionServer(t, db)

	_, err := server.CreateGitHubInstallSession(context.Background(), createSessionRequest())
	requireGRPCCode(t, err, codes.PermissionDenied)
	if got := db.callsMatching(sqlSessionSweep); len(got) != 0 {
		t.Fatalf("an unauthorized caller must not reach the sweep, saw %d statements", len(got))
	}
}
