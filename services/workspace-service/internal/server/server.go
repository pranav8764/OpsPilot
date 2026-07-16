package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/opspilot/gen/proto/workspace/v1"
)

type WorkspaceServer struct {
	workspacev1.UnimplementedWorkspaceServiceServer
	db     *pgxpool.Pool
	logger *slog.Logger
}

func New(db *pgxpool.Pool, logger *slog.Logger) *WorkspaceServer {
	return &WorkspaceServer{db: db, logger: logger}
}

func (s *WorkspaceServer) CreateWorkspace(ctx context.Context, req *workspacev1.CreateWorkspaceRequest) (*workspacev1.CreateWorkspaceResponse, error) {
	if req.Name == "" || req.OwnerId == "" {
		return nil, status.Error(codes.InvalidArgument, "name and owner_id are required")
	}

	slug := toSlug(req.Name)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}
	defer tx.Rollback(ctx)

	var ws workspacev1.Workspace
	var createdAt time.Time

	err = tx.QueryRow(ctx, `
		INSERT INTO workspaces (name, slug, owner_id, plan)
		VALUES ($1, $2, $3, 'free')
		RETURNING id, name, COALESCE(slug,''), owner_id, plan, log_retention_days, created_at
	`, req.Name, slug, req.OwnerId).Scan(
		&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.LogRetentionDays, &createdAt,
	)
	if err != nil {
		s.logger.Error("create workspace", "error", err)
		return nil, status.Error(codes.Internal, "failed to create workspace")
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, ws.Id, req.OwnerId)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to add owner as member")
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'workspace.created', 'workspace', $1)
	`, ws.Id, req.OwnerId); err != nil {
		return nil, status.Error(codes.Internal, "failed to audit workspace creation")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "commit failed")
	}

	ws.Role = "owner"
	ws.CreatedAt = createdAt.Format(time.RFC3339)
	s.logger.Info("workspace created", "id", ws.Id, "name", ws.Name)
	return &workspacev1.CreateWorkspaceResponse{Workspace: &ws}, nil
}

func (s *WorkspaceServer) GetWorkspaces(ctx context.Context, req *workspacev1.GetWorkspacesRequest) (*workspacev1.GetWorkspacesResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	rows, err := s.db.Query(ctx, `
		SELECT w.id, w.name, COALESCE(w.slug,''), w.owner_id, w.plan, wm.role, w.log_retention_days, w.created_at
		FROM workspaces w
		JOIN workspace_members wm ON w.id = wm.workspace_id
		WHERE wm.user_id = $1
		ORDER BY w.created_at DESC
	`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, "query failed")
	}
	defer rows.Close()

	var workspaces []*workspacev1.Workspace
	for rows.Next() {
		var ws workspacev1.Workspace
		var createdAt time.Time
		if err := rows.Scan(&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.Role, &ws.LogRetentionDays, &createdAt); err != nil {
			continue
		}
		ws.CreatedAt = createdAt.Format(time.RFC3339)
		workspaces = append(workspaces, &ws)
	}

	return &workspacev1.GetWorkspacesResponse{Workspaces: workspaces}, nil
}

func (s *WorkspaceServer) GetWorkspace(ctx context.Context, req *workspacev1.GetWorkspaceRequest) (*workspacev1.GetWorkspaceResponse, error) {
	if req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and user_id are required")
	}

	var ws workspacev1.Workspace
	var createdAt time.Time

	err := s.db.QueryRow(ctx, `
		SELECT w.id, w.name, COALESCE(w.slug,''), w.owner_id, w.plan, wm.role, w.log_retention_days, w.created_at
		FROM workspaces w
		JOIN workspace_members wm ON w.id = wm.workspace_id
		WHERE w.id = $1 AND wm.user_id = $2
	`, req.WorkspaceId, req.UserId).Scan(
		&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.Role, &ws.LogRetentionDays, &createdAt,
	)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("workspace %s not found", req.WorkspaceId))
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	ws.CreatedAt = createdAt.Format(time.RFC3339)
	return &workspacev1.GetWorkspaceResponse{Workspace: &ws}, nil
}

func (s *WorkspaceServer) DeleteWorkspace(ctx context.Context, req *workspacev1.DeleteWorkspaceRequest) (*workspacev1.DeleteWorkspaceResponse, error) {
	if req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and user_id are required")
	}

	result, err := s.db.Exec(ctx,
		`DELETE FROM workspaces WHERE id = $1 AND owner_id = $2`,
		req.WorkspaceId, req.UserId,
	)
	if err != nil || result.RowsAffected() == 0 {
		return nil, status.Error(codes.NotFound, "workspace not found or you are not the owner")
	}
	return &workspacev1.DeleteWorkspaceResponse{Success: true}, nil
}

func (s *WorkspaceServer) UpdateWorkspaceSettings(ctx context.Context, req *workspacev1.UpdateWorkspaceSettingsRequest) (*workspacev1.UpdateWorkspaceSettingsResponse, error) {
	if req.WorkspaceId == "" || req.UserId == "" || req.LogRetentionDays < 1 || req.LogRetentionDays > 365 {
		return nil, status.Error(codes.InvalidArgument, "workspace_id, user_id, and log_retention_days between 1 and 365 are required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}
	defer tx.Rollback(ctx)

	var ws workspacev1.Workspace
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE workspaces AS w
		SET log_retention_days = $3, updated_at = NOW()
		FROM workspace_members AS member
		WHERE w.id = $1 AND member.workspace_id = w.id AND member.user_id = $2
		  AND member.role IN ('owner', 'admin')
		RETURNING w.id, w.name, COALESCE(w.slug,''), w.owner_id, w.plan,
		          member.role, w.log_retention_days, w.created_at
	`, req.WorkspaceId, req.UserId, req.LogRetentionDays).Scan(
		&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.Role, &ws.LogRetentionDays, &createdAt,
	)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.PermissionDenied, "only workspace owners and admins can update retention")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to update workspace settings")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, 'workspace.retention_updated', 'workspace', $1,
		        jsonb_build_object('log_retention_days', $3::integer))
	`, req.WorkspaceId, req.UserId, req.LogRetentionDays); err != nil {
		s.logger.Error("audit workspace retention update", "error", err, "workspace_id", req.WorkspaceId)
		return nil, status.Error(codes.Internal, "failed to audit workspace settings")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit workspace settings")
	}
	ws.CreatedAt = createdAt.Format(time.RFC3339)
	return &workspacev1.UpdateWorkspaceSettingsResponse{Workspace: &ws}, nil
}

func toSlug(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
