package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"github.com/opspilot/integration-service/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errInstallationAppMismatch is returned whenever the stored installation cannot
// belong to the App this service authenticates as. FailedPrecondition rather than
// Internal on purpose: nothing is broken server side, and the only resolution is a
// human reconnecting the App.
const errInstallationAppMismatch = "installation belongs to a different GitHub App; reconnect required"

// Revocation reasons are fixed server-side constants. Webhook payload prose never
// reaches this value, so a delivery cannot write arbitrary text into audit_logs.
const (
	revokeReasonAppMismatch       = "app_id_mismatch"
	revokeReasonTokenNotFound     = "installation_token_404"
	revokeReasonWebhookDeleted    = "webhook.installation.deleted"
	revokeReasonWebhookSuspended  = "webhook.installation.suspend"
	revokeReasonWebhookReposRemvd = "webhook.installation_repositories.removed"
)

// githubReadTimeout caps how long a read blocks on GitHub. Reconciliation runs on
// the request path, so an unreachable GitHub must degrade quickly to stored data
// rather than hold the caller open for the full HTTP client timeout.
const githubReadTimeout = 10 * time.Second

// dbConn is the narrow slice of *pgxpool.Pool this service actually uses.
// Depending on an interface rather than the concrete pool lets tests drive the
// revocation and reconciliation branches, which are pure database effects and
// would otherwise be reachable only against a live cluster.
type dbConn interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type IntegrationServer struct {
	integrationv1.UnimplementedIntegrationServiceServer
	db        dbConn
	cfg       *config.Config
	logger    *slog.Logger
	publisher EventPublisher
}

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

func New(db *pgxpool.Pool, cfg *config.Config, logger *slog.Logger, publisher EventPublisher) *IntegrationServer {
	return &IntegrationServer{db: db, cfg: cfg, logger: logger, publisher: publisher}
}

// githubAppIDMismatch reports whether a stored installation is bound to a
// different GitHub App than the one this service is configured as.
//
// A NULL app_id is deliberately not a mismatch. Rows written before migration 008
// predate the binding, so the App that minted them was never observed; treating
// "unknown" as "wrong" would revoke every legacy installation on first read. Those
// rows fall through to the token mint, which is authoritative, and a successful
// reconnect backfills the binding.
func githubAppIDMismatch(storedAppID *int64, configuredAppID string) bool {
	if storedAppID == nil {
		return false
	}
	configured, err := strconv.ParseInt(strings.TrimSpace(configuredAppID), 10, 64)
	if err != nil || configured <= 0 {
		// Unconfigured or unparseable app id: nothing trustworthy to compare
		// against, so defer to GitHub rather than guessing.
		return false
	}
	return *storedAppID != configured
}

// revokeInstallation marks one installation and every repository connection it
// backs as revoked, inside a single statement so the two tables can never
// disagree. Nothing is deleted: status is the only thing that moves, so indexed
// chunks, ingestion history, and connection metadata all survive a reconnect.
//
// The audit row is written only when something actually changed, which keeps a
// polling frontend from flooding audit_logs while an installation stays broken.
func (s *IntegrationServer) revokeInstallation(ctx context.Context, workspaceID, installationRowID, reason string) error {
	_, err := s.db.Exec(ctx, `
		WITH revoked_installation AS (
			UPDATE github_installations
			SET status = 'revoked', updated_at = NOW()
			WHERE id = $1 AND workspace_id = $2 AND status <> 'revoked'
			RETURNING id
		), revoked_connections AS (
			UPDATE repository_connections
			SET status = 'revoked', revoked_at = NOW(), updated_at = NOW()
			WHERE github_installation_id = $1 AND workspace_id = $2 AND status <> 'revoked'
			RETURNING id
		)
		INSERT INTO audit_logs (workspace_id, action, resource_type, resource_id, metadata)
		SELECT $2::uuid, 'github.installation.revoked', 'github_installation', $1::uuid,
			jsonb_build_object('reason', $3::text,
				'connections_revoked', (SELECT count(*) FROM revoked_connections))
		WHERE EXISTS (SELECT 1 FROM revoked_installation)
			OR EXISTS (SELECT 1 FROM revoked_connections)
	`, installationRowID, workspaceID, reason)
	return err
}

// revokeInstallationByGitHubID is the webhook-side revocation. The caller only
// knows GitHub's numeric installation id, so the workspace is derived from the
// stored row rather than taken from the payload: the payload selects, it never
// scopes. Keyed on installation_id so it stays correct under migration 004, which
// scoped uniqueness to (workspace_id, installation_id) and therefore lets one
// installation legitimately serve several of its owner's workspaces.
func (s *IntegrationServer) revokeInstallationByGitHubID(ctx context.Context, installationID int64, reason string) error {
	_, err := s.db.Exec(ctx, `
		WITH target AS (
			SELECT id, workspace_id FROM github_installations WHERE installation_id = $1
		), revoked_installation AS (
			UPDATE github_installations
			SET status = 'revoked', updated_at = NOW()
			WHERE installation_id = $1 AND status <> 'revoked'
			RETURNING id
		), revoked_connections AS (
			UPDATE repository_connections rc
			SET status = 'revoked', revoked_at = NOW(), updated_at = NOW()
			FROM target t
			WHERE rc.github_installation_id = t.id
				AND rc.workspace_id = t.workspace_id
				AND rc.status <> 'revoked'
			RETURNING rc.id
		)
		INSERT INTO audit_logs (workspace_id, action, resource_type, resource_id, metadata)
		SELECT t.workspace_id, 'github.installation.revoked', 'github_installation', t.id,
			jsonb_build_object('reason', $2::text, 'installation_id', $1::bigint,
				'connections_revoked', (SELECT count(*) FROM revoked_connections))
		FROM target t
		WHERE EXISTS (SELECT 1 FROM revoked_installation)
			OR EXISTS (SELECT 1 FROM revoked_connections)
	`, installationID, reason)
	return err
}

