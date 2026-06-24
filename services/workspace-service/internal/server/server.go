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
		RETURNING id, name, COALESCE(slug,''), owner_id, plan, created_at
	`, req.Name, slug, req.OwnerId).Scan(
		&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &createdAt,
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

	_, _ = tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'workspace.created', 'workspace', $1)
	`, ws.Id, req.OwnerId)

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
		SELECT w.id, w.name, COALESCE(w.slug,''), w.owner_id, w.plan, wm.role, w.created_at
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
		if err := rows.Scan(&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.Role, &createdAt); err != nil {
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
		SELECT w.id, w.name, COALESCE(w.slug,''), w.owner_id, w.plan, wm.role, w.created_at
		FROM workspaces w
		JOIN workspace_members wm ON w.id = wm.workspace_id
		WHERE w.id = $1 AND wm.user_id = $2
	`, req.WorkspaceId, req.UserId).Scan(
		&ws.Id, &ws.Name, &ws.Slug, &ws.OwnerId, &ws.Plan, &ws.Role, &createdAt,
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
