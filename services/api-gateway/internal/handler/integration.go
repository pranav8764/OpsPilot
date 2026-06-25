package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
)

type IntegrationHandler struct {
	authClient        authv1.AuthServiceClient
	integrationClient integrationv1.IntegrationServiceClient
	logger            *slog.Logger
}

func NewIntegrationHandler(
	authClient authv1.AuthServiceClient,
	integrationClient integrationv1.IntegrationServiceClient,
	logger *slog.Logger,
) *IntegrationHandler {
	return &IntegrationHandler{
		authClient:        authClient,
		integrationClient: integrationClient,
		logger:            logger,
	}
}

func (h *IntegrationHandler) resolveUserID(r *http.Request) (string, error) {
	token, _ := r.Context().Value(middleware.TokenKey).(string)
	resp, err := h.authClient.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid || resp.UserId == "" {
		return "", fmt.Errorf("could not resolve user")
	}
	return resp.UserId, nil
}

// GitHubConfig returns frontend-safe GitHub App setup details.
// GET /api/v1/integrations/github/config
func (h *IntegrationHandler) GitHubConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	resp, err := h.integrationClient.GetGitHubConfig(r.Context(), &integrationv1.GetGitHubConfigRequest{
		UserId: userID,
	})
	if err != nil {
		h.logger.Error("get github config failed", "error", err)
		writeError(w, http.StatusInternalServerError, "GITHUB_CONFIG_FAILED", "failed to fetch GitHub configuration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"install_url":  resp.InstallUrl,
		"callback_url": resp.CallbackUrl,
		"webhook_url":  resp.WebhookUrl,
		"configured":   resp.Configured,
	})
}

// GitHubCallback handles redirect from GitHub after app installation.
// GET /api/v1/integrations/github/callback
func (h *IntegrationHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}

	instIDStr := r.URL.Query().Get("installation_id")
	if instIDStr == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "installation_id is required")
		return
	}
	var installationID int64
	_, err = fmt.Sscanf(instIDStr, "%d", &installationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid installation_id")
		return
	}

	workspaceID := r.URL.Query().Get("state")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "state (workspace_id) is required")
		return
	}

	resp, err := h.integrationClient.HandleGitHubCallback(r.Context(), &integrationv1.HandleGitHubCallbackRequest{
		UserId:         userID,
		WorkspaceId:    workspaceID,
		InstallationId: installationID,
	})
	if err != nil {
		h.logger.Error("handle callback failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CALLBACK_FAILED", "failed to process GitHub installation")
		return
	}

	// Check if the request wants JSON or HTML redirect
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]any{
			"installation": resp.Installation,
		})
	} else {
		// Redirect back to frontend dashboard
		redirectURL := fmt.Sprintf("http://localhost:3000/dashboard/%s", workspaceID)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}
}

// ListInstallations lists installations connected to workspace.
// GET /api/v1/workspaces/{workspaceId}/integrations/github/installations
func (h *IntegrationHandler) ListInstallations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	resp, err := h.integrationClient.ListInstallations(r.Context(), &integrationv1.ListInstallationsRequest{
		UserId:      userID,
		WorkspaceId: workspaceID,
	})
	if err != nil {
		h.logger.Error("list installations failed", "error", err)
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", "failed to list installations")
		return
	}

	installations := resp.Installations
	if installations == nil {
		installations = []*integrationv1.GitHubInstallation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": installations,
	})
}

// ListRepositories lists repositories accessible by a github installation.
// GET /api/v1/workspaces/{workspaceId}/integrations/github/installations/{installationId}/repositories
func (h *IntegrationHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	parts := strings.Split(r.URL.Path, "/")
	var installationID string
	for i, p := range parts {
		if p == "installations" && i+1 < len(parts) {
			installationID = parts[i+1]
			break
		}
	}
	if installationID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "installation id required")
		return
	}

	resp, err := h.integrationClient.ListRepositories(r.Context(), &integrationv1.ListRepositoriesRequest{
		UserId:               userID,
		WorkspaceId:          workspaceID,
		GithubInstallationId: installationID,
	})
	if err != nil {
		h.logger.Error("list repositories failed", "error", err)
		writeError(w, http.StatusInternalServerError, "LIST_REPOS_FAILED", "failed to list repositories")
		return
	}

	repos := resp.Repositories
	if repos == nil {
		repos = []*integrationv1.GitHubRepository{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
	})
}

// AttachRepository connects repository to a project.
// POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/repository
func (h *IntegrationHandler) AttachRepository(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	parts := strings.Split(r.URL.Path, "/")
	var projectID string
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			projectID = parts[i+1]
			break
		}
	}
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "project id required")
		return
	}

	var body struct {
		GithubInstallationID string `json:"github_installation_id"`
		RepositoryID         int64  `json:"repository_id"`
		Owner                string `json:"owner"`
		Name                 string `json:"name"`
		FullName             string `json:"full_name"`
		HTMLURL              string `json:"html_url"`
		CloneURL             string `json:"clone_url"`
		DefaultBranch        string `json:"default_branch"`
		SelectedBranch       string `json:"selected_branch"`
		Private              bool   `json:"private"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	resp, err := h.integrationClient.AttachRepository(r.Context(), &integrationv1.AttachRepositoryRequest{
		UserId:               userID,
		WorkspaceId:          workspaceID,
		ProjectId:            projectID,
		GithubInstallationId: body.GithubInstallationID,
		RepositoryId:         body.RepositoryID,
		Owner:                body.Owner,
		Name:                 body.Name,
		FullName:             body.FullName,
		HtmlUrl:              body.HTMLURL,
		CloneUrl:             body.CloneURL,
		DefaultBranch:        body.DefaultBranch,
		SelectedBranch:       body.SelectedBranch,
		Private:              body.Private,
	})
	if err != nil {
		h.logger.Error("attach repository failed", "error", err)
		writeError(w, http.StatusInternalServerError, "ATTACH_FAILED", "failed to attach repository to project")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repository_connection": resp.RepositoryConnection,
		"ingestion_job":         resp.IngestionJob,
	})
}

// GetRepository gets repository and job status details for project.
// GET /api/v1/workspaces/{workspaceId}/projects/{projectId}/repository
func (h *IntegrationHandler) GetRepository(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	parts := strings.Split(r.URL.Path, "/")
	var projectID string
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			projectID = parts[i+1]
			break
		}
	}
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "project id required")
		return
	}

	resp, err := h.integrationClient.GetRepositoryConnection(r.Context(), &integrationv1.GetRepositoryConnectionRequest{
		UserId:      userID,
		WorkspaceId: workspaceID,
		ProjectId:   projectID,
	})
	if err != nil {
		h.logger.Error("get repository connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch repository connection details")
		return
	}

	if resp.RepositoryConnection == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"repository_connection": nil,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repository_connection": resp.RepositoryConnection,
		"latest_ingestion_job":  resp.LatestIngestionJob,
	})
}

// GitHubWebhook receives verified webhook notifications from GitHub.
// POST /api/v1/webhooks/github
func (h *IntegrationHandler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	signature := r.Header.Get("X-Hub-Signature-256")

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "READ_ERROR", "failed to read webhook body")
		return
	}

	_, err = h.integrationClient.HandleGitHubWebhook(r.Context(), &integrationv1.HandleGitHubWebhookRequest{
		EventType: eventType,
		Signature: signature,
		Payload:   payload,
	})
	if err != nil {
		h.logger.Error("handle webhook failed", "error", err)
		writeError(w, http.StatusUnauthorized, "WEBHOOK_FAILED", "failed webhook verification or handling")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
