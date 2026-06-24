package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	clerkSDK "github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const (
	UserIDKey      contextKey = "user_id"
	ClerkUserIDKey contextKey = "clerk_user_id"
	EmailKey       contextKey = "email"
	NameKey        contextKey = "name"
	TokenKey       contextKey = "token"
)

// UserContext holds the authenticated user's information.
type UserContext struct {
	UserID      string
	ClerkUserID string
	Email       string
	Name        string
	Token       string
}

// GetUserContext retrieves the user context from the request context.
func GetUserContext(ctx context.Context) *UserContext {
	v := ctx.Value(UserIDKey)
	if v == nil {
		return nil
	}
	return &UserContext{
		UserID:      ctx.Value(UserIDKey).(string),
		ClerkUserID: ctx.Value(ClerkUserIDKey).(string),
		Email:       ctx.Value(EmailKey).(string),
		Name:        ctx.Value(NameKey).(string),
		Token:       ctx.Value(TokenKey).(string),
	}
}

// RequireAuth is a middleware that validates Clerk JWT tokens.
// On success it injects user info into the request context.
func RequireAuth(clerkSecretKey string, logger *slog.Logger) func(http.Handler) http.Handler {
	clerkSDK.SetKey(clerkSecretKey)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid Authorization header")
				return
			}

			claims, err := jwt.Verify(r.Context(), &jwt.VerifyParams{Token: token})
			if err != nil {
				logger.Warn("token validation failed", "error", err, "path", r.URL.Path)
				writeError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token is invalid or expired")
				return
			}

			// Inject user context
			ctx := context.WithValue(r.Context(), ClerkUserIDKey, claims.Subject)
			ctx = context.WithValue(ctx, TokenKey, token)

			// Forward minimal user info from token claims
			// Full user data is fetched from auth-service when needed
			ctx = context.WithValue(ctx, UserIDKey, "")    // populated after SyncUser
			ctx = context.WithValue(ctx, EmailKey, "")
			ctx = context.WithValue(ctx, NameKey, "")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Logging middleware logs each HTTP request.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.statusCode,
				"duration_ms", time.Since(start).Milliseconds(),
				"user_agent", r.UserAgent(),
			)
		})
	}
}

// RequestID adds a unique request ID to each request (simple version).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = generateID()
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r)
	})
}

// ── Helpers ────────────────────────────────────────────────────

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   errCode,
		"message": message,
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
