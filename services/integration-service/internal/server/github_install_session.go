package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

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

	// Sweep abandoned sessions opportunistically. Most installs are never
	// completed, so unconsumed rows would otherwise accumulate forever behind the
	// partial index from migration 005. Doing it here keeps the table bounded by
	// install attempts without a background goroutine, and a failed sweep is only
	// housekeeping: it must never block the user from starting an install.
	// Consumed rows are left alone; they are the record of a real installation.
	if _, err := s.db.Exec(ctx, `
		DELETE FROM github_install_sessions
		WHERE consumed_at IS NULL AND expires_at < NOW()
	`); err != nil {
		s.logger.Warn("expired GitHub install session sweep failed", "error", err)
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

	// Claim the state in one atomic statement BEFORE the external read.
	//
	// This endpoint is necessarily unauthenticated -- GitHub redirects here with
	// no session -- and installation_id is caller-supplied. Previously the state
	// was only consumed at the very end, so a failed metadata lookup left it
	// reusable and a caller could sweep installation_ids with a single valid
	// state until one resolved. Burning it up front makes that impossible: one
	// state, one attempt, whatever the outcome.
	var workspaceID, userID string
	if err := s.db.QueryRow(ctx, `
		UPDATE github_install_sessions
		SET consumed_at = NOW(), installation_id = $2
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > NOW()
		RETURNING workspace_id, user_id
	`, stateHash, req.InstallationId).Scan(&workspaceID, &userID); err != nil {
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

	// Refuse to bind an installation that another person already connected.
	//
	// Migration 004 scoped uniqueness to (workspace_id, installation_id) so one
	// GitHub account can serve several of its owner's workspaces. That is the
	// intended behaviour, but it also removed the global constraint that used to
	// stop a caller inserting somebody else's installation into a workspace they
	// control. Keying on installed_by preserves the multi-workspace case while
	// closing the cross-tenant one.
	var existingInstaller *string
	err = tx.QueryRow(ctx, `
		SELECT installed_by::text
		FROM github_installations
		WHERE installation_id = $1 AND workspace_id <> $2
		LIMIT 1
	`, req.InstallationId, workspaceID).Scan(&existingInstaller)
	switch {
	case err == nil:
		if existingInstaller == nil || *existingInstaller != userID {
			s.logger.Warn("rejected cross-tenant github installation bind",
				"installation_id", req.InstallationId, "workspace_id", workspaceID, "user_id", userID)
			return nil, status.Error(codes.PermissionDenied,
				"this GitHub installation is already connected by another account")
		}
	case errors.Is(err, pgx.ErrNoRows):
		// Not bound anywhere else; nothing to check.
	default:
		s.logger.Error("failed to check existing installation binding", "error", err)
		return nil, status.Error(codes.Internal, "failed to verify installation ownership")
	}

	installation, err := s.persistGitHubInstallation(ctx, tx, workspaceID, userID, req.InstallationId, meta)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit GitHub installation")
	}

	return &integrationv1.CompleteGitHubInstallSessionResponse{
		Installation: installation,
		WorkspaceId:  workspaceID,
	}, nil
}
