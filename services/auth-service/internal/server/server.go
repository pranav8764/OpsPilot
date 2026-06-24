package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	clerkSDK "github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/opspilot/gen/proto/auth/v1"
)

// AuthServer implements the AuthService gRPC server.
type AuthServer struct {
	authv1.UnimplementedAuthServiceServer
	db     *pgxpool.Pool
	logger *slog.Logger
}

// New creates a new AuthServer. Clerk SDK must be pre-initialised (SetKey called in main).
func New(db *pgxpool.Pool, logger *slog.Logger) *AuthServer {
	return &AuthServer{db: db, logger: logger}
}

// ValidateToken verifies a Clerk JWT and returns the resolved internal user.
func (s *AuthServer) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	claims, err := jwt.Verify(ctx, &jwt.VerifyParams{Token: req.Token})
	if err != nil {
		s.logger.Warn("invalid token", "error", err)
		return &authv1.ValidateTokenResponse{Valid: false}, nil
	}

	clerkUserID := claims.Subject

	var (
		userID string
		email  string
		name   string
	)
	err = s.db.QueryRow(ctx,
		`SELECT id, email, COALESCE(name,'') FROM users WHERE clerk_user_id = $1`,
		clerkUserID,
	).Scan(&userID, &email, &name)

	if err == pgx.ErrNoRows {
		// Not synced yet — valid token but no internal user record
		return &authv1.ValidateTokenResponse{
			Valid:       true,
			ClerkUserId: clerkUserID,
		}, nil
	}
	if err != nil {
		s.logger.Error("db query error", "error", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &authv1.ValidateTokenResponse{
		Valid:       true,
		UserId:      userID,
		ClerkUserId: clerkUserID,
		Email:       email,
		Name:        name,
	}, nil
}

// SyncUser upserts a Clerk user into our PostgreSQL users table.
func (s *AuthServer) SyncUser(ctx context.Context, req *authv1.SyncUserRequest) (*authv1.SyncUserResponse, error) {
	if req.ClerkUserId == "" || req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "clerk_user_id and email are required")
	}

	// Suppress unused import warning for clerkSDK (used implicitly via global SetKey)
	_ = clerkSDK.String("")

	var (
		userID  string
		created bool
	)

	err := s.db.QueryRow(ctx, `
		INSERT INTO users (clerk_user_id, email, name, avatar_url, auth_provider)
		VALUES ($1, $2, $3, $4, 'clerk')
		ON CONFLICT (clerk_user_id) DO UPDATE
			SET email      = EXCLUDED.email,
			    name       = EXCLUDED.name,
			    avatar_url = EXCLUDED.avatar_url,
			    updated_at = NOW()
		RETURNING id, (xmax = 0) AS inserted
	`, req.ClerkUserId, req.Email, req.Name, req.AvatarUrl,
	).Scan(&userID, &created)

	if err != nil {
		s.logger.Error("upsert user failed", "error", err, "clerk_user_id", req.ClerkUserId)
		return nil, status.Error(codes.Internal, "failed to sync user")
	}

	s.logger.Info("user synced", "user_id", userID, "clerk_user_id", req.ClerkUserId, "new", created)
	return &authv1.SyncUserResponse{UserId: userID, Created: created}, nil
}

// GetUser fetches a user by internal UUID.
func (s *AuthServer) GetUser(ctx context.Context, req *authv1.GetUserRequest) (*authv1.GetUserResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	var u authv1.User
	var createdAt time.Time

	err := s.db.QueryRow(ctx, `
		SELECT id, clerk_user_id, email,
		       COALESCE(name,''), COALESCE(avatar_url,''), created_at
		FROM users WHERE id = $1
	`, req.UserId).Scan(
		&u.UserId, &u.ClerkUserId, &u.Email, &u.Name, &u.AvatarUrl, &createdAt,
	)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("user %s not found", req.UserId))
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	u.CreatedAt = createdAt.Format(time.RFC3339)
	return &authv1.GetUserResponse{User: &u}, nil
}
