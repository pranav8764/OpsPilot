package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ── Scripted database ────────────────────────────────────────────────────────

// fakeDB is a scripted dbConn. Statements are matched by a distinctive substring
// of their SQL so a test only has to describe the queries it actually cares about;
// anything unscripted answers "no rows" or "nothing updated". Every call is
// recorded, which is what lets the revocation tests assert both that the right
// statement ran and that only stored identifiers were ever passed as arguments.
type fakeDB struct {
	rowScript  []scriptedRow
	rowsScript []scriptedRows
	execScript []scriptedExec

	calls []recordedCall
}

type recordedCall struct {
	sql  string
	args []any
}

type scriptedRow struct {
	match  string
	values []any
	err    error
}

type scriptedRows struct {
	match string
	rows  [][]any
	err   error
}

type scriptedExec struct {
	match string
	rows  int64
	err   error
}

func (f *fakeDB) record(sql string, args []any) {
	f.calls = append(f.calls, recordedCall{sql: sql, args: args})
}

// callsMatching returns every recorded statement whose SQL contains match.
func (f *fakeDB) callsMatching(match string) []recordedCall {
	var found []recordedCall
	for _, call := range f.calls {
		if strings.Contains(call.sql, match) {
			found = append(found, call)
		}
	}
	return found
}

func (f *fakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errors.New("fakeDB: Begin is not scripted")
}

func (f *fakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.record(sql, args)
	for _, scripted := range f.execScript {
		if strings.Contains(sql, scripted.match) {
			if scripted.err != nil {
				return pgconn.CommandTag{}, scripted.err
			}
			return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", scripted.rows)), nil
		}
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

func (f *fakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.record(sql, args)
	for _, scripted := range f.rowsScript {
		if strings.Contains(sql, scripted.match) {
			if scripted.err != nil {
				return nil, scripted.err
			}
			return &fakeRows{rows: scripted.rows}, nil
		}
	}
	return &fakeRows{}, nil
}

func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.record(sql, args)
	for _, scripted := range f.rowScript {
		if strings.Contains(sql, scripted.match) {
			return &fakeRow{values: scripted.values, err: scripted.err}
		}
	}
	return &fakeRow{err: pgx.ErrNoRows}
}

type fakeRow struct {
	values []any
	err    error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(r.values, dest)
}

type fakeRows struct {
	rows   [][]any
	cursor int
	err    error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return r.rows[r.cursor-1], nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRows) Next() bool {
	if r.cursor >= len(r.rows) {
		return false
	}
	r.cursor++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.cursor == 0 || r.cursor > len(r.rows) {
		return errors.New("fakeRows: Scan called outside a row")
	}
	return scanInto(r.rows[r.cursor-1], dest)
}

// scanInto copies scripted values into scan destinations. Only the destination
// types this service actually scans are supported; anything else fails loudly so a
// silently wrong test cannot pass.
func scanInto(values []any, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("fakeDB: scripted %d values for %d destinations", len(values), len(dest))
	}
	for i, target := range dest {
		value := values[i]
		switch typed := target.(type) {
		case *string:
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("fakeDB: column %d is %T, not string", i, value)
			}
			*typed = text
		case *int64:
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("fakeDB: column %d is %T, not int64", i, value)
			}
			*typed = number
		case *bool:
			flag, ok := value.(bool)
			if !ok {
				return fmt.Errorf("fakeDB: column %d is %T, not bool", i, value)
			}
			*typed = flag
		case **int64:
			if value == nil {
				*typed = nil
				continue
			}
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("fakeDB: column %d is %T, not int64 or nil", i, value)
			}
			copied := number
			*typed = &copied
		default:
			return fmt.Errorf("fakeDB: unsupported scan destination %T", target)
		}
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// SQL fragments unique to each statement under test.
const (
	sqlWorkspaceRole      = "FROM workspace_members"
	sqlInstallationLookup = "SELECT installation_id, app_id FROM github_installations"
	sqlRevokeByRowID      = "WHERE id = $1 AND workspace_id = $2 AND status <> 'revoked'"
	sqlRevokeByGitHubID   = "WHERE installation_id = $1 AND status <> 'revoked'"
	sqlRevokeRepoIDs      = "rc.repository_id = ANY($2::bigint[])"
	sqlReconcile          = "NOT (repository_id = ANY($3::bigint[]))"
	sqlStoredRepositories = "ORDER BY full_name"
	sqlWebhookAudit       = "'github.webhook.received'"
)

