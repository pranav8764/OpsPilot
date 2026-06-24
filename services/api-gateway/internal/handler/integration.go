package handler

import (
	"fmt"
	"log/slog"
	"net/http"

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
