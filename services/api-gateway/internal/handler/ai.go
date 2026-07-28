package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	projectv1 "github.com/opspilot/gen/proto/project/v1"
	workspacev1 "github.com/opspilot/gen/proto/workspace/v1"
)

const maxAIResponseBytes = 5 << 20

func setWorkerRequestHeaders(workerRequest *http.Request, sourceRequest *http.Request, internalToken, contentType string) {
	if contentType != "" {
		workerRequest.Header.Set("Content-Type", contentType)
	}
	if internalToken != "" {
		workerRequest.Header.Set("Authorization", "Bearer "+internalToken)
	}
	if requestID := middleware.RequestIDFromContext(sourceRequest.Context()); requestID != "" {
		workerRequest.Header.Set("X-Request-ID", requestID)
	}
}

func relayWorkerError(w http.ResponseWriter, statusCode int, body []byte, fallbackCode, fallbackMessage string) bool {
	if statusCode < 400 {
		return false
	}
	var payload struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(body, &payload)
	message := fallbackMessage
	if (statusCode == http.StatusTooManyRequests || statusCode == http.StatusConflict || statusCode == http.StatusBadRequest) && payload.Detail != "" && len(payload.Detail) <= 1000 {
		message = payload.Detail
	}
	switch statusCode {
	case http.StatusTooManyRequests:
		writeError(w, statusCode, "USAGE_LIMIT_REACHED", message)
	case http.StatusConflict:
		writeError(w, statusCode, fallbackCode, message)
	case http.StatusBadRequest:
		writeError(w, statusCode, fallbackCode, message)
	default:
		writeError(w, http.StatusBadGateway, fallbackCode, fallbackMessage)
	}
	return true
}

type AIHandler struct {
	authClient        authv1.AuthServiceClient
	projectClient     projectv1.ProjectServiceClient
	workspaceClient   workspacev1.WorkspaceServiceClient
	integrationClient integrationv1.IntegrationServiceClient
	aiWorkerURL       string
	internalToken     string
	httpClient        *http.Client
	logger            *slog.Logger
}

// ListIncidents restores durable incident, workflow, approval, and PR state.
// GET /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents
func (h *AIHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	if workspaceID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace and project ids are required")
		return
	}
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}
	query := url.Values{"workspace_id": {workspaceID}, "project_id": {projectID}, "limit": {"25"}}
	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.aiWorkerURL+"/incidents?"+query.Encode(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INCIDENT_LIST_FAILED", "could not prepare incident list request")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AI_WORKER_UNAVAILABLE", "incident history service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(responseBody) > maxAIResponseBytes {
		writeError(w, http.StatusBadGateway, "INCIDENT_LIST_FAILED", "incident history could not be loaded")
		return
	}
	if relayWorkerError(w, response.StatusCode, responseBody, "INCIDENT_LIST_FAILED", "incident history could not be loaded") {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)
}

// DeploymentAnalysis produces a deterministic readiness report or config
// draft from the pinned repository commit.
func (h *AIHandler) DeploymentAnalysis(w http.ResponseWriter, r *http.Request, operation string) {
	if r.Method != http.MethodPost || (operation != "readiness" && operation != "generate") {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	if workspaceID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace and project ids are required")
		return
	}
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}
	payload, _ := json.Marshal(map[string]string{"workspace_id": workspaceID, "project_id": projectID, "user_id": userID})
	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.aiWorkerURL+"/deployment/"+operation, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DEPLOYMENT_ANALYSIS_FAILED", "could not prepare deployment analysis")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "application/json")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AI_WORKER_UNAVAILABLE", "deployment analysis service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(responseBody) > maxAIResponseBytes {
		writeError(w, http.StatusBadGateway, "DEPLOYMENT_ANALYSIS_FAILED", "deployment analysis could not be completed")
		return
	}
	if relayWorkerError(w, response.StatusCode, responseBody, "DEPLOYMENT_ANALYSIS_FAILED", "deployment analysis could not be completed") {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)
}

func NewAIHandler(
	authClient authv1.AuthServiceClient,
	projectClient projectv1.ProjectServiceClient,
	workspaceClient workspacev1.WorkspaceServiceClient,
	integrationClient integrationv1.IntegrationServiceClient,
	aiWorkerURL string,
	internalToken string,
	logger *slog.Logger,
) *AIHandler {
	return &AIHandler{
		authClient:        authClient,
		projectClient:     projectClient,
		workspaceClient:   workspaceClient,
		integrationClient: integrationClient,
		aiWorkerURL:       strings.TrimRight(aiWorkerURL, "/"),
		internalToken:     internalToken,
		httpClient:        &http.Client{Timeout: 40 * time.Second},
		logger:            logger,
	}
}

