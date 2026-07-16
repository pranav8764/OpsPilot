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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start project transaction")
	}
	defer tx.Rollback(ctx)

	// Project creation is an administrative action and is serialized with the
	// workspace row so concurrent requests cannot bypass the plan limit.
	var memberRole string
	err = tx.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`,
		req.WorkspaceId, req.UserId,
	).Scan(&memberRole)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.PermissionDenied, "you are not a member of this workspace")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "membership check failed")
	}
	if memberRole != "owner" && memberRole != "admin" {
		return nil, status.Error(codes.PermissionDenied, "only owners and admins can create projects")
	}
	var plan string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(plan, 'free') FROM workspaces WHERE id = $1 FOR UPDATE", req.WorkspaceId).Scan(&plan); err != nil {
		return nil, status.Error(codes.NotFound, "workspace not found")
	}
	var projectCount int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM projects WHERE workspace_id = $1 AND status != 'archived'", req.WorkspaceId).Scan(&projectCount); err != nil {
		return nil, status.Error(codes.Internal, "failed to check project limit")
	}
	if projectCount >= projectLimit(plan) {
		return nil, status.Errorf(codes.ResourceExhausted, "%s plan project limit reached (%d/%d)", plan, projectCount, projectLimit(plan))
	}

	var p projectv1.Project
	var createdAt, updatedAt time.Time

	err = tx.QueryRow(ctx, `
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

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id)
		VALUES ($1, $2, 'project.created', 'project', $3)
	`, req.WorkspaceId, req.UserId, p.Id); err != nil {
		return nil, status.Error(codes.Internal, "failed to audit project creation")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit project creation")
	}

	p.CreatedAt = createdAt.Format(time.RFC3339)
	p.UpdatedAt = updatedAt.Format(time.RFC3339)
	s.logger.Info("project created", "id", p.Id, "workspace_id", req.WorkspaceId)
	return &projectv1.CreateProjectResponse{Project: &p}, nil
}

func projectLimit(plan string) int {
	switch plan {
	case "team":
		return 50
	case "pro":
		return 25
	default:
		return 3
	}
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
	).Scan(&role); err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.PermissionDenied, "access denied")
		}
		return nil, status.Error(codes.Internal, "membership check failed")
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
	).Scan(&role); err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.PermissionDenied, "access denied")
		}
		return nil, status.Error(codes.Internal, "membership check failed")
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start deletion transaction")
	}
	defer tx.Rollback(ctx)
	var projectName string
	if err := tx.QueryRow(ctx, "SELECT name FROM projects WHERE id = $1 AND workspace_id = $2 FOR UPDATE", req.ProjectId, req.WorkspaceId).Scan(&projectName); err != nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, metadata)
		VALUES ($1, $2, 'project.deleted', 'project',
		        jsonb_build_object('project_id', $3::text, 'project_name', $4::text))
	`, req.WorkspaceId, req.UserId, req.ProjectId, projectName); err != nil {
		s.logger.Error("audit project deletion", "error", err, "project_id", req.ProjectId)
		return nil, status.Error(codes.Internal, "failed to audit project deletion")
	}
	if _, err := tx.Exec(ctx, "DELETE FROM projects WHERE id = $1 AND workspace_id = $2", req.ProjectId, req.WorkspaceId); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete project data")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit project deletion")
	}
	return &projectv1.DeleteProjectResponse{Success: true}, nil
}
