package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	projectv1 "github.com/opspilot/gen/proto/project/v1"
)

// ProjectHandler handles project HTTP endpoints.
type ProjectHandler struct {
	authClient    authv1.AuthServiceClient
	projectClient projectv1.ProjectServiceClient
	logger        *slog.Logger
}

func NewProjectHandler(
	authClient authv1.AuthServiceClient,
	projectClient projectv1.ProjectServiceClient,
	logger *slog.Logger,
) *ProjectHandler {
	return &ProjectHandler{authClient: authClient, projectClient: projectClient, logger: logger}
}

func (h *ProjectHandler) resolveUserID(r *http.Request) (string, error) {
	token, _ := r.Context().Value(middleware.TokenKey).(string)
	resp, err := h.authClient.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid || resp.UserId == "" {
		return "", fmt.Errorf("could not resolve user")
	}
	return resp.UserId, nil
}

// List returns all projects in a workspace.
// GET /api/v1/workspaces/{workspaceId}/projects
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	workspaceID := extractWorkspaceID(r.URL.Path)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace id required")
		return
	}

	resp, err := h.projectClient.GetProjects(r.Context(), &projectv1.GetProjectsRequest{
		WorkspaceId: workspaceID,
		UserId:      userID,
	})
	if err != nil {
		h.logger.Error("get projects failed", "error", err)
		writeError(w, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch projects")
		return
	}

	projects := resp.Projects
	if projects == nil {
		projects = []*projectv1.Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// Create creates a new project.
// POST /api/v1/workspaces/{workspaceId}/projects
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	workspaceID := extractWorkspaceID(r.URL.Path)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace id required")
		return
	}

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "name is required")
		return
	}

	resp, err := h.projectClient.CreateProject(r.Context(), &projectv1.CreateProjectRequest{
		WorkspaceId: workspaceID,
		Name:        body.Name,
		Description: body.Description,
		UserId:      userID,
	})
	if err != nil {
		h.logger.Error("create project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "failed to create project")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"project": resp.Project})
}

// extractWorkspaceID extracts workspace ID from path like /api/v1/workspaces/{id}/projects
func extractWorkspaceID(path string) string {
	parts := strings.Split(path, "/")
	// /api/v1/workspaces/{id}/projects → ["", "api", "v1", "workspaces", "{id}", "projects"]
	for i, p := range parts {
		if p == "workspaces" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// suppress unused import warning
var _ = fmt.Sprintf
