package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var providerHTTPClient = &http.Client{Timeout: 35 * time.Second}
var vercelAPIBaseURL = "https://api.vercel.com"
var railwayGraphQLURL = "https://backboard.railway.com/graphql/v2"

const maxProviderLogResponseBytes = 2 << 20
const maxNormalizedLogBytes = 1 << 20

type logProviderMetadata struct {
	ProjectID    string `json:"project_id"`
	DeploymentID string `json:"deployment_id"`
	ScopeID      string `json:"scope_id"`
	TokenKind    string `json:"token_kind"`
}

func validateProviderConnection(req *integrationv1.UpsertLogProviderConnectionRequest) (logProviderMetadata, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	projectID := strings.TrimSpace(req.ProjectId)
	deploymentID := strings.TrimSpace(req.DeploymentId)
	scopeID := strings.TrimSpace(req.ScopeId)
	tokenKind := strings.ToLower(strings.TrimSpace(req.TokenKind))
	if tokenKind == "" {
		tokenKind = "bearer"
	}
	if provider != "vercel" && provider != "railway" {
		return logProviderMetadata{}, status.Error(codes.InvalidArgument, "provider must be vercel or railway")
	}
	if len(req.Token) < 8 || len(req.Token) > 4096 || strings.ContainsAny(req.Token, "\r\n") {
		return logProviderMetadata{}, status.Error(codes.InvalidArgument, "provider token is invalid")
	}
	for _, value := range []string{projectID, deploymentID, scopeID} {
		if len(value) > 256 || strings.ContainsAny(value, "\r\n") {
			return logProviderMetadata{}, status.Error(codes.InvalidArgument, "provider identifier is invalid")
		}
	}
	if deploymentID == "" || (provider == "vercel" && projectID == "") {
		return logProviderMetadata{}, status.Error(codes.InvalidArgument, "deployment_id is required, and Vercel also requires project_id")
	}
	if provider == "vercel" && tokenKind != "bearer" {
		return logProviderMetadata{}, status.Error(codes.InvalidArgument, "Vercel requires bearer token authentication")
	}
	if provider == "railway" && tokenKind != "bearer" && tokenKind != "project_token" {
		return logProviderMetadata{}, status.Error(codes.InvalidArgument, "Railway token_kind must be bearer or project_token")
	}
	return logProviderMetadata{ProjectID: projectID, DeploymentID: deploymentID, ScopeID: scopeID, TokenKind: tokenKind}, nil
}

func (s *IntegrationServer) UpsertLogProviderConnection(ctx context.Context, req *integrationv1.UpsertLogProviderConnectionRequest) (*integrationv1.UpsertLogProviderConnectionResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and workspace_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin"); err != nil {
		return nil, err
	}
	metadata, err := validateProviderConnection(req)
	if err != nil {
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	encrypted, err := encryptCredential(s.cfg.CredentialEncryptionKey, providerCredential{
		Token: req.Token, TokenKind: metadata.TokenKind,
	})
	if err != nil {
		s.logger.Error("provider credential encryption unavailable", "error", err)
		return nil, status.Error(codes.FailedPrecondition, "provider credential encryption is not configured")
	}
	metadataJSON, _ := json.Marshal(metadata)
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = provider + " / " + metadata.DeploymentID
	}
	if len(displayName) > 120 {
		return nil, status.Error(codes.InvalidArgument, "display_name is too long")
	}
	externalID := metadata.ProjectID + ":" + metadata.DeploymentID
	var connection integrationv1.LogProviderConnection
	var storedMetadata []byte
	var createdAt, updatedAt time.Time
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start provider connection transaction")
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `
		INSERT INTO integrations (
			workspace_id, provider, auth_type, status, external_account_id,
			external_account_name, encrypted_credentials, scopes, metadata, created_by
		)
		VALUES ($1, $2, $3, 'active', $4, $5, $6, ARRAY['logs:read'], $7::jsonb, $8)
		ON CONFLICT (workspace_id, provider, external_account_id) DO UPDATE
		SET auth_type = EXCLUDED.auth_type, status = 'active',
			external_account_name = EXCLUDED.external_account_name,
			encrypted_credentials = EXCLUDED.encrypted_credentials,
			metadata = EXCLUDED.metadata, updated_at = NOW()
		RETURNING id, provider, external_account_name, status, metadata, created_at, updated_at
	`, req.WorkspaceId, provider, metadata.TokenKind, externalID, displayName, encrypted, metadataJSON, req.UserId).Scan(
		&connection.Id, &connection.Provider, &connection.DisplayName, &connection.Status,
		&storedMetadata, &createdAt, &updatedAt,
	)
	if err != nil {
		s.logger.Error("upsert log provider connection failed", "error", err, "provider", provider)
		return nil, status.Error(codes.Internal, "failed to save provider connection")
	}
	populateLogProviderConnection(&connection, storedMetadata, createdAt, updatedAt)
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, 'integration.logs.connected', 'integration', $3,
			jsonb_build_object('provider', $4::text, 'deployment_id', $5::text))
	`, req.WorkspaceId, req.UserId, connection.Id, provider, metadata.DeploymentID); err != nil {
		s.logger.Error("audit log provider connection failed", "error", err, "provider", provider)
		return nil, status.Error(codes.Internal, "failed to audit provider connection")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to commit provider connection")
	}
	return &integrationv1.UpsertLogProviderConnectionResponse{Connection: &connection}, nil
}

func (s *IntegrationServer) ListLogProviderConnections(ctx context.Context, req *integrationv1.ListLogProviderConnectionsRequest) (*integrationv1.ListLogProviderConnectionsResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and workspace_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, provider, external_account_name, status, metadata, created_at, updated_at
		FROM integrations
		WHERE workspace_id = $1 AND provider IN ('vercel', 'railway') AND status = 'active'
		ORDER BY created_at DESC
	`, req.WorkspaceId)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list provider connections")
	}
	defer rows.Close()
	connections := make([]*integrationv1.LogProviderConnection, 0)
	for rows.Next() {
		connection := &integrationv1.LogProviderConnection{}
		var metadata []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&connection.Id, &connection.Provider, &connection.DisplayName, &connection.Status, &metadata, &createdAt, &updatedAt); err != nil {
			return nil, status.Error(codes.Internal, "failed to read provider connection")
		}
		populateLogProviderConnection(connection, metadata, createdAt, updatedAt)
		connections = append(connections, connection)
	}
	return &integrationv1.ListLogProviderConnectionsResponse{Connections: connections}, nil
}