// revokeConnectionsByRepositoryIDs revokes only the named repositories of one
// installation. repositoryIDs comes from a webhook body, so it is used purely as a
// selector against rows that already exist; it cannot introduce a repository, move
// one between workspaces, or reach a connection the installation does not back.
func (s *IntegrationServer) revokeConnectionsByRepositoryIDs(ctx context.Context, installationID int64, repositoryIDs []int64) error {
	if len(repositoryIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		WITH target AS (
			SELECT id, workspace_id FROM github_installations WHERE installation_id = $1
		), revoked_connections AS (
			UPDATE repository_connections rc
			SET status = 'revoked', revoked_at = NOW(), updated_at = NOW()
			FROM target t
			WHERE rc.github_installation_id = t.id
				AND rc.workspace_id = t.workspace_id
				AND rc.repository_id = ANY($2::bigint[])
				AND rc.status <> 'revoked'
			RETURNING rc.id, rc.workspace_id
		)
		INSERT INTO audit_logs (workspace_id, action, resource_type, resource_id, metadata)
		SELECT rc.workspace_id, 'github.repository.revoked', 'repository_connection', rc.id,
			jsonb_build_object('reason', $3::text, 'installation_id', $1::bigint)
		FROM revoked_connections rc
	`, installationID, repositoryIDs, revokeReasonWebhookReposRemvd)
	return err
}

// reconcileRepositoryConnections revokes stored connections that the installation
// no longer grants access to. The App ships a disabled example.com webhook in local
// development, so a read is the only reliable moment to notice that a repository
// was removed from the installation and to stop showing it.
//
// Callers must only reach this with a live list they actually received. An errored
// or timed-out GitHub call is not an empty list, and must never be passed here.
func (s *IntegrationServer) reconcileRepositoryConnections(
	ctx context.Context,
	workspaceID string,
	installationRowID string,
	live []githubRepoMeta,
) {
	// Non-nil even when empty: an installation that genuinely grants nothing should
	// revoke everything, and a nil slice would encode as SQL NULL and silently
	// match no rows.
	liveIDs := make([]int64, 0, len(live))
	for _, repo := range live {
		liveIDs = append(liveIDs, repo.ID)
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE repository_connections
		SET status = 'revoked', revoked_at = NOW(), updated_at = NOW()
		WHERE workspace_id = $1
			AND github_installation_id = $2
			AND status <> 'revoked'
			AND NOT (repository_id = ANY($3::bigint[]))
	`, workspaceID, installationRowID, liveIDs)
	if err != nil {
		// Reconciliation is a side effect of a read. Never fail the read for it.
		s.logger.Error("repository connection reconciliation failed", "error", err, "workspace_id", workspaceID)
		return
	}
	if tag.RowsAffected() > 0 {
		s.logger.Info("revoked repository connections no longer granted by installation",
			"workspace_id", workspaceID, "count", tag.RowsAffected())
	}
}

// storedRepositories returns the repositories this workspace already has connected
// through an installation. It backs the fail-open path: when GitHub is unreachable
// the user keeps seeing what they connected instead of an empty list.
func (s *IntegrationServer) storedRepositories(ctx context.Context, workspaceID, installationRowID string) ([]*integrationv1.GitHubRepository, error) {
	rows, err := s.db.Query(ctx, `
		SELECT repository_id, owner, name, full_name, COALESCE(html_url, ''),
			COALESCE(clone_url, ''), default_branch, COALESCE(is_private, false)
		FROM repository_connections
		WHERE workspace_id = $1 AND github_installation_id = $2 AND status <> 'revoked'
		ORDER BY full_name
	`, workspaceID, installationRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repositories []*integrationv1.GitHubRepository
	for rows.Next() {
		var repo integrationv1.GitHubRepository
		if err := rows.Scan(&repo.RepositoryId, &repo.Owner, &repo.Name, &repo.FullName,
			&repo.HtmlUrl, &repo.CloneUrl, &repo.DefaultBranch, &repo.Private); err != nil {
			return nil, err
		}
		repositories = append(repositories, &repo)
	}
	return repositories, rows.Err()
}

func (s *IntegrationServer) GetGitHubConfig(ctx context.Context, req *integrationv1.GetGitHubConfigRequest) (*integrationv1.GetGitHubConfigResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	return &integrationv1.GetGitHubConfigResponse{
		InstallUrl:  s.cfg.GitHubAppInstallURL,
		CallbackUrl: s.cfg.GitHubAppCallbackURL,
		WebhookUrl:  s.cfg.GitHubWebhookURL,
		Configured:  s.cfg.GitHubConfigured(),
	}, nil
}

func (s *IntegrationServer) HandleGitHubCallback(ctx context.Context, req *integrationv1.HandleGitHubCallbackRequest) (*integrationv1.HandleGitHubCallbackResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" || req.InstallationId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id, workspace_id, and installation_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin"); err != nil {
		return nil, err
	}

	s.logger.Info("fetching installation metadata from github", "installation_id", req.InstallationId)
	meta, err := s.fetchInstallationMetadata(req.InstallationId)
	if err != nil {
		s.logger.Error("failed to fetch installation metadata", "error", err, "installation_id", req.InstallationId)
		return nil, status.Error(codes.Internal, "failed to fetch github installation metadata")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start database transaction")
	}
	defer tx.Rollback(ctx)

	installation, err := s.persistGitHubInstallation(ctx, tx, req.WorkspaceId, req.UserId, req.InstallationId, meta)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit transaction")
	}

	return &integrationv1.HandleGitHubCallbackResponse{Installation: installation}, nil
}