// UsageSummary returns plan entitlements and current counters after workspace
// membership is verified by the owning service.
func (h *AIHandler) UsageSummary(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace id is required")
		return
	}
	if _, err := h.workspaceClient.GetWorkspace(r.Context(), &workspacev1.GetWorkspaceRequest{WorkspaceId: workspaceID, UserId: userID}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "workspace access denied")
		return
	}
	query := url.Values{"workspace_id": {workspaceID}}
	workerRequest, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, h.aiWorkerURL+"/usage?"+query.Encode(), nil)
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "USAGE_UNAVAILABLE", "usage service is unavailable")
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode >= 400 {
		writeError(w, http.StatusBadGateway, "USAGE_UNAVAILABLE", "usage summary could not be loaded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// CollectAndAnalyzeLogs fetches provider logs server-to-server, then sends only
// that bounded payload through the same redaction and RCA pipeline as manual logs.
// POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/collect
func (h *AIHandler) CollectAndAnalyzeLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	if workspaceID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace and project ids are required")
		return
	}
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body struct {
		IntegrationID string `json:"integration_id"`
		LogKind       string `json:"log_kind"`
		Limit         int32  `json:"limit"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || strings.TrimSpace(body.IntegrationID) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "integration_id is required")
		return
	}
	collected, err := h.integrationClient.CollectProviderLogs(r.Context(), &integrationv1.CollectProviderLogsRequest{
		UserId: userID, WorkspaceId: workspaceID, ProjectId: projectID,
		IntegrationId: body.IntegrationID, LogKind: body.LogKind, Limit: body.Limit,
	})
	if err != nil {
		h.logger.Warn("provider log collection failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusBadGateway, "LOG_COLLECTION_FAILED", "provider logs could not be collected")
		return
	}
	h.forwardLogAnalysis(w, r, userID, workspaceID, projectID, collected.Source, collected.Content)
}

func (h *AIHandler) resolveUserID(r *http.Request) (string, error) {
	token, _ := r.Context().Value(middleware.TokenKey).(string)
	resp, err := h.authClient.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid || resp.UserId == "" {
		return "", fmt.Errorf("could not resolve user")
	}
	return resp.UserId, nil
}

// Query asks a tenant-scoped question over an indexed project.
// POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/query
func (h *AIHandler) Query(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	if workspaceID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace and project ids are required")
		return
	}

	// Project Service owns membership authorization. Do this before sending any
	// tenant identifiers to the AI worker.
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body struct {
		Question string `json:"question"`
		TopK     int    `json:"top_k"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid query request")
		return
	}
	body.Question = strings.TrimSpace(body.Question)
	if body.Question == "" || len(body.Question) > 4000 {
		writeError(w, http.StatusBadRequest, "INVALID_QUESTION", "question must contain between 1 and 4000 characters")
		return
	}
	if body.TopK == 0 {
		body.TopK = 8
	}
	if body.TopK < 1 || body.TopK > 20 {
		writeError(w, http.StatusBadRequest, "INVALID_TOP_K", "top_k must be between 1 and 20")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"workspace_id": workspaceID,
		"project_id":   projectID,
		"user_id":      userID,
		"question":     body.Question,
		"top_k":        body.TopK,
	})
	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.aiWorkerURL+"/rag/query", bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "could not prepare project query")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "application/json")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		h.logger.Error("AI worker query failed", "error", err, "workspace_id", workspaceID, "project_id", projectID)
		writeError(w, http.StatusBadGateway, "AI_WORKER_UNAVAILABLE", "project query service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(responseBody) > maxAIResponseBytes {
		writeError(w, http.StatusBadGateway, "INVALID_AI_RESPONSE", "project query returned an invalid response")
		return
	}
	if response.StatusCode >= 400 {
		h.logger.Warn("AI worker rejected query", "status", response.StatusCode, "project_id", projectID)
		relayWorkerError(w, response.StatusCode, responseBody, "QUERY_FAILED", "project query could not be completed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)
}

// AnalyzeLogs creates a durable, evidence-backed incident from pasted logs.
// POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/analyze
func (h *AIHandler) AnalyzeLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	if workspaceID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace and project ids are required")
		return
	}
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, (1<<20)+(64<<10))
	var body struct {
		Source  string `json:"source"`
		Content string `json:"content"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid log analysis request")
		return
	}
	body.Source = strings.ToLower(strings.TrimSpace(body.Source))
	if body.Source == "" {
		body.Source = "manual"
	}
	if len(body.Source) > 64 || strings.ContainsAny(body.Source, " /\\") {
		writeError(w, http.StatusBadRequest, "INVALID_SOURCE", "log source is invalid")
		return
	}
	if strings.TrimSpace(body.Content) == "" || len([]byte(body.Content)) > 1<<20 {
		writeError(w, http.StatusBadRequest, "INVALID_LOGS", "logs must contain between 1 byte and 1 MiB")
		return
	}

	h.forwardLogAnalysis(w, r, userID, workspaceID, projectID, body.Source, body.Content)
}

func (h *AIHandler) forwardLogAnalysis(w http.ResponseWriter, r *http.Request, userID, workspaceID, projectID, source, content string) {
	payload, _ := json.Marshal(map[string]any{
		"workspace_id": workspaceID, "project_id": projectID, "user_id": userID,
		"source": source, "content": content,
	})
	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.aiWorkerURL+"/analyze/logs", bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_FAILED", "could not prepare log analysis")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "application/json")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		h.logger.Error("AI worker log analysis failed", "error", err, "workspace_id", workspaceID, "project_id", projectID)
		writeError(w, http.StatusBadGateway, "AI_WORKER_UNAVAILABLE", "log analysis service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(responseBody) > maxAIResponseBytes {
		writeError(w, http.StatusBadGateway, "INVALID_AI_RESPONSE", "log analysis returned an invalid response")
		return
	}
	if response.StatusCode >= 400 {
		h.logger.Warn("AI worker rejected log analysis", "status", response.StatusCode, "project_id", projectID)
		relayWorkerError(w, response.StatusCode, responseBody, "ANALYSIS_FAILED", "log analysis could not be completed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(responseBody)
}

// ProposeFix creates a pinned, review-only diff and an expiring approval.
// It never publishes to GitHub; the tool layer may do that only after approval.
func (h *AIHandler) ProposeFix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	workspaceID, projectID := extractWorkspaceAndProjectIDs(r.URL.Path)
	incidentID := extractIncidentID(r.URL.Path)
	if workspaceID == "" || projectID == "" || incidentID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "workspace, project, and incident ids are required")
		return
	}
	if _, err := h.projectClient.GetProject(r.Context(), &projectv1.GetProjectRequest{
		ProjectId: projectID, WorkspaceId: workspaceID, UserId: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "ACCESS_DENIED", "project access denied")
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"workspace_id": workspaceID,
		"project_id":   projectID,
		"incident_id":  incidentID,
		"user_id":      userID,
	})
	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.aiWorkerURL+"/fixes/propose", bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PROPOSAL_FAILED", "could not prepare fix proposal")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "application/json")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		h.logger.Error("AI worker fix proposal failed", "error", err, "incident_id", incidentID)
		writeError(w, http.StatusBadGateway, "AI_WORKER_UNAVAILABLE", "fix proposal service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(responseBody) > maxAIResponseBytes {
		writeError(w, http.StatusBadGateway, "INVALID_AI_RESPONSE", "fix proposal returned an invalid response")
		return
	}
	if response.StatusCode >= 400 {
		h.logger.Warn("AI worker rejected fix proposal", "status", response.StatusCode, "incident_id", incidentID)
		relayWorkerError(w, response.StatusCode, responseBody, "PROPOSAL_BLOCKED", "a safe fix proposal could not be generated; re-index or add stronger evidence")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(responseBody)
}

func extractWorkspaceAndProjectIDs(path string) (string, string) {
	parts := strings.Split(path, "/")
	var workspaceID, projectID string
	for index, part := range parts {
		if part == "workspaces" && index+1 < len(parts) {
			workspaceID = parts[index+1]
		}
		if part == "projects" && index+1 < len(parts) {
			projectID = parts[index+1]
		}
	}
	return workspaceID, projectID
}

func extractIncidentID(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if part == "incidents" && index+1 < len(parts) && parts[index+1] != "analyze" {
			return parts[index+1]
		}
	}
	return ""
}

// GetEvalReport proxies the request to the AI worker to fetch the evaluation report.
func (h *AIHandler) GetEvalReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, err := h.resolveUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "could not identify user")
		return
	}
	_ = userID // Auth succeeded

	workerRequest, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.aiWorkerURL+"/eval/report", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create worker request")
		return
	}
	setWorkerRequestHeaders(workerRequest, r, h.internalToken, "")
	response, err := h.httpClient.Do(workerRequest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "EVAL_UNAVAILABLE", "evaluation service is unavailable")
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20)) // 10MB limit
	if err != nil || response.StatusCode >= 400 {
		writeError(w, http.StatusBadGateway, "EVAL_UNAVAILABLE", "evaluation report could not be loaded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

