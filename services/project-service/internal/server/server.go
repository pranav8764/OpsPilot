package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	projectv1 "github.com/opspilot/gen/proto/project/v1"
)

type ProjectServer struct {
	projectv1.UnimplementedProjectServiceServer
	db     *pgxpool.Pool
	logger *slog.Logger
}

func New(db *pgxpool.Pool, logger *slog.Logger) *ProjectServer {
	return &ProjectServer{db: db, logger: logger}
}

func (s *ProjectServer) CreateProject(ctx context.Context, req *projectv1.CreateProjectRequest) (*projectv1.CreateProjectResponse, error) {
	if req.WorkspaceId == "" || req.Name == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id, name, and user_id are required")
	}

	// Verify membership
	var memberRole string
	err := s.db.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`,
		req.WorkspaceId, req.UserId,
	).Scan(&memberRole)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.PermissionDenied, "you are not a member of this workspace")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "membership check failed")
	}

	var p projectv1.Project
	var createdAt, updatedAt time.Time

	err = s.db.QueryRow(ctx, `
		INSERT INTO projects (workspace_id, name, description, status, indexing_status, created_by)
		VALUES ($1, $2, $3, 'active', 'not_started', $4)
		RETURNING id, workspace_id, name,
		          COALESCE(description,''), COALESCE(repo_url,''),
		          COALESCE(repo_provider,'github'), COALESCE(default_branch,'main'),
		          status, indexing_status, COALESCE(created_by::text,''),
		          created_at, updated_at
	`, req.WorkspaceId, req.Name, req.Description, req.UserId,
	).Scan(
		&p.Id, &p.WorkspaceId, &p.Name, &p.Description, &p.RepoUrl,
		&p.RepoProvider, &p.DefaultBranch, &p.Status, &p.IndexingStatus,
		&p.CreatedBy, &createdAt, &updatedAt,
	)
	if err != nil {
		s.logger.Error("create project failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to create project")
	}

	_, _ = s.db.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'project.created', 'project', $3)
	`, req.WorkspaceId, req.UserId, p.Id)

	p.CreatedAt = createdAt.Format(time.RFC3339)
	p.UpdatedAt = updatedAt.Format(time.RFC3339)
	s.logger.Info("project created", "id", p.Id, "workspace_id", req.WorkspaceId)
	return &projectv1.CreateProjectResponse{Project: &p}, nil
}

func (s *ProjectServer) GetProjects(ctx context.Context, req *projectv1.GetProjectsRequest) (*projectv1.GetProjectsResponse, error) {
	if req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id and user_id are required")
	}

	// Verify membership
	var role string
	if err := s.db.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		req.WorkspaceId, req.UserId,
	).Scan(&role); err == pgx.ErrNoRows {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, workspace_id, name,
		       COALESCE(description,''), COALESCE(repo_url,''),
		       COALESCE(repo_provider,'github'), COALESCE(default_branch,'main'),
		       status, indexing_status, COALESCE(created_by::text,''),
		       created_at, updated_at
		FROM projects
		WHERE workspace_id = $1 AND status != 'archived'
		ORDER BY created_at DESC
	`, req.WorkspaceId)
	if err != nil {
		return nil, status.Error(codes.Internal, "query failed")
	}
	defer rows.Close()

	var projects []*projectv1.Project
	for rows.Next() {
		var p projectv1.Project
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&p.Id, &p.WorkspaceId, &p.Name, &p.Description, &p.RepoUrl,
			&p.RepoProvider, &p.DefaultBranch, &p.Status, &p.IndexingStatus,
			&p.CreatedBy, &createdAt, &updatedAt,
		); err != nil {
			continue
		}
		p.CreatedAt = createdAt.Format(time.RFC3339)
		p.UpdatedAt = updatedAt.Format(time.RFC3339)
		projects = append(projects, &p)
	}
	return &projectv1.GetProjectsResponse{Projects: projects}, nil
}

func (s *ProjectServer) GetProject(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
	if req.ProjectId == "" || req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id, workspace_id, user_id required")
	}

	// Verify membership
	var role string
	if err := s.db.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		req.WorkspaceId, req.UserId,
	).Scan(&role); err == pgx.ErrNoRows {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}

	var p projectv1.Project
	var createdAt, updatedAt time.Time

	err := s.db.QueryRow(ctx, `
		SELECT id, workspace_id, name,
		       COALESCE(description,''), COALESCE(repo_url,''),
		       COALESCE(repo_provider,'github'), COALESCE(default_branch,'main'),
		       status, indexing_status, COALESCE(created_by::text,''),
		       created_at, updated_at
		FROM projects WHERE id = $1 AND workspace_id = $2
	`, req.ProjectId, req.WorkspaceId,
	).Scan(
		&p.Id, &p.WorkspaceId, &p.Name, &p.Description, &p.RepoUrl,
		&p.RepoProvider, &p.DefaultBranch, &p.Status, &p.IndexingStatus,
		&p.CreatedBy, &createdAt, &updatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("project %s not found", req.ProjectId))
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	p.CreatedAt = createdAt.Format(time.RFC3339)
	p.UpdatedAt = updatedAt.Format(time.RFC3339)
	return &projectv1.GetProjectResponse{Project: &p}, nil
}

func (s *ProjectServer) DeleteProject(ctx context.Context, req *projectv1.DeleteProjectRequest) (*projectv1.DeleteProjectResponse, error) {
	if req.ProjectId == "" || req.WorkspaceId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "all fields required")
	}

	// Only owner/admin can delete
	var role string
	_ = s.db.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		req.WorkspaceId, req.UserId,
	).Scan(&role)

	if role != "owner" && role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "only owners and admins can delete projects")
	}

	result, err := s.db.Exec(ctx, `
		UPDATE projects SET status = 'archived', updated_at = NOW()
		WHERE id = $1 AND workspace_id = $2
	`, req.ProjectId, req.WorkspaceId)
	if err != nil || result.RowsAffected() == 0 {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return &projectv1.DeleteProjectResponse{Success: true}, nil
}
