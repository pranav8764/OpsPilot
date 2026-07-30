package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	clerkSDK "github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	clerkUser "github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	// The caller-supplied email arrives in an HTTP request body and is therefore
	// untrusted — only clerk_user_id comes from the verified session token. Ask
	// Clerk itself for the address so the value we persist (and, below, the value
	// we may re-key an existing account on) is one Clerk has actually verified.
	email := req.Email
	emailVerified := false
	if verified, err := s.verifiedClerkEmail(ctx, req.ClerkUserId); err != nil {
		// Degrade to the untrusted value: a new sign-up still works, but the
		// re-bind path below stays closed because emailVerified remains false.
		s.logger.Warn("clerk email lookup failed; falling back to request body",
			"error", err, "clerk_user_id", req.ClerkUserId)
	} else if verified != "" {
		email = verified
		emailVerified = true
	}

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
	`, req.ClerkUserId, email, req.Name, req.AvatarUrl,
	).Scan(&userID, &created)

	// Postgres permits only one ON CONFLICT target, so the upsert above cannot
	// also resolve users_email_key. That fires when the same person signs in
	// under a second Clerk identity (new provider, recreated account): the email
	// row already exists under a different clerk_user_id, the insert 23505s, and
	// without a users row ValidateToken returns an empty UserId — which the
	// gateway turns into a 401 on every request.
	if err != nil && s.isEmailConflict(err) {
		if !emailVerified {
			// Never re-key an account on an address Clerk has not confirmed:
			// that would let any signed-in caller claim another user's row.
			s.logger.Warn("email conflict with unverified address; refusing to re-bind",
				"clerk_user_id", req.ClerkUserId)
			return nil, status.Error(codes.AlreadyExists,
				"that email is already linked to another sign-in method")
		}
		err = s.db.QueryRow(ctx, `
			UPDATE users
			   SET clerk_user_id = $1,
			       name          = $3,
			       avatar_url    = $4,
			       updated_at    = NOW()
			 WHERE email = $2
			RETURNING id
		`, req.ClerkUserId, email, req.Name, req.AvatarUrl).Scan(&userID)
		created = false
		if err == nil {
			s.logger.Info("re-bound existing user to new clerk identity",
				"user_id", userID, "clerk_user_id", req.ClerkUserId)
		}
	}

	if err != nil {
		s.logger.Error("upsert user failed", "error", err, "clerk_user_id", req.ClerkUserId)
		return nil, status.Error(codes.Internal, "failed to sync user")
	}

	s.logger.Info("user synced", "user_id", userID, "clerk_user_id", req.ClerkUserId, "new", created)
	return &authv1.SyncUserResponse{UserId: userID, Created: created}, nil
}

// isEmailConflict reports whether err is the users_email_key unique violation.
func (s *AuthServer) isEmailConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key"
}

// verifiedClerkEmail returns the Clerk user's primary email address, but only
// when Clerk reports it as verified. An empty string means "Clerk knows this
// user but has not confirmed a primary address" — treated as unverified.
func (s *AuthServer) verifiedClerkEmail(ctx context.Context, clerkUserID string) (string, error) {
	u, err := clerkUser.Get(ctx, clerkUserID)
	if err != nil {
		return "", err
	}
	if u.PrimaryEmailAddressID == nil {
		return "", nil
	}
	for _, addr := range u.EmailAddresses {
		if addr == nil || addr.ID != *u.PrimaryEmailAddressID {
			continue
		}
		if addr.Verification != nil && addr.Verification.Status == "verified" {
			return addr.EmailAddress, nil
		}
		return "", nil
	}
	return "", nil
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