const (
	testWorkspaceID       = "a2bf5612-53ad-4735-b187-5c4b485e9c56"
	testInstallationRowID = "041f8e7c-b0cb-49e6-a2cd-82eca266b9bc"
	testUserID            = "6f1c0f2a-1c1b-4a1f-8a1f-2f4f8a1c0f2a"
	testInstallationID    = int64(142620052)
	testConfiguredAppID   = "4464433"
)

func listRepositoriesRequest() *integrationv1.ListRepositoriesRequest {
	return &integrationv1.ListRepositoriesRequest{
		WorkspaceId:          testWorkspaceID,
		GithubInstallationId: testInstallationRowID,
		UserId:               testUserID,
	}
}

// installationScript wires the two lookups every ListRepositories call performs:
// the workspace role check and the installation row. storedAppID is nil for a
// legacy row that predates the App binding.
func installationScript(storedAppID any) []scriptedRow {
	return []scriptedRow{
		{match: sqlWorkspaceRole, values: []any{"owner"}},
		{match: sqlInstallationLookup, values: []any{testInstallationID, storedAppID}},
	}
}

func requireGRPCCode(t *testing.T, err error, want codes.Code) *status.Status {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s error, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected a gRPC status error, got %v", err)
	}
	if st.Code() != want {
		t.Fatalf("expected code %s, got %s (%s)", want, st.Code(), st.Message())
	}
	return st
}

func requireCallCount(t *testing.T, db *fakeDB, match string, want int) []recordedCall {
	t.Helper()
	found := db.callsMatching(match)
	if len(found) != want {
		t.Fatalf("expected %d statements matching %q, got %d", want, match, len(found))
	}
	return found
}

// ── App binding ──────────────────────────────────────────────────────────────

func TestGithubAppIDMismatch(t *testing.T) {
	appID := func(v int64) *int64 { return &v }

	cases := []struct {
		name       string
		stored     *int64
		configured string
		want       bool
	}{
		// The live failure: installation minted under a previous App.
		{"different app is a mismatch", appID(1234567), "4464433", true},
		{"same app is not a mismatch", appID(4464433), "4464433", false},
		// Rows written before migration 008 never recorded a binding. Treating
		// unknown as wrong would revoke every legacy installation on first read.
		{"null app id is unverified, not a mismatch", nil, "4464433", false},
		{"null app id stays unverified even when unconfigured", nil, "", false},
		// With nothing trustworthy to compare against we defer to GitHub.
		{"unconfigured app id cannot mismatch", appID(1234567), "", false},
		{"unparseable app id cannot mismatch", appID(1234567), "not-a-number", false},
		{"zero configured app id cannot mismatch", appID(1234567), "0", false},
		{"whitespace is tolerated", appID(4464433), "  4464433\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := githubAppIDMismatch(tc.stored, tc.configured); got != tc.want {
				t.Fatalf("githubAppIDMismatch = %v, want %v", got, tc.want)
			}
		})
	}
}

// The exact production failure: installation 142620052 was created under a
// previous App, so the token endpoint 404s. The read must stop being an opaque
// 500 and must leave the rows marked revoked.
func TestListRepositoriesRevokesInstallationWhenTokenIs404(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	db := &fakeDB{rowScript: installationScript(nil)}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	resp, err := server.ListRepositories(context.Background(), listRepositoriesRequest())
	if resp != nil {
		t.Fatal("expected no response body on a failed precondition")
	}
	st := requireGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(st.Message(), "reconnect required") {
		t.Fatalf("error message is not actionable: %s", st.Message())
	}

	revokes := requireCallCount(t, db, sqlRevokeByRowID, 1)
	if !argsContain(revokes[0].args, testInstallationRowID) || !argsContain(revokes[0].args, testWorkspaceID) {
		t.Fatalf("revocation is not tenant-scoped: %v", revokes[0].args)
	}
	if !argsContain(revokes[0].args, revokeReasonTokenNotFound) {
		t.Fatalf("expected reason %q, got %v", revokeReasonTokenNotFound, revokes[0].args)
	}
	// Status only. A revocation that deleted rows would lose the indexed chunks
	// that a reconnect is supposed to reuse.
	assertNoDestructiveWrites(t, db)
}