func (s *IntegrationServer) persistGitHubInstallation(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	userID string,
	installationID int64,
	meta *githubInstallationMeta,
) (*integrationv1.GitHubInstallation, error) {
	var integrationID string
	err := tx.QueryRow(ctx, `
		INSERT INTO integrations (workspace_id, provider, auth_type, status, external_account_id, external_account_name, created_by)
		VALUES ($1, 'github', 'github_app', 'active', $2, $3, $4)
		ON CONFLICT (workspace_id, provider, external_account_id) DO UPDATE
			SET status = 'active', external_account_name = EXCLUDED.external_account_name, updated_at = NOW()
		RETURNING id
	`, workspaceID, fmt.Sprintf("%d", meta.Account.ID), meta.Account.Login, userID).Scan(&integrationID)
	if err != nil {
		s.logger.Error("upsert integration failed", "error", err)
		return nil, status.Error(codes.Internal, "database error updating integrations")
	}

	permissions, err := json.Marshal(meta.Permissions)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to encode GitHub permissions")
	}
	// Bind the row to the App that GitHub says owns this installation. Stored as
	// NULL rather than 0 when GitHub omits it, so "unknown" stays distinguishable
	// from a real app id and githubAppIDMismatch keeps failing open on it.
	var appID *int64
	if meta.AppID > 0 {
		appID = &meta.AppID
	}

	var (
		instID          string
		accountLogin    string
		accountType     string
		repoSelection   string
		statusStr       string
		installedAtTime *time.Time
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO github_installations (
			workspace_id, integration_id, installation_id, app_id, account_id, account_login,
			account_type, repository_selection, permissions, status, installed_by, installed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'active', $10, NOW())
		ON CONFLICT (workspace_id, installation_id) DO UPDATE
			SET status = 'active', integration_id = EXCLUDED.integration_id,
				account_id = EXCLUDED.account_id, account_login = EXCLUDED.account_login,
				account_type = EXCLUDED.account_type,
				repository_selection = EXCLUDED.repository_selection,
				permissions = EXCLUDED.permissions,
				-- Reconnecting under a new App must rebind the row, but a response
				-- that omitted app_id must not erase a binding we already observed.
				app_id = COALESCE(EXCLUDED.app_id, github_installations.app_id),
				updated_at = NOW()
		RETURNING id, account_login, COALESCE(account_type, ''),
			COALESCE(repository_selection, ''), status, installed_at
	`, workspaceID, integrationID, installationID, appID, meta.Account.ID, meta.Account.Login,
		meta.Account.Type, meta.RepositorySelection, permissions, userID).Scan(
		&instID, &accountLogin, &accountType, &repoSelection, &statusStr, &installedAtTime,
	)
	if err != nil {
		s.logger.Error("upsert github installation failed", "error", err)
		return nil, status.Error(codes.Internal, "database error updating github_installations")
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, 'github.installation.connected', 'github_installation', $3,
			jsonb_build_object('installation_id', $4::bigint))
	`, workspaceID, userID, instID, installationID); err != nil {
		s.logger.Error("audit github installation failed", "error", err, "installation_id", installationID)
		return nil, status.Error(codes.Internal, "failed to write installation audit record")
	}

	installedAt := ""
	if installedAtTime != nil {
		installedAt = installedAtTime.Format(time.RFC3339)
	}
	return &integrationv1.GitHubInstallation{
		Id:                  instID,
		InstallationId:      installationID,
		AccountLogin:        accountLogin,
		AccountType:         accountType,
		RepositorySelection: repoSelection,
		Status:              statusStr,
		InstalledAt:         installedAt,
	}, nil
}

