package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"time"

	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const githubInstallSessionTTL = 10 * time.Minute

func githubInstallStateHash(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}

func (s *IntegrationServer) CreateGitHubInstallSession(ctx context.Context, req *integrationv1.CreateGitHubInstallSessionRequest) (*integrationv1.CreateGitHubInstallSessionResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and workspace_id are required")
	}
	if !s.cfg.GitHubConfigured() {
		return nil, status.Error(codes.FailedPrecondition, "GitHub App is not configured")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin"); err != nil {
		return nil, err
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, status.Error(codes.Internal, "failed to create installation state")
	}
	state := base64.RawURLEncoding.EncodeToString(random)
	expiresAt := time.Now().UTC().Add(githubInstallSessionTTL)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO github_install_sessions (token_hash, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4)
	`, githubInstallStateHash(state), req.WorkspaceId, req.UserId, expiresAt); err != nil {
		s.logger.Error("create GitHub install session failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to create GitHub installation session")
	}

	installURL, err := url.Parse(s.cfg.GitHubAppInstallURL)
	if err != nil || installURL.Scheme != "https" || installURL.Host != "github.com" {
		return nil, status.Error(codes.FailedPrecondition, "GitHub install URL is invalid")
	}
	query := installURL.Query()
	query.Set("state", state)
	installURL.RawQuery = query.Encode()

	return &integrationv1.CreateGitHubInstallSessionResponse{
		InstallUrl: installURL.String(),
		ExpiresAt:  expiresAt.Format(time.RFC3339),
	}, nil
}

func (s *IntegrationServer) CompleteGitHubInstallSession(ctx context.Context, req *integrationv1.CompleteGitHubInstallSessionRequest) (*integrationv1.CompleteGitHubInstallSessionResponse, error) {
	if len(req.State) < 40 || len(req.State) > 128 || req.InstallationId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "valid state and installation_id are required")
	}
	stateHash := githubInstallStateHash(req.State)

	// Resolve an unexpired session before the external read. The same predicate
	// is checked again under a row lock before any local state is changed.
	var workspaceID, userID string
	if err := s.db.QueryRow(ctx, `
		SELECT workspace_id, user_id
		FROM github_install_sessions
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > NOW()
	`, stateHash).Scan(&workspaceID, &userID); err != nil {
		return nil, status.Error(codes.PermissionDenied, "installation state is invalid, expired, or already used")
	}

	meta, err := s.fetchInstallationMetadata(req.InstallationId)
	if err != nil {
		s.logger.Error("failed to fetch installation metadata", "error", err, "installation_id", req.InstallationId)
		return nil, status.Error(codes.Unavailable, "failed to verify GitHub installation")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start database transaction")
	}
	defer tx.Rollback(ctx)

	var lockedWorkspaceID, lockedUserID string
	if err := tx.QueryRow(ctx, `
		SELECT workspace_id, user_id
		FROM github_install_sessions
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > NOW()
		FOR UPDATE
	`, stateHash).Scan(&lockedWorkspaceID, &lockedUserID); err != nil {
		return nil, status.Error(codes.PermissionDenied, "installation state is invalid, expired, or already used")
	}
	if lockedWorkspaceID != workspaceID || lockedUserID != userID {
		return nil, status.Error(codes.PermissionDenied, "installation state does not match")
	}

	installation, err := s.persistGitHubInstallation(ctx, tx, workspaceID, userID, req.InstallationId, meta)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE github_install_sessions
		SET consumed_at = NOW(), installation_id = $2
		WHERE token_hash = $1 AND consumed_at IS NULL
	`, stateHash, req.InstallationId)
	if err != nil || result.RowsAffected() != 1 {
		return nil, status.Error(codes.Aborted, "installation session was already consumed")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit GitHub installation")
	}

	return &integrationv1.CompleteGitHubInstallSessionResponse{
		Installation: installation,
		WorkspaceId:  workspaceID,
	}, nil
}