// A stored binding that names another App is detectable without spending a round
// trip on GitHub, so the request must fail before the token mint.
func TestListRepositoriesRevokesOnAppIDMismatchWithoutCallingGitHub(t *testing.T) {
	var githubCalls int
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		githubCalls++
		http.Error(w, "unexpected", http.StatusTeapot)
	})

	db := &fakeDB{rowScript: installationScript(int64(1234567))}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	_, err := server.ListRepositories(context.Background(), listRepositoriesRequest())
	requireGRPCCode(t, err, codes.FailedPrecondition)
	if githubCalls != 0 {
		t.Fatalf("a known mismatch must not reach GitHub, saw %d calls", githubCalls)
	}

	revokes := requireCallCount(t, db, sqlRevokeByRowID, 1)
	if !argsContain(revokes[0].args, revokeReasonAppMismatch) {
		t.Fatalf("expected reason %q, got %v", revokeReasonAppMismatch, revokes[0].args)
	}
	assertNoDestructiveWrites(t, db)
}

// Legacy rows carry no binding. They must keep working, with GitHub as the
// authority, instead of being revoked on sight.
func TestListRepositoriesTreatsNullAppIDAsUnverified(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"ghs_installationtoken"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"repositories": []map[string]any{
				{"id": 1, "name": "ParkIntel", "full_name": "pranav8764/ParkIntel", "default_branch": "main"},
			},
		})
	})

	db := &fakeDB{rowScript: installationScript(nil)}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	resp, err := server.ListRepositories(context.Background(), listRepositoriesRequest())
	if err != nil {
		t.Fatalf("a NULL app_id must not fail the read: %v", err)
	}
	if len(resp.Repositories) != 1 || resp.Repositories[0].FullName != "pranav8764/ParkIntel" {
		t.Fatalf("unexpected repositories: %+v", resp.Repositories)
	}
	if got := db.callsMatching(sqlRevokeByRowID); len(got) != 0 {
		t.Fatalf("a NULL app_id must not trigger revocation, saw %d", len(got))
	}
}

