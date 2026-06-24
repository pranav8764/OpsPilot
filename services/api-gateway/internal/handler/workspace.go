package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	workspacev1 "github.com/opspilot/gen/proto/workspace/v1"
)

// WorkspaceHandler handles workspace HTTP endpoints.
type WorkspaceHandler struct {
	authClient      authv1.AuthServiceClient
	workspaceClient workspacev1.WorkspaceServiceClient
	logger          *slog.Logger
}

func NewWorkspaceHandler(
	authClient authv1.AuthServiceClient,
	workspaceClient workspacev1.WorkspaceServiceClient,
	logger *slog.Logger,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		authClient:      authClient,
		workspaceClient: workspaceClient,
		logger:          logger,
	}
}

// resolveUserID gets the internal user UUID from the Clerk token.
func (h *WorkspaceHandler) resolveUserID(r *http.Request) (string, error) {
	token, _ := r.Context().Value(middleware.TokenKey).(string)
	resp, err := h.authClient.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid || resp.UserId == "" {
		return "", errors.New("could not resolve user")
	}
	return resp.UserId, nil
}

// List returns all workspaces for the authenticated user.
// GET /api/v1/workspaces
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	resp, err := h.workspaceClient.GetWorkspaces(r.Context(), &workspacev1.GetWorkspacesRequest{
		UserId: userID,
	})
	if err != nil {
		h.logger.Error("get workspaces failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch workspaces")
		return
	}

	workspaces := resp.Workspaces
	if workspaces == nil {
		workspaces = []*workspacev1.Workspace{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": workspaces})
}

// Create creates a new workspace.
// POST /api/v1/workspaces
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "name is required")
		return
	}

	resp, err := h.workspaceClient.CreateWorkspace(r.Context(), &workspacev1.CreateWorkspaceRequest{
		Name:    body.Name,
		OwnerId: userID,
	})
	if err != nil {
		h.logger.Error("create workspace failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "failed to create workspace")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"workspace": resp.Workspace})
}

// Get returns a single workspace.
// GET /api/v1/workspaces/{id}
func (h *WorkspaceHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	workspaceID := extractPathParam(r.URL.Path, "/api/v1/workspaces/")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace id is required")
		return
	}

	resp, err := h.workspaceClient.GetWorkspace(r.Context(), &workspacev1.GetWorkspaceRequest{
		WorkspaceId: workspaceID,
		UserId:      userID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "workspace not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"workspace": resp.Workspace})
}

func extractPathParam(path, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	// Remove trailing slash or sub-path
	parts := strings.SplitN(trimmed, "/", 2)
	return parts[0]
}
