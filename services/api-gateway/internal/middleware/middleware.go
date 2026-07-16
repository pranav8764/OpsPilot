package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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
	RequestIDKey   contextKey = "request_id"
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
			ctx = context.WithValue(ctx, UserIDKey, "") // populated after SyncUser
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
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// RequestID adds a bounded correlation ID. Untrusted values are rejected so
// they cannot create log-injection or oversized-header problems.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if !validRequestID(reqID) {
			reqID = generateID()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(RequestIDKey).(string)
	return value
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

// RateLimit bounds abusive traffic per authenticated token fingerprint (or
// remote address for public routes). It intentionally never stores bearer
// tokens. A distributed limiter can replace this middleware at multi-region scale.
func RateLimit(maxRequests int, window time.Duration, logger *slog.Logger) func(http.Handler) http.Handler {
	var mutex sync.Mutex
	buckets := make(map[string]rateBucket)
	lastCleanup := time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			if token := extractBearerToken(r); token != "" {
				digest := sha256.Sum256([]byte(token))
				key = "token:" + hex.EncodeToString(digest[:16])
			}
			now := time.Now()
			mutex.Lock()
			bucket := buckets[key]
			if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= window {
				bucket = rateBucket{windowStart: now}
			}
			bucket.count++
			buckets[key] = bucket
			if now.Sub(lastCleanup) >= 5*window {
				for candidate, value := range buckets {
					if now.Sub(value.windowStart) >= window {
						delete(buckets, candidate)
					}
				}
				lastCleanup = now
			}
			remaining := max(0, maxRequests-bucket.count)
			limited := bucket.count > maxRequests
			reset := int(window.Seconds() - now.Sub(bucket.windowStart).Seconds())
			mutex.Unlock()

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", max(0, reset)))
			if limited {
				logger.Warn("request rate limit exceeded", "path", r.URL.Path)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, reset)))
				writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests; retry after the current window")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(production bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			if production {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
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
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}