// Reconcile-on-read is what enforces "remove a repo from the App and it
// disappears from the frontend" while webhooks are inactive locally.
func TestListRepositoriesReconcilesAgainstLiveRepositoryList(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"ghs_installationtoken"}`)
			return
		}
		// SentinelAPI (id 2) was removed from the installation.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"repositories": []map[string]any{
				{"id": 1, "name": "ParkIntel", "full_name": "pranav8764/ParkIntel", "default_branch": "main"},
			},
		})
	})

	db := &fakeDB{
		rowScript:  installationScript(int64(4464433)),
		execScript: []scriptedExec{{match: sqlReconcile, rows: 1}},
	}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	if _, err := server.ListRepositories(context.Background(), listRepositoriesRequest()); err != nil {
		t.Fatalf("ListRepositories failed: %v", err)
	}

	reconciles := requireCallCount(t, db, sqlReconcile, 1)
	args := reconciles[0].args
	if args[0] != testWorkspaceID || args[1] != testInstallationRowID {
		t.Fatalf("reconciliation is not tenant-scoped: %v", args)
	}
	liveIDs, ok := args[2].([]int64)
	if !ok || !reflect.DeepEqual(liveIDs, []int64{1}) {
		t.Fatalf("expected the live repository ids [1], got %#v", args[2])
	}
	assertNoDestructiveWrites(t, db)
}

// An installation that genuinely grants nothing must revoke everything, and the
// empty id list must be a real empty array rather than a nil that encodes as SQL
// NULL and quietly matches no rows.
func TestListRepositoriesReconcilesWithEmptyLiveList(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"ghs_installationtoken"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "repositories": []map[string]any{}})
	})

	db := &fakeDB{rowScript: installationScript(nil)}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	if _, err := server.ListRepositories(context.Background(), listRepositoriesRequest()); err != nil {
		t.Fatalf("ListRepositories failed: %v", err)
	}
	reconciles := requireCallCount(t, db, sqlReconcile, 1)
	liveIDs, ok := reconciles[0].args[2].([]int64)
	if !ok || liveIDs == nil || len(liveIDs) != 0 {
		t.Fatalf("expected a non-nil empty id list, got %#v", reconciles[0].args[2])
	}
}

// A GitHub outage must never be read as "the user removed every repository".
func TestListRepositoriesFailsOpenWhenGitHubListingFails(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"ghs_installationtoken"}`)
			return
		}
		http.Error(w, `{"message":"upstream is down"}`, http.StatusBadGateway)
	})

	db := &fakeDB{
		rowScript: installationScript(nil),
		rowsScript: []scriptedRows{{match: sqlStoredRepositories, rows: [][]any{
			{int64(1), "pranav8764", "ParkIntel", "pranav8764/ParkIntel", "https://github.test/pranav8764/ParkIntel", "https://github.test/pranav8764/ParkIntel.git", "main", false},
			{int64(2), "pranav8764", "SentinelAPI", "pranav8764/SentinelAPI", "https://github.test/pranav8764/SentinelAPI", "https://github.test/pranav8764/SentinelAPI.git", "main", true},
		}}},
	}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	resp, err := server.ListRepositories(context.Background(), listRepositoriesRequest())
	if err != nil {
		t.Fatalf("an outage must not fail the read: %v", err)
	}
	if len(resp.Repositories) != 2 {
		t.Fatalf("expected the stored repositories to be served, got %+v", resp.Repositories)
	}
	if resp.Repositories[1].FullName != "pranav8764/SentinelAPI" || !resp.Repositories[1].Private {
		t.Fatalf("stored repository fields were not preserved: %+v", resp.Repositories[1])
	}
	// The load-bearing assertion: nothing may be revoked off the back of a failure.
	if got := db.callsMatching(sqlReconcile); len(got) != 0 {
		t.Fatalf("reconciliation ran on an errored listing, %d statements", len(got))
	}
	if got := db.callsMatching(sqlRevokeByRowID); len(got) != 0 {
		t.Fatalf("an outage must not revoke the installation, %d statements", len(got))
	}
}