func populateLogProviderConnection(connection *integrationv1.LogProviderConnection, metadataJSON []byte, createdAt, updatedAt time.Time) {
	var metadata logProviderMetadata
	_ = json.Unmarshal(metadataJSON, &metadata)
	connection.ProjectId = metadata.ProjectID
	connection.DeploymentId = metadata.DeploymentID
	connection.ScopeId = metadata.ScopeID
	connection.TokenKind = metadata.TokenKind
	connection.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	connection.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
}

func (s *IntegrationServer) CollectProviderLogs(ctx context.Context, req *integrationv1.CollectProviderLogsRequest) (*integrationv1.CollectProviderLogsResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" || req.ProjectId == "" || req.IntegrationId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, workspace_id, project_id, and integration_id are required")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin", "developer", "viewer"); err != nil {
		return nil, err
	}
	var projectExists bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND workspace_id = $2)", req.ProjectId, req.WorkspaceId).Scan(&projectExists); err != nil || !projectExists {
		return nil, status.Error(codes.NotFound, "project was not found in workspace")
	}
	var provider, encrypted string
	var metadataJSON []byte
	if err := s.db.QueryRow(ctx, `
		SELECT provider, encrypted_credentials, metadata
		FROM integrations
		WHERE id = $1 AND workspace_id = $2 AND provider IN ('vercel', 'railway') AND status = 'active'
	`, req.IntegrationId, req.WorkspaceId).Scan(&provider, &encrypted, &metadataJSON); err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "active log provider connection not found")
	} else if err != nil {
		return nil, status.Error(codes.Internal, "failed to load provider connection")
	}
	credential, err := decryptCredential(s.cfg.CredentialEncryptionKey, encrypted)
	if err != nil {
		s.logger.Error("decrypt provider credential failed", "error", err, "integration_id", req.IntegrationId)
		return nil, status.Error(codes.FailedPrecondition, "provider credential could not be decrypted")
	}
	var metadata logProviderMetadata
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return nil, status.Error(codes.Internal, "provider connection metadata is invalid")
	}
	limit := int(req.Limit)
	if limit == 0 {
		limit = 300
	}
	if limit < 1 || limit > 1000 {
		return nil, status.Error(codes.InvalidArgument, "log limit must be between 1 and 1000")
	}
	logKind := strings.ToLower(strings.TrimSpace(req.LogKind))
	if logKind == "" {
		logKind = "runtime"
	}
	if logKind != "runtime" && logKind != "build" {
		return nil, status.Error(codes.InvalidArgument, "log_kind must be runtime or build")
	}

	var content string
	if provider == "vercel" {
		content, err = fetchVercelLogs(ctx, credential.Token, metadata, logKind, limit)
	} else {
		content, err = fetchRailwayLogs(ctx, credential, metadata, logKind, limit)
	}
	if err != nil {
		s.logger.Warn("provider log collection failed", "provider", provider, "integration_id", req.IntegrationId, "error", err)
		return nil, status.Error(codes.Unavailable, "provider log collection failed")
	}
	lineCount := int32(strings.Count(content, "\n"))
	if content != "" && !strings.HasSuffix(content, "\n") {
		lineCount++
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, 'integration.logs.collected', 'integration', $3,
			jsonb_build_object('provider', $4::text, 'project_id', $5::text,
			                   'deployment_id', $6::text, 'line_count', $7::integer))
	`, req.WorkspaceId, req.UserId, req.IntegrationId, provider, req.ProjectId, metadata.DeploymentID, lineCount); err != nil {
		s.logger.Error("audit provider log collection failed", "error", err, "integration_id", req.IntegrationId)
		return nil, status.Error(codes.Internal, "failed to audit provider log collection")
	}
	return &integrationv1.CollectProviderLogsResponse{
		Source: provider + "." + logKind, Content: content, Provider: provider,
		DeploymentId: metadata.DeploymentID, LineCount: lineCount,
	}, nil
}

func fetchVercelLogs(ctx context.Context, token string, metadata logProviderMetadata, logKind string, limit int) (string, error) {
	var endpoint string
	if logKind == "build" {
		endpoint = fmt.Sprintf("%s/v3/deployments/%s/events", strings.TrimRight(vercelAPIBaseURL, "/"), url.PathEscape(metadata.DeploymentID))
	} else {
		endpoint = fmt.Sprintf("%s/v1/projects/%s/deployments/%s/runtime-logs", strings.TrimRight(vercelAPIBaseURL, "/"), url.PathEscape(metadata.ProjectID), url.PathEscape(metadata.DeploymentID))
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	if logKind == "build" {
		query.Set("direction", "backward")
		query.Set("follow", "0")
		query.Set("limit", strconv.Itoa(limit))
	}
	if metadata.ScopeID != "" {
		query.Set("teamId", metadata.ScopeID)
	}
	parsed.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Accept", "application/json, application/x-ndjson")
	return executeAndNormalizeProviderRequest(httpRequest, limit)
}

func fetchRailwayLogs(ctx context.Context, credential providerCredential, metadata logProviderMetadata, logKind string, limit int) (string, error) {
	field := "deploymentLogs"
	if logKind == "build" {
		field = "buildLogs"
	}
	payload, _ := json.Marshal(map[string]any{
		"query":     fmt.Sprintf("query OpsPilotLogs($deploymentId: String!, $limit: Int) { %s(deploymentId: $deploymentId, limit: $limit) { message timestamp severity } }", field),
		"variables": map[string]any{"deploymentId": metadata.DeploymentID, "limit": limit},
	})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, railwayGraphQLURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if credential.TokenKind == "project_token" {
		httpRequest.Header.Set("Project-Access-Token", credential.Token)
	} else {
		httpRequest.Header.Set("Authorization", "Bearer "+credential.Token)
	}
	return executeAndNormalizeProviderRequest(httpRequest, limit)
}

func executeAndNormalizeProviderRequest(request *http.Request, limit int) (string, error) {
	response, err := providerHTTPClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderLogResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxProviderLogResponseBytes {
		return "", fmt.Errorf("provider response exceeded safety limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	content := normalizeProviderLogs(body, limit)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("provider returned no log messages")
	}
	if len(content) > maxNormalizedLogBytes {
		content = content[:maxNormalizedLogBytes]
		for !utf8.ValidString(content) {
			content = content[:len(content)-1]
		}
	}
	return content, nil
}

func normalizeProviderLogs(body []byte, limit int) string {
	entries := make([]string, 0, limit)
	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		collectLogMessages(decoded, &entries, limit)
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 64*1024), 512*1024)
		for scanner.Scan() && len(entries) < limit {
			var item any
			if json.Unmarshal(scanner.Bytes(), &item) == nil {
				collectLogMessages(item, &entries, limit)
			}
		}
	}
	return strings.Join(entries, "\n")
}

func collectLogMessages(value any, entries *[]string, limit int) {
	if len(*entries) >= limit {
		return
	}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectLogMessages(item, entries, limit)
			if len(*entries) >= limit {
				return
			}
		}
	case map[string]any:
		message, _ := typed["message"].(string)
		if message == "" {
			message, _ = typed["text"].(string)
		}
		if message == "" {
			if payload, ok := typed["payload"].(map[string]any); ok {
				message, _ = payload["text"].(string)
			}
		}
		if message != "" {
			message = strings.ReplaceAll(strings.ReplaceAll(message, "\r", ""), "\x00", "")
			if len(message) > 8192 {
				message = message[:8192] + " [truncated]"
			}
			level, _ := typed["level"].(string)
			if level == "" {
				level, _ = typed["severity"].(string)
			}
			timestamp := formatProviderTimestamp(typed)
			prefix := strings.TrimSpace(timestamp + " " + strings.ToUpper(level))
			if prefix != "" {
				message = "[" + prefix + "] " + message
			}
			*entries = append(*entries, message)
			return
		}
		for _, child := range typed {
			collectLogMessages(child, entries, limit)
			if len(*entries) >= limit {
				return
			}
		}
	}
}

func formatProviderTimestamp(item map[string]any) string {
	for _, key := range []string{"timestamp", "timestampInMs", "created"} {
		switch value := item[key].(type) {
		case string:
			return value
		case float64:
			milliseconds := int64(value)
			if milliseconds > 0 && milliseconds < 10_000_000_000 {
				milliseconds *= 1000
			}
			if milliseconds > 0 {
				return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return ""
}