func (s *IntegrationServer) ListInstallations(ctx context.Context, req *integrationv1.ListInstallationsRequest) (*integrationv1.ListInstallationsResponse, error) {
	if req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and user_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, installation_id, account_login, COALESCE(account_type, ''), COALESCE(repository_selection, ''), status, installed_at
		FROM github_installations
		WHERE workspace_id = $1 AND status = 'active'
		ORDER BY created_at DESC
	`, req.WorkspaceId)
	if err != nil {
		s.logger.Error("list installations failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to query installations")
	}
	defer rows.Close()

	var installations []*integrationv1.GitHubInstallation
	for rows.Next() {
		var inst integrationv1.GitHubInstallation
		var installedAtTime *time.Time
		err := rows.Scan(&inst.Id, &inst.InstallationId, &inst.AccountLogin, &inst.AccountType, &inst.RepositorySelection, &inst.Status, &installedAtTime)
		if err != nil {
			continue
		}
		if installedAtTime != nil {
			inst.InstalledAt = installedAtTime.Format(time.RFC3339)
		}
		installations = append(installations, &inst)
	}

	return &integrationv1.ListInstallationsResponse{Installations: installations}, nil
}

func (s *IntegrationServer) ListRepositories(ctx context.Context, req *integrationv1.ListRepositoriesRequest) (*integrationv1.ListRepositoriesResponse, error) {
	if req.WorkspaceId == "" || req.GithubInstallationId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id, github_installation_id, and user_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
		return nil, err
	}

	var installationID int64
	var storedAppID *int64
	err := s.db.QueryRow(ctx, `
		SELECT installation_id, app_id FROM github_installations
		WHERE id = $1 AND workspace_id = $2
	`, req.GithubInstallationId, req.WorkspaceId).Scan(&installationID, &storedAppID)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "github installation not found in workspace")
	}
	if err != nil {
		s.logger.Error("fetch installation_id failed", "error", err)
		return nil, status.Error(codes.Internal, "database error fetching installation")
	}

	if err := s.enforceInstallationAppBinding(ctx, req.WorkspaceId, req.GithubInstallationId, installationID, storedAppID); err != nil {
		return nil, err
	}

	token, err := s.getInstallationToken(installationID)
	if err != nil {
		if errors.Is(err, ErrInstallationNotFound) {
			s.revokeUnusableInstallation(ctx, req.WorkspaceId, req.GithubInstallationId, installationID, revokeReasonTokenNotFound)
			return nil, status.Error(codes.FailedPrecondition, errInstallationAppMismatch)
		}
		s.logger.Error("failed to generate installation token", "error", err, "installation_id", installationID)
		return nil, status.Error(codes.Internal, "failed to authorize with github")
	}

	listCtx, cancel := context.WithTimeout(ctx, githubReadTimeout)
	defer cancel()
	repos, err := s.listInstallationRepositories(listCtx, token)
	if err != nil {
		// Fail open. An outage is indistinguishable from "the user removed every
		// repository" if it is allowed to reach reconciliation, so we neither revoke
		// nor blank the list: serve what is already stored and let the next read
		// reconcile. Only a read that actually saw GitHub may revoke anything.
		s.logger.Warn("github repository listing failed; serving stored connections",
			"error", err, "installation_id", installationID)
		stored, storedErr := s.storedRepositories(ctx, req.WorkspaceId, req.GithubInstallationId)
		if storedErr != nil {
			s.logger.Error("failed to read stored repository connections", "error", storedErr)
			return nil, status.Error(codes.Internal, "failed to retrieve repositories from github")
		}
		return &integrationv1.ListRepositoriesResponse{Repositories: stored}, nil
	}

	// GitHub answered, so this list is authoritative: anything stored but absent
	// from it is no longer granted to the installation and must stop being shown.
	s.reconcileRepositoryConnections(ctx, req.WorkspaceId, req.GithubInstallationId, repos)

	var responseRepos []*integrationv1.GitHubRepository
	for _, r := range repos {
		responseRepos = append(responseRepos, &integrationv1.GitHubRepository{
			RepositoryId:  r.ID,
			Owner:         r.Owner.Login,
			Name:          r.Name,
			FullName:      r.FullName,
			HtmlUrl:       r.HTMLURL,
			CloneUrl:      r.CloneURL,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
		})
	}

	return &integrationv1.ListRepositoriesResponse{Repositories: responseRepos}, nil
}

// enforceInstallationAppBinding fails a request before it ever reaches GitHub when
// the stored installation is bound to a different App, and revokes the stale rows
// on the way out so the frontend stops offering a connection that cannot work.
func (s *IntegrationServer) enforceInstallationAppBinding(
	ctx context.Context,
	workspaceID string,
	installationRowID string,
	installationID int64,
	storedAppID *int64,
) error {
	if !githubAppIDMismatch(storedAppID, s.cfg.GitHubAppID) {
		return nil
	}
	s.logger.Error("installation is bound to a different github app",
		"installation_id", installationID, "stored_app_id", *storedAppID)
	s.revokeUnusableInstallation(ctx, workspaceID, installationRowID, installationID, revokeReasonAppMismatch)
	return status.Error(codes.FailedPrecondition, errInstallationAppMismatch)
}

// revokeUnusableInstallation records the revocation and never propagates its own
// failure: the caller is already returning an actionable error, and a database
// hiccup here must not downgrade that into an opaque Internal.
func (s *IntegrationServer) revokeUnusableInstallation(
	ctx context.Context,
	workspaceID string,
	installationRowID string,
	installationID int64,
	reason string,
) {
	if err := s.revokeInstallation(ctx, workspaceID, installationRowID, reason); err != nil {
		s.logger.Error("failed to revoke unusable github installation",
			"error", err, "installation_id", installationID, "reason", reason)
	}
}

func (s *IntegrationServer) AttachRepository(ctx context.Context, req *integrationv1.AttachRepositoryRequest) (*integrationv1.AttachRepositoryResponse, error) {
	if req.WorkspaceId == "" || req.ProjectId == "" || req.GithubInstallationId == "" || req.RepositoryId == 0 || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, workspace_id, project_id, github_installation_id, and repository_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin"); err != nil {
		return nil, err
	}

	// Verify project exists in workspace
	var existsID string
	err := s.db.QueryRow(ctx, `
		SELECT id FROM projects WHERE id = $1 AND workspace_id = $2
	`, req.ProjectId, req.WorkspaceId).Scan(&existsID)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "project not found in workspace")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to check project existence")
	}

	// Resolve the installation inside the same workspace, then use GitHub as the
	// authority for repository metadata. Browser-supplied owner/name/URLs are
	// deliberately ignored because they are untrusted input.
	var integrationID string
	var installationID int64
	var storedAppID *int64
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(integration_id::text, ''), installation_id, app_id
		FROM github_installations
		WHERE id = $1 AND workspace_id = $2 AND status = 'active'
	`, req.GithubInstallationId, req.WorkspaceId).Scan(&integrationID, &installationID, &storedAppID)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "active github installation not found in workspace")
	}
	if err != nil || integrationID == "" {
		return nil, status.Error(codes.Internal, "failed to resolve installation details")
	}

	if err := s.enforceInstallationAppBinding(ctx, req.WorkspaceId, req.GithubInstallationId, installationID, storedAppID); err != nil {
		return nil, err
	}

	token, err := s.getInstallationToken(installationID)
	if err != nil {
		if errors.Is(err, ErrInstallationNotFound) {
			s.revokeUnusableInstallation(ctx, req.WorkspaceId, req.GithubInstallationId, installationID, revokeReasonTokenNotFound)
			return nil, status.Error(codes.FailedPrecondition, errInstallationAppMismatch)
		}
		s.logger.Error("failed to authorize repository attachment", "error", err, "installation_id", installationID)
		return nil, status.Error(codes.Unavailable, "failed to authorize with github")
	}
	listCtx, cancelList := context.WithTimeout(ctx, githubReadTimeout)
	repositories, err := s.listInstallationRepositories(listCtx, token)
	cancelList()
	if err != nil {
		s.logger.Error("failed to validate repository attachment", "error", err)
		return nil, status.Error(codes.Unavailable, "failed to validate repository with github")
	}
	var repository *githubRepoMeta
	for i := range repositories {
		if repositories[i].ID == req.RepositoryId {
			repository = &repositories[i]
			break
		}
	}
	if repository == nil {
		return nil, status.Error(codes.PermissionDenied, "repository is not accessible to this github installation")
	}
	selectedBranch := strings.TrimSpace(req.SelectedBranch)
	if selectedBranch == "" {
		selectedBranch = repository.DefaultBranch
	}
	if !validGitBranch(selectedBranch) {
		return nil, status.Error(codes.InvalidArgument, "selected_branch is not a valid git branch name")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start database transaction")
	}
	defer tx.Rollback(ctx)

	var userUUID *string
	if req.UserId != "" {
		userUUID = &req.UserId
	}

	// 1. Upsert repository_connections
	var (
		connID             string
		connFullName       string
		connHTMLURL        string
		connSelectedBranch string
		connStatus         string
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO repository_connections (workspace_id, project_id, integration_id, github_installation_id, provider, repository_id, owner, name, full_name, html_url, clone_url, default_branch, selected_branch, is_private, status, connected_by)
		VALUES ($1, $2, $3, $4, 'github', $5, $6, $7, $8, $9, $10, $11, $12, $13, 'connected', $14)
		ON CONFLICT (project_id) DO UPDATE
			SET github_installation_id = EXCLUDED.github_installation_id,
				integration_id = EXCLUDED.integration_id,
				repository_id = EXCLUDED.repository_id,
				owner = EXCLUDED.owner,
				name = EXCLUDED.name,
				full_name = EXCLUDED.full_name,
				html_url = EXCLUDED.html_url,
				clone_url = EXCLUDED.clone_url,
				default_branch = EXCLUDED.default_branch,
				selected_branch = EXCLUDED.selected_branch,
				is_private = EXCLUDED.is_private,
				status = 'connected',
				-- Reattaching clears a previous revocation outright, so a reconnect
				-- restores the row rather than leaving a stale revoked_at behind.
				revoked_at = NULL,
				updated_at = NOW()
		RETURNING id, full_name, COALESCE(html_url, ''), selected_branch, status
	`, req.WorkspaceId, req.ProjectId, integrationID, req.GithubInstallationId, repository.ID, repository.Owner.Login, repository.Name, repository.FullName, repository.HTMLURL, repository.CloneURL, repository.DefaultBranch, selectedBranch, repository.Private, userUUID).Scan(
		&connID, &connFullName, &connHTMLURL, &connSelectedBranch, &connStatus,
	)
	if err != nil {
		s.logger.Error("upsert repository connection failed", "error", err)
		return nil, status.Error(codes.Internal, "database error saving repository connection")
	}

	// 2. Update projects metadata
	_, err = tx.Exec(ctx, `
		UPDATE projects
		SET repo_url = $1, repo_provider = 'github', default_branch = $2, indexing_status = 'queued', updated_at = NOW()
		WHERE id = $3
	`, repository.HTMLURL, repository.DefaultBranch, req.ProjectId)
	if err != nil {
		s.logger.Error("update project metadata failed", "error", err)
		return nil, status.Error(codes.Internal, "database error updating project metadata")
	}

	// 3. Queue ingestion job
	var (
		jobID        string
		jobStatus    string
		jobSrcBranch string
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO ingestion_jobs (workspace_id, project_id, repository_connection_id, job_type, status, source_branch, attempt_count, max_attempts, created_by)
		VALUES ($1, $2, $3, 'repo_index', 'queued', $4, 0, 3, $5)
		RETURNING id, status, COALESCE(source_branch, '')
	`, req.WorkspaceId, req.ProjectId, connID, selectedBranch, userUUID).Scan(
		&jobID, &jobStatus, &jobSrcBranch,
	)
	if err != nil {
		s.logger.Error("queue ingestion job failed", "error", err)
		return nil, status.Error(codes.Internal, "database error creating ingestion job")
	}

	// 4. Audit log entry
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'project.repository.connected', 'repository_connection', $3)
	`, req.WorkspaceId, userUUID, connID); err != nil {
		s.logger.Error("audit repository connection failed", "error", err, "project_id", req.ProjectId)
		return nil, status.Error(codes.Internal, "failed to audit repository connection")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit transaction")
	}

	if s.publisher != nil {
		payload, marshalErr := json.Marshal(map[string]string{
			"job_id": jobID, "workspace_id": req.WorkspaceId, "project_id": req.ProjectId,
		})
		if marshalErr == nil {
			publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if publishErr := s.publisher.Publish(publishCtx, "repo.ingestion.requested", payload); publishErr != nil {
				s.logger.Warn("ingestion event publish failed; durable database recovery will retry", "error", publishErr, "job_id", jobID)
			}
		}
	}

	return &integrationv1.AttachRepositoryResponse{
		RepositoryConnection: &integrationv1.RepositoryConnection{
			Id:             connID,
			WorkspaceId:    req.WorkspaceId,
			ProjectId:      req.ProjectId,
			FullName:       connFullName,
			HtmlUrl:        connHTMLURL,
			SelectedBranch: connSelectedBranch,
			Status:         connStatus,
		},
		IngestionJob: &integrationv1.IngestionJob{
			Id:           jobID,
			WorkspaceId:  req.WorkspaceId,
			ProjectId:    req.ProjectId,
			Status:       jobStatus,
			SourceBranch: jobSrcBranch,
		},
	}, nil
}

func (s *IntegrationServer) GetRepositoryConnection(ctx context.Context, req *integrationv1.GetRepositoryConnectionRequest) (*integrationv1.GetRepositoryConnectionResponse, error) {
	if req.WorkspaceId == "" || req.ProjectId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, workspace_id and project_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
		return nil, err
	}

	var conn integrationv1.RepositoryConnection
	var lastIndexedSha string
	var lastIndexedTime *time.Time

	err := s.db.QueryRow(ctx, `
		SELECT id, workspace_id, project_id, full_name, COALESCE(html_url, ''), selected_branch, status, COALESCE(last_indexed_commit_sha, ''), last_indexed_at
		FROM repository_connections
		WHERE project_id = $1 AND workspace_id = $2
	`, req.ProjectId, req.WorkspaceId).Scan(
		&conn.Id, &conn.WorkspaceId, &conn.ProjectId, &conn.FullName, &conn.HtmlUrl, &conn.SelectedBranch, &conn.Status, &lastIndexedSha, &lastIndexedTime,
	)

	if err == pgx.ErrNoRows {
		// No connection connected yet
		return &integrationv1.GetRepositoryConnectionResponse{}, nil
	}
	if err != nil {
		s.logger.Error("get repository connection failed", "error", err)
		return nil, status.Error(codes.Internal, "database query error")
	}

	conn.LastIndexedCommitSha = lastIndexedSha
	if lastIndexedTime != nil {
		conn.LastIndexedAt = lastIndexedTime.Format(time.RFC3339)
	}

	var job integrationv1.IngestionJob
	var jobSrcSha string
	var jobStarted, jobFinished *time.Time

	err = s.db.QueryRow(ctx, `
		SELECT id, workspace_id, project_id, status, COALESCE(source_branch, ''), COALESCE(source_commit_sha, ''), started_at, finished_at
		FROM ingestion_jobs
		WHERE project_id = $1 AND workspace_id = $2
		ORDER BY queued_at DESC LIMIT 1
	`, req.ProjectId, req.WorkspaceId).Scan(
		&job.Id, &job.WorkspaceId, &job.ProjectId, &job.Status, &job.SourceBranch, &jobSrcSha, &jobStarted, &jobFinished,
	)

	if err != nil && err != pgx.ErrNoRows {
		s.logger.Error("get latest ingestion job failed", "error", err)
	} else if err == nil {
		job.SourceCommitSha = jobSrcSha
		if jobStarted != nil {
			job.StartedAt = jobStarted.Format(time.RFC3339)
		}
		if jobFinished != nil {
			job.FinishedAt = jobFinished.Format(time.RFC3339)
		}
	}

	res := &integrationv1.GetRepositoryConnectionResponse{
		RepositoryConnection: &conn,
	}
	if job.Id != "" {
		res.LatestIngestionJob = &job
	}

	return res, nil
}

// ListRepositoryConnections returns every connected repository the caller can
// see. With an empty workspace_id it spans all of the caller's workspaces,
// which is what the top-level dashboard renders.
func (s *IntegrationServer) ListRepositoryConnections(ctx context.Context, req *integrationv1.ListRepositoryConnectionsRequest) (*integrationv1.ListRepositoryConnectionsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	// Scoped mode gets the explicit role guard. Unscoped mode is bounded by the
	// workspace_members join below — keep that predicate in the JOIN condition,
	// because moving it into WHERE alongside the OR would leak every repository
	// in the database.
	if req.WorkspaceId != "" {
		if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
			return nil, err
		}
	}

	rows, err := s.db.Query(ctx, `
		SELECT rc.id, rc.workspace_id, w.name, rc.project_id, p.name,
		       rc.full_name, COALESCE(rc.html_url, ''), rc.selected_branch, rc.status,
		       COALESCE(rc.last_indexed_commit_sha, ''), rc.last_indexed_at,
		       COALESCE(j.id::text, ''), COALESCE(j.status, ''),
		       COALESCE(j.source_branch, ''), COALESCE(j.source_commit_sha, ''),
		       j.started_at, j.finished_at
		FROM repository_connections rc
		JOIN workspace_members wm ON wm.workspace_id = rc.workspace_id AND wm.user_id = $1::uuid
		JOIN workspaces w ON w.id = rc.workspace_id
		JOIN projects   p ON p.id = rc.project_id
		LEFT JOIN LATERAL (
			SELECT ij.id, ij.status, ij.source_branch, ij.source_commit_sha, ij.started_at, ij.finished_at
			FROM ingestion_jobs ij
			WHERE ij.workspace_id = rc.workspace_id AND ij.project_id = rc.project_id
			ORDER BY ij.queued_at DESC
			LIMIT 1
		) j ON TRUE
		-- NULLIF keeps the empty (unscoped) case from ever reaching a uuid cast,
		-- so this does not depend on OR short-circuit evaluation order.
		WHERE (NULLIF($2, '')::uuid IS NULL OR rc.workspace_id = NULLIF($2, '')::uuid)
			-- A revoked connection is one the installation no longer grants. It stays
			-- in the table so a reconnect can restore it without re-indexing, but it
			-- must not be listed: this is what makes a repository removed from the
			-- GitHub App disappear from the dashboard.
			AND rc.status <> 'revoked'
		ORDER BY w.created_at DESC, p.name ASC
	`, req.UserId, req.WorkspaceId)
	if err != nil {
		s.logger.Error("list repository connections failed", "error", err)
		return nil, status.Error(codes.Internal, "database query error")
	}
	defer rows.Close()

	summaries := make([]*integrationv1.RepositoryConnectionSummary, 0)
	for rows.Next() {
		conn := &integrationv1.RepositoryConnection{}
		job := &integrationv1.IngestionJob{}
		var workspaceName, projectName string
		var lastIndexedAt, jobStarted, jobFinished *time.Time

		if err := rows.Scan(
			&conn.Id, &conn.WorkspaceId, &workspaceName, &conn.ProjectId, &projectName,
			&conn.FullName, &conn.HtmlUrl, &conn.SelectedBranch, &conn.Status,
			&conn.LastIndexedCommitSha, &lastIndexedAt,
			&job.Id, &job.Status, &job.SourceBranch, &job.SourceCommitSha,
			&jobStarted, &jobFinished,
		); err != nil {
			s.logger.Error("scan repository connection failed", "error", err)
			return nil, status.Error(codes.Internal, "failed to read repository connection")
		}

		if lastIndexedAt != nil {
			conn.LastIndexedAt = lastIndexedAt.Format(time.RFC3339)
		}

		summary := &integrationv1.RepositoryConnectionSummary{
			RepositoryConnection: conn,
			ProjectName:          projectName,
			WorkspaceName:        workspaceName,
		}
		if job.Id != "" {
			job.WorkspaceId = conn.WorkspaceId
			job.ProjectId = conn.ProjectId
			if jobStarted != nil {
				job.StartedAt = jobStarted.Format(time.RFC3339)
			}
			if jobFinished != nil {
				job.FinishedAt = jobFinished.Format(time.RFC3339)
			}
			summary.LatestIngestionJob = job
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("iterate repository connections failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to read repository connections")
	}

	return &integrationv1.ListRepositoryConnectionsResponse{Connections: summaries}, nil
}

func (s *IntegrationServer) HandleGitHubWebhook(ctx context.Context, req *integrationv1.HandleGitHubWebhookRequest) (*integrationv1.HandleGitHubWebhookResponse, error) {
	s.logger.Info("github webhook received", "event_type", req.EventType)

	verified := verifySignature(req.Payload, req.Signature, s.cfg.GitHubAppWebhookSecret)
	if !verified {
		s.logger.Warn("invalid webhook signature", "event_type", req.EventType)
		return nil, status.Error(codes.Unauthenticated, "invalid webhook signature")
	}

	s.logger.Info("webhook verified successfully, processing event", "event_type", req.EventType)
	// A verified signature proves GitHub sent the delivery. It says nothing about
	// the body, which stays untrusted: see applyGitHubWebhookRevocations for the
	// limits placed on what the payload is allowed to influence.

	// Create audit log event
	metadata, _ := json.Marshal(map[string]string{"event_type": req.EventType})
	if _, err := s.db.Exec(ctx, `
		INSERT INTO audit_logs (action, resource_type, metadata)
		VALUES ('github.webhook.received', 'webhook', $1::jsonb)
	`, string(metadata)); err != nil {
		s.logger.Error("audit github webhook failed", "error", err, "event_type", req.EventType)
		return nil, status.Error(codes.Internal, "failed to audit webhook")
	}

	if err := s.applyGitHubWebhookRevocations(ctx, req.EventType, req.Payload); err != nil {
		// Returning an error lets GitHub redeliver. Revocation is idempotent, so a
		// redelivery is always safe.
		s.logger.Error("github webhook revocation failed", "error", err, "event_type", req.EventType)
		return nil, status.Error(codes.Internal, "failed to apply webhook revocation")
	}

	return &integrationv1.HandleGitHubWebhookResponse{Success: true}, nil
}

// githubWebhookEnvelope is the deliberately minimal view this service takes of a
// webhook body: an installation id, an action, and the ids of removed
// repositories. Nothing else is decoded, so no other field can influence anything.
type githubWebhookEnvelope struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	RepositoriesRemoved []struct {
		ID int64 `json:"id"`
	} `json:"repositories_removed"`
}

// applyGitHubWebhookRevocations narrows a verified delivery down to the only two
// events that may change local state, and only ever in the revoking direction.
//
// The payload is untrusted data, not instructions. It is used solely to select
// rows that already exist, by numeric id. It cannot create a row, grant access,
// widen a scope, move a resource between workspaces, or select a code path beyond
// the fixed cases below; every workspace scope is read from the stored
// installation, never from the body. Names, prose, and every other field are
// ignored outright.
func (s *IntegrationServer) applyGitHubWebhookRevocations(ctx context.Context, eventType string, payload []byte) error {
	if eventType != "installation" && eventType != "installation_repositories" {
		return nil
	}

	var envelope githubWebhookEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		// An undecodable body is not a server fault; there is simply nothing to act
		// on. The delivery has already been audited.
		s.logger.Warn("github webhook payload could not be decoded", "event_type", eventType)
		return nil
	}
	if envelope.Installation.ID <= 0 {
		return nil
	}

	switch eventType {
	case "installation":
		// "deleted" and "suspend" both mean the installation grants nothing right
		// now. Any other action ("created", "unsuspend", "new_permissions_accepted")
		// deliberately falls through: no webhook may restore access, only a
		// reconnect through the authenticated install flow can.
		var reason string
		switch envelope.Action {
		case "deleted":
			reason = revokeReasonWebhookDeleted
		case "suspend":
			reason = revokeReasonWebhookSuspended
		default:
			return nil
		}
		s.logger.Info("revoking installation from webhook",
			"installation_id", envelope.Installation.ID, "reason", reason)
		return s.revokeInstallationByGitHubID(ctx, envelope.Installation.ID, reason)

	case "installation_repositories":
		if envelope.Action != "removed" {
			return nil
		}
		repositoryIDs := make([]int64, 0, len(envelope.RepositoriesRemoved))
		for _, repo := range envelope.RepositoriesRemoved {
			if repo.ID > 0 {
				repositoryIDs = append(repositoryIDs, repo.ID)
			}
		}
		if len(repositoryIDs) == 0 {
			return nil
		}
		s.logger.Info("revoking repository connections from webhook",
			"installation_id", envelope.Installation.ID, "count", len(repositoryIDs))
		return s.revokeConnectionsByRepositoryIDs(ctx, envelope.Installation.ID, repositoryIDs)
	}

	return nil
}

func verifySignature(payload []byte, signatureHeader string, secret string) bool {
	if secret == "" {
		return false
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	hexSignature := signatureHeader[7:]
	expectedSignature, err := hex.DecodeString(hexSignature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualSignature := mac.Sum(nil)

	return hmac.Equal(expectedSignature, actualSignature)
}

func validGitBranch(branch string) bool {
	if branch == "" || len(branch) > 255 || branch == "@" || strings.HasPrefix(branch, ".") ||
		strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, "/") ||
		strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.Contains(branch, "//") ||
		strings.ContainsAny(branch, " ~^:?*[\\") {
		return false
	}
	for _, char := range branch {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func (s *IntegrationServer) requireWorkspaceRole(
	ctx context.Context,
	workspaceID string,
	userID string,
	allowedRoles ...string,
) error {
	var role string
	err := s.db.QueryRow(ctx, `
		SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&role)
	if err == pgx.ErrNoRows {
		return status.Error(codes.PermissionDenied, "workspace access denied")
	}
	if err != nil {
		s.logger.Error("workspace membership check failed", "error", err, "workspace_id", workspaceID)
		return status.Error(codes.Internal, "workspace membership check failed")
	}
	for _, allowedRole := range allowedRoles {
		if role == allowedRole {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "workspace role does not permit this action")
}