// If GitHub is unreachable and the stored fallback also fails there is nothing
// left to serve, and the caller must be told rather than handed an empty list.
func TestListRepositoriesReportsWhenFallbackAlsoFails(t *testing.T) {
	withGitHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"ghs_installationtoken"}`)
			return
		}
		http.Error(w, "down", http.StatusBadGateway)
	})

	db := &fakeDB{
		rowScript:  installationScript(nil),
		rowsScript: []scriptedRows{{match: sqlStoredRepositories, err: errors.New("connection refused")}},
	}
	server := &IntegrationServer{db: db, cfg: testConfig(t, testConfiguredAppID), logger: discardLogger()}

	_, err := server.ListRepositories(context.Background(), listRepositoriesRequest())
	requireGRPCCode(t, err, codes.Internal)
}

// ── Webhooks ─────────────────────────────────────────────────────────────────

func signedWebhook(t *testing.T, secret string, payload any) (*integrationv1.HandleGitHubWebhookRequest, string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return &integrationv1.HandleGitHubWebhookRequest{
		Payload:   body,
		Signature: "sha256=" + hex.EncodeToString(mac.Sum(nil)),
	}, string(body)
}

func webhookServer(t *testing.T, db *fakeDB) *IntegrationServer {
	t.Helper()
	cfg := testConfig(t, testConfiguredAppID)
	cfg.GitHubAppWebhookSecret = "test-webhook-secret"
	return &IntegrationServer{db: db, cfg: cfg, logger: discardLogger()}
}

func TestWebhookRevokesRemovedRepositories(t *testing.T) {
	db := &fakeDB{}
	server := webhookServer(t, db)
	req, _ := signedWebhook(t, server.cfg.GitHubAppWebhookSecret, map[string]any{
		"action":       "removed",
		"installation": map[string]any{"id": testInstallationID},
		"repositories_removed": []map[string]any{
			{"id": 2, "full_name": "pranav8764/SentinelAPI"},
		},
	})
	req.EventType = "installation_repositories"

	resp, err := server.HandleGitHubWebhook(context.Background(), req)
	if err != nil || !resp.Success {
		t.Fatalf("webhook handling failed: %v", err)
	}

	requireCallCount(t, db, sqlWebhookAudit, 1)
	revokes := requireCallCount(t, db, sqlRevokeRepoIDs, 1)
	if revokes[0].args[0] != testInstallationID {
		t.Fatalf("expected the installation id as the selector, got %v", revokes[0].args[0])
	}
	repositoryIDs, ok := revokes[0].args[1].([]int64)
	if !ok || !reflect.DeepEqual(repositoryIDs, []int64{2}) {
		t.Fatalf("expected repository ids [2], got %#v", revokes[0].args[1])
	}
	assertNoDestructiveWrites(t, db)
}

// The payload is data, not instructions. Names and prose in the body must never
// reach a query: only numeric ids may be used, and only to select existing rows.
func TestWebhookNeverSelectsRowsByPayloadProse(t *testing.T) {
	db := &fakeDB{}
	server := webhookServer(t, db)
	req, _ := signedWebhook(t, server.cfg.GitHubAppWebhookSecret, map[string]any{
		"action":       "removed",
		"installation": map[string]any{"id": testInstallationID, "app_id": 999},
		"repositories_removed": []map[string]any{
			{"id": 2, "full_name": "attacker/evil", "name": "evil"},
		},
		"sender":       map[string]any{"login": "attacker"},
		"workspace_id": "11111111-1111-1111-1111-111111111111",
	})
	req.EventType = "installation_repositories"

	if _, err := server.HandleGitHubWebhook(context.Background(), req); err != nil {
		t.Fatalf("webhook handling failed: %v", err)
	}

	forbidden := []string{"attacker/evil", "attacker", "evil", "11111111-1111-1111-1111-111111111111"}
	for _, call := range db.calls {
		for _, arg := range call.args {
			text, ok := arg.(string)
			if !ok {
				continue
			}
			for _, banned := range forbidden {
				if strings.Contains(text, banned) {
					t.Fatalf("payload prose %q reached a query argument: %q", banned, text)
				}
			}
		}
	}
}

func TestWebhookRevokesDeletedAndSuspendedInstallations(t *testing.T) {
	cases := []struct {
		action string
		reason string
	}{
		{"deleted", revokeReasonWebhookDeleted},
		{"suspend", revokeReasonWebhookSuspended},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			db := &fakeDB{}
			server := webhookServer(t, db)
			req, _ := signedWebhook(t, server.cfg.GitHubAppWebhookSecret, map[string]any{
				"action":       tc.action,
				"installation": map[string]any{"id": testInstallationID},
			})
			req.EventType = "installation"

			if _, err := server.HandleGitHubWebhook(context.Background(), req); err != nil {
				t.Fatalf("webhook handling failed: %v", err)
			}

			revokes := requireCallCount(t, db, sqlRevokeByGitHubID, 1)
			if revokes[0].args[0] != testInstallationID {
				t.Fatalf("expected the installation id as the selector, got %v", revokes[0].args[0])
			}
			if revokes[0].args[1] != tc.reason {
				t.Fatalf("expected reason %q, got %v", tc.reason, revokes[0].args[1])
			}
			assertNoDestructiveWrites(t, db)
		})
	}
}

// Only the two revoking cases may touch state. Everything else is audited and
// dropped; in particular no webhook may ever restore access.
func TestWebhookIgnoresEveryOtherEvent(t *testing.T) {
	cases := []struct{ eventType, action string }{
		{"installation", "created"},
		{"installation", "unsuspend"},
		{"installation", "new_permissions_accepted"},
		{"installation_repositories", "added"},
		{"push", "removed"},
		{"repository", "deleted"},
	}

	for _, tc := range cases {
		t.Run(tc.eventType+"."+tc.action, func(t *testing.T) {
			db := &fakeDB{}
			server := webhookServer(t, db)
			req, _ := signedWebhook(t, server.cfg.GitHubAppWebhookSecret, map[string]any{
				"action":               tc.action,
				"installation":         map[string]any{"id": testInstallationID},
				"repositories_removed": []map[string]any{{"id": 2}},
				"repositories_added":   []map[string]any{{"id": 3}},
			})
			req.EventType = tc.eventType

			if _, err := server.HandleGitHubWebhook(context.Background(), req); err != nil {
				t.Fatalf("webhook handling failed: %v", err)
			}

			requireCallCount(t, db, sqlWebhookAudit, 1)
			if len(db.calls) != 1 {
				t.Fatalf("expected the audit row to be the only statement, got %d", len(db.calls))
			}
		})
	}
}

// A body GitHub never signed must change nothing, and must not even be audited.
func TestWebhookWithBadSignatureTouchesNothing(t *testing.T) {
	db := &fakeDB{}
	server := webhookServer(t, db)
	req, _ := signedWebhook(t, "the-wrong-secret", map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": testInstallationID},
	})
	req.EventType = "installation"

	_, err := server.HandleGitHubWebhook(context.Background(), req)
	requireGRPCCode(t, err, codes.Unauthenticated)
	if len(db.calls) != 0 {
		t.Fatalf("an unverified delivery must not reach the database, saw %d statements", len(db.calls))
	}
}

// An undecodable body is not a server fault: audit it and move on, rather than
// asking GitHub to redeliver something that will never parse.
func TestWebhookWithUndecodablePayloadSucceedsWithoutWrites(t *testing.T) {
	db := &fakeDB{}
	server := webhookServer(t, db)
	payload := []byte("not-json")
	mac := hmac.New(sha256.New, []byte(server.cfg.GitHubAppWebhookSecret))
	mac.Write(payload)
	req := &integrationv1.HandleGitHubWebhookRequest{
		EventType: "installation",
		Payload:   payload,
		Signature: "sha256=" + hex.EncodeToString(mac.Sum(nil)),
	}

	resp, err := server.HandleGitHubWebhook(context.Background(), req)
	if err != nil || !resp.Success {
		t.Fatalf("expected an undecodable body to be tolerated: %v", err)
	}
	requireCallCount(t, db, sqlWebhookAudit, 1)
	if len(db.calls) != 1 {
		t.Fatalf("expected no revocation statements, got %d calls", len(db.calls))
	}
}

// Revocation is idempotent, so a database failure should surface and let GitHub
// redeliver rather than be swallowed.
func TestWebhookRevocationFailurePropagates(t *testing.T) {
	db := &fakeDB{execScript: []scriptedExec{{match: sqlRevokeByGitHubID, err: errors.New("deadlock detected")}}}
	server := webhookServer(t, db)
	req, _ := signedWebhook(t, server.cfg.GitHubAppWebhookSecret, map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": testInstallationID},
	})
	req.EventType = "installation"

	_, err := server.HandleGitHubWebhook(context.Background(), req)
	st := requireGRPCCode(t, err, codes.Internal)
	if strings.Contains(st.Message(), "deadlock") {
		t.Fatalf("database detail leaked to the caller: %s", st.Message())
	}
}

// ── Shared assertions ────────────────────────────────────────────────────────

// argsContain reports whether any argument equals want.
func argsContain(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

// assertNoDestructiveWrites enforces the invariant that revocation is a
// reversible status change. Nothing on these paths may delete a connection, an
// installation, or an indexed chunk.
func assertNoDestructiveWrites(t *testing.T, db *fakeDB) {
	t.Helper()
	for _, call := range db.calls {
		upper := strings.ToUpper(call.sql)
		if strings.Contains(upper, "DELETE FROM REPOSITORY_CONNECTIONS") ||
			strings.Contains(upper, "DELETE FROM GITHUB_INSTALLATIONS") ||
			strings.Contains(upper, "TRUNCATE") {
			t.Fatalf("revocation must never destroy rows: %s", call.sql)
		}
	}
}
