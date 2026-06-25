package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"github.com/opspilot/integration-service/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IntegrationServer struct {
	integrationv1.UnimplementedIntegrationServiceServer
	db     *pgxpool.Pool
	cfg    *config.Config
	logger *slog.Logger
}

func New(db *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *IntegrationServer {
	return &IntegrationServer{db: db, cfg: cfg, logger: logger}
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

	var userUUID *string
	if req.UserId != "" {
		userUUID = &req.UserId
	}

	// 1. Upsert integrations
	var integrationID string
	err = tx.QueryRow(ctx, `
		INSERT INTO integrations (workspace_id, provider, auth_type, status, external_account_id, external_account_name, created_by)
		VALUES ($1, 'github', 'github_app', 'active', $2, $3, $4)
		ON CONFLICT (workspace_id, provider, external_account_id) DO UPDATE
			SET status = 'active', external_account_name = EXCLUDED.external_account_name, updated_at = NOW()
		RETURNING id
	`, req.WorkspaceId, fmt.Sprintf("%d", meta.Account.ID), meta.Account.Login, userUUID).Scan(&integrationID)
	if err != nil {
		s.logger.Error("upsert integration failed", "error", err)
		return nil, status.Error(codes.Internal, "database error updating integrations")
	}

	// 2. Upsert github_installations
	var (
		instID          string
		accountLogin    string
		accountType     string
		repoSelection   string
		statusStr       string
		installedAtTime *time.Time
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO github_installations (workspace_id, integration_id, installation_id, account_id, account_login, account_type, repository_selection, status, installed_by, installed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, NOW())
		ON CONFLICT (installation_id) DO UPDATE
			SET status = 'active', repository_selection = EXCLUDED.repository_selection, updated_at = NOW()
		RETURNING id, account_login, COALESCE(account_type, ''), COALESCE(repository_selection, ''), status, installed_at
	`, req.WorkspaceId, integrationID, req.InstallationId, meta.Account.ID, meta.Account.Login, meta.Account.Type, meta.RepositorySelection, userUUID).Scan(
		&instID, &accountLogin, &accountType, &repoSelection, &statusStr, &installedAtTime,
	)
	if err != nil {
		s.logger.Error("upsert github installation failed", "error", err)
		return nil, status.Error(codes.Internal, "database error updating github_installations")
	}

	// 3. Write to audit logs
	_, _ = tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'github.installation.connected', 'github_installation', $3)
	`, req.WorkspaceId, userUUID, instID)

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit transaction")
	}

	installedAtStr := ""
	if installedAtTime != nil {
		installedAtStr = installedAtTime.Format(time.RFC3339)
	}

	return &integrationv1.HandleGitHubCallbackResponse{
		Installation: &integrationv1.GitHubInstallation{
			Id:                  instID,
			InstallationId:      req.InstallationId,
			AccountLogin:        accountLogin,
			AccountType:         accountType,
			RepositorySelection: repoSelection,
			Status:              statusStr,
			InstalledAt:         installedAtStr,
		},
	}, nil
}

func (s *IntegrationServer) ListInstallations(ctx context.Context, req *integrationv1.ListInstallationsRequest) (*integrationv1.ListInstallationsResponse, error) {
	if req.WorkspaceId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id is required")
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
	if req.WorkspaceId == "" || req.GithubInstallationId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and github_installation_id are required")
	}

	var installationID int64
	err := s.db.QueryRow(ctx, `
		SELECT installation_id FROM github_installations
		WHERE id = $1 AND workspace_id = $2
	`, req.GithubInstallationId, req.WorkspaceId).Scan(&installationID)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "github installation not found in workspace")
	}
	if err != nil {
		s.logger.Error("fetch installation_id failed", "error", err)
		return nil, status.Error(codes.Internal, "database error fetching installation")
	}

	token, err := s.getInstallationToken(installationID)
	if err != nil {
		s.logger.Error("failed to generate installation token", "error", err, "installation_id", installationID)
		return nil, status.Error(codes.Internal, "failed to authorize with github")
	}

	repos, err := s.fetchInstallationRepositories(token)
	if err != nil {
		s.logger.Error("failed to fetch installation repositories", "error", err)
		return nil, status.Error(codes.Internal, "failed to retrieve repositories from github")
	}

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

func (s *IntegrationServer) AttachRepository(ctx context.Context, req *integrationv1.AttachRepositoryRequest) (*integrationv1.AttachRepositoryResponse, error) {
	if req.WorkspaceId == "" || req.ProjectId == "" || req.GithubInstallationId == "" || req.RepositoryId == 0 {
		return nil, status.Error(codes.InvalidArgument, "workspace_id, project_id, github_installation_id, and repository_id are required")
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start database transaction")
	}
	defer tx.Rollback(ctx)

	var userUUID *string
	if req.UserId != "" {
		userUUID = &req.UserId
	}

	// Fetch integration_id from installation
	var integrationID string
	err = tx.QueryRow(ctx, `
		SELECT integration_id FROM github_installations WHERE id = $1
	`, req.GithubInstallationId).Scan(&integrationID)
	if err != nil {
		s.logger.Error("fetch integration_id failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to resolve installation details")
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
				updated_at = NOW()
		RETURNING id, full_name, COALESCE(html_url, ''), selected_branch, status
	`, req.WorkspaceId, req.ProjectId, integrationID, req.GithubInstallationId, req.RepositoryId, req.Owner, req.Name, req.FullName, req.HtmlUrl, req.CloneUrl, req.DefaultBranch, req.SelectedBranch, req.Private, userUUID).Scan(
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
	`, req.HtmlUrl, req.DefaultBranch, req.ProjectId)
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
	`, req.WorkspaceId, req.ProjectId, connID, req.SelectedBranch, userUUID).Scan(
		&jobID, &jobStatus, &jobSrcBranch,
	)
	if err != nil {
		s.logger.Error("queue ingestion job failed", "error", err)
		return nil, status.Error(codes.Internal, "database error creating ingestion job")
	}

	// 4. Audit log entry
	_, _ = tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'project.repository.connected', 'repository_connection', $3)
	`, req.WorkspaceId, userUUID, connID)

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit transaction")
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
	if req.WorkspaceId == "" || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and project_id are required")
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

func (s *IntegrationServer) HandleGitHubWebhook(ctx context.Context, req *integrationv1.HandleGitHubWebhookRequest) (*integrationv1.HandleGitHubWebhookResponse, error) {
	s.logger.Info("github webhook received", "event_type", req.EventType, "signature", req.Signature)

	verified := verifySignature(req.Payload, req.Signature, s.cfg.GitHubAppWebhookSecret)
	if !verified {
		s.logger.Warn("invalid webhook signature", "event_type", req.EventType)
		return nil, status.Error(codes.Unauthenticated, "invalid webhook signature")
	}

	s.logger.Info("webhook verified successfully, processing event", "event_type", req.EventType)
	// Webhook signature verified. Under MVP rules:
	// - Treat webhook data as untrusted input.
	// - Do not trigger risky actions from webhooks.
	// - Log successfully.
	
	// Create audit log event
	_, _ = s.db.Exec(ctx, `
		INSERT INTO audit_logs (action, resource_type, metadata)
		VALUES ('github.webhook.received', 'webhook', $1)
	`, []byte(fmt.Sprintf(`{"event_type": %q}`, req.EventType)))

	return &integrationv1.HandleGitHubWebhookResponse{Success: true}, nil
}

func verifySignature(payload []byte, signatureHeader string, secret string) bool {
	if secret == "" {
		return true // skip verification if no secret is configured
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

