package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
)

// UserHandler handles user-related HTTP endpoints.
type UserHandler struct {
	authClient authv1.AuthServiceClient
	logger     *slog.Logger
}

func NewUserHandler(authClient authv1.AuthServiceClient, logger *slog.Logger) *UserHandler {
	return &UserHandler{authClient: authClient, logger: logger}
}

// Me returns the authenticated user's info (syncing them if first visit).
// POST /api/v1/users/sync
func (h *UserHandler) Sync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clerkUserID, ok := r.Context().Value(middleware.ClerkUserIDKey).(string)
	if !ok || clerkUserID == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var body struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	resp, err := h.authClient.SyncUser(r.Context(), &authv1.SyncUserRequest{
		ClerkUserId: clerkUserID,
		Email:       body.Email,
		Name:        body.Name,
		AvatarUrl:   body.AvatarURL,
	})
	if err != nil {
		h.logger.Error("sync user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "SYNC_FAILED", "failed to sync user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": resp.UserId,
		"created": resp.Created,
	})
}

// Me returns the authenticated user's profile.
// GET /api/v1/users/me
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, ok := r.Context().Value(middleware.TokenKey).(string)
	if !ok || token == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	resp, err := h.authClient.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":       resp.UserId,
		"clerk_user_id": resp.ClerkUserId,
		"email":         resp.Email,
		"name":          resp.Name,
	})
}

// ── Helpers ────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	writeJSON(w, code, map[string]string{
		"error":   errCode,
		"message": message,
	})
}
