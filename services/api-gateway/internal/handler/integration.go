package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcErrorResponse is the client-visible shape a gRPC status code maps onto.
type grpcErrorResponse struct {
	httpStatus int
	errorCode  string
	message    string
}

// grpcErrorResponses maps the gRPC status codes the Integration Service uses
// deliberately onto a real HTTP status and a stable, machine-readable error
// code the browser can branch on. Every entry carries a fixed message of our
// own: upstream error text may embed installation tokens, webhook signatures,
// or raw GitHub response bodies, so it is never forwarded to the client.
var grpcErrorResponses = map[codes.Code]grpcErrorResponse{
	codes.FailedPrecondition: {http.StatusConflict, "INSTALLATION_REVOKED", "this GitHub connection is no longer usable; reconnect the GitHub App"},
	codes.NotFound:           {http.StatusNotFound, "NOT_FOUND", "the requested resource was not found"},
	codes.PermissionDenied:   {http.StatusForbidden, "FORBIDDEN", "you do not have access to this resource"},
	codes.Unauthenticated:    {http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required"},
	codes.InvalidArgument:    {http.StatusBadRequest, "INVALID_REQUEST", "the request was rejected as invalid"},
	codes.Unavailable:        {http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "the service is temporarily unavailable; please retry"},
}

// resolveGRPCError picks the HTTP status, error code, and message for a gRPC
// error. Codes outside the mapping — including plain errors carrying no status
// — fall through to the caller's own defaults so existing behaviour is
// unchanged for everything the Integration Service does not classify.
func resolveGRPCError(err error, defaultStatus int, defaultCode, defaultMessage string) (int, string, string) {
	if err == nil {
		return defaultStatus, defaultCode, defaultMessage
	}
	grpcStatus, ok := status.FromError(err)
	if !ok || grpcStatus == nil {
		return defaultStatus, defaultCode, defaultMessage
	}
	if mapped, found := grpcErrorResponses[grpcStatus.Code()]; found {
		return mapped.httpStatus, mapped.errorCode, mapped.message
	}
	return defaultStatus, defaultCode, defaultMessage
}

// writeGRPCError renders a gRPC error through resolveGRPCError using the shared
// error envelope. The upstream error string is never included in the response.
func writeGRPCError(w http.ResponseWriter, err error, defaultStatus int, defaultCode, defaultMessage string) {
	httpStatus, errorCode, message := resolveGRPCError(err, defaultStatus, defaultCode, defaultMessage)
	writeError(w, httpStatus, errorCode, message)
}

type IntegrationHandler struct {
	authClient        authv1.AuthServiceClient
	integrationClient integrationv1.IntegrationServiceClient
	logger            *slog.Logger
	frontendURL       string
}

func NewIntegrationHandler(
	authClient authv1.AuthServiceClient,
	integrationClient integrationv1.IntegrationServiceClient,
	frontendURL string,
	logger *slog.Logger,
) *IntegrationHandler {
	return &IntegrationHandler{
		authClient:        authClient,
		integrationClient: integrationClient,
		frontendURL:       strings.TrimRight(frontendURL, "/"),
		logger:            logger,
	}
}

// CreateGitHubInstallSession returns a one-time GitHub installation URL whose
// opaque state is bound server-side to the authenticated user and workspace.
// POST /api/v1/workspaces/{workspaceId}/integrations/github/install-session
func (h *IntegrationHandler) CreateGitHubInstallSession(w http.ResponseWriter, r *http.Request) {
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
	resp, err := h.integrationClient.CreateGitHubInstallSession(r.Context(), &integrationv1.CreateGitHubInstallSessionRequest{
		UserId: userID, WorkspaceId: workspaceID,
	})
	if err != nil {
		h.logger.Error("create GitHub install session failed", "error", err)
		writeGRPCError(w, err, http.StatusBadGateway, "GITHUB_INSTALL_SESSION_FAILED", "failed to prepare GitHub installation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"install_url": resp.InstallUrl, "expires_at": resp.ExpiresAt})
}

// UpsertLogProvider stores an encrypted, least-privilege provider token. The
// token is write-only and never appears in API responses.
// POST /api/v1/workspaces/{workspaceId}/integrations/log-providers
func (h *IntegrationHandler) UpsertLogProvider(w http.ResponseWriter, r *http.Request) {
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
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body struct {
		Provider     string `json:"provider"`
		DisplayName  string `json:"display_name"`
		Token        string `json:"token"`
		ProjectID    string `json:"project_id"`
		DeploymentID string `json:"deployment_id"`
		ScopeID      string `json:"scope_id"`
		TokenKind    string `json:"token_kind"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if workspaceID == "" || decoder.Decode(&body) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "workspace and valid provider connection details are required")
		return
	}
	resp, err := h.integrationClient.UpsertLogProviderConnection(r.Context(), &integrationv1.UpsertLogProviderConnectionRequest{
		UserId: userID, WorkspaceId: workspaceID, Provider: body.Provider,
		DisplayName: body.DisplayName, Token: body.Token, ProjectId: body.ProjectID,
		DeploymentId: body.DeploymentID, ScopeId: body.ScopeID, TokenKind: body.TokenKind,
	})
	if err != nil {
		h.logger.Warn("upsert log provider failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusBadRequest, "LOG_PROVIDER_SAVE_FAILED", "provider connection could not be saved; check its fields and encryption configuration")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connection": resp.Connection})
}

// ListLogProviders returns connection metadata without credentials.
// GET /api/v1/workspaces/{workspaceId}/integrations/log-providers
func (h *IntegrationHandler) ListLogProviders(w http.ResponseWriter, r *http.Request) {
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
	resp, err := h.integrationClient.ListLogProviderConnections(r.Context(), &integrationv1.ListLogProviderConnectionsRequest{
		UserId: userID, WorkspaceId: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "LOG_PROVIDER_LIST_FAILED", "failed to list provider connections")
		return
	}
	connections := resp.Connections
	if connections == nil {
		connections = []*integrationv1.LogProviderConnection{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": connections})
}

// ListConnectedRepositories returns every repository connection visible to the
// caller. Scoped to one workspace when the path carries a workspace id, otherwise
// across every workspace the caller belongs to.
// GET /api/v1/repositories
// GET /api/v1/workspaces/{workspaceId}/repositories
func (h *IntegrationHandler) ListConnectedRepositories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	// Empty on the cross-workspace route: extractWorkspaceID scans for a
	// "workspaces" segment and returns "" when there is none. The integration
	// service then scopes by membership instead of by an explicit id.
	workspaceID := extractWorkspaceID(r.URL.Path)

	resp, err := h.integrationClient.ListRepositoryConnections(r.Context(), &integrationv1.ListRepositoryConnectionsRequest{
		UserId:      userID,
		WorkspaceId: workspaceID,
	})
	if err != nil {
		h.logger.Error("list repository connections failed", "error", err, "workspace_id", workspaceID)
		writeGRPCError(w, err, http.StatusBadGateway, "REPOSITORY_LIST_FAILED", "failed to list connected repositories")
		return
	}
	connections := resp.Connections
	if connections == nil {
		connections = []*integrationv1.RepositoryConnectionSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": connections})
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
		writeGRPCError(w, err, http.StatusInternalServerError, "GITHUB_CONFIG_FAILED", "failed to fetch GitHub configuration")
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instIDStr := r.URL.Query().Get("installation_id")
	if instIDStr == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "installation_id is required")
		return
	}
	var installationID int64
	_, err := fmt.Sscanf(instIDStr, "%d", &installationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid installation_id")
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "state is required")
		return
	}
	resp, err := h.integrationClient.CompleteGitHubInstallSession(r.Context(), &integrationv1.CompleteGitHubInstallSessionRequest{
		State: state, InstallationId: installationID,
	})
	if err != nil {
		h.logger.Error("handle callback failed", "error", err)
		writeGRPCError(w, err, http.StatusInternalServerError, "CALLBACK_FAILED", "failed to process GitHub installation")
		return
	}

	redirectURL := h.frontendURL + "/dashboard/" + url.PathEscape(resp.WorkspaceId) + "?github=connected"
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
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
		writeGRPCError(w, err, http.StatusInternalServerError, "LIST_FAILED", "failed to list installations")
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
		writeGRPCError(w, err, http.StatusInternalServerError, "LIST_REPOS_FAILED", "failed to list repositories")
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
		writeGRPCError(w, err, http.StatusInternalServerError, "ATTACH_FAILED", "failed to attach repository to project")
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
		writeGRPCError(w, err, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch repository connection details")
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

// DecideFixApproval approves/rejects a pinned fix proposal. The Integration
// Service independently enforces role, expiry, payload integrity, and tool policy.
func (h *IntegrationHandler) DecideFixApproval(w http.ResponseWriter, r *http.Request) {
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
	parts := strings.Split(r.URL.Path, "/")
	var projectID, approvalID string
	for index, part := range parts {
		if part == "projects" && index+1 < len(parts) {
			projectID = parts[index+1]
		}
		if part == "approvals" && index+1 < len(parts) {
			approvalID = parts[index+1]
		}
	}
	if workspaceID == "" || projectID == "" || approvalID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace, project, and approval ids are required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid approval decision")
		return
	}
	body.Decision = strings.ToLower(strings.TrimSpace(body.Decision))
	if body.Decision != "approve" && body.Decision != "reject" {
		writeError(w, http.StatusBadRequest, "INVALID_DECISION", "decision must be approve or reject")
		return
	}

	response, err := h.integrationClient.DecideFixApproval(r.Context(), &integrationv1.DecideFixApprovalRequest{
		UserId: userID, WorkspaceId: workspaceID, ProjectId: projectID,
		ApprovalId: approvalID, Decision: body.Decision, Reason: body.Reason,
	})
	if err != nil {
		h.logger.Error("fix approval decision failed", "error", err, "approval_id", approvalID)
		writeError(w, http.StatusConflict, "APPROVAL_FAILED", "approval could not be applied or the draft PR could not be created")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"approval_id":         response.ApprovalId,
		"approval_status":     response.ApprovalStatus,
		"pull_request_url":    response.PullRequestUrl,
		"pull_request_number": response.PullRequestNumber,
		"branch":              response.Branch,
	})
}
