package server

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"github.com/opspilot/integration-service/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IntegrationServer struct {
	integrationv1.UnimplementedIntegrationServiceServer
	db     *pgxpool.Pool
	cfg    *config.Config
	logger *slog.Logger
}

func New(db *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *IntegrationServer {
	return &IntegrationServer{db: db, cfg: cfg, logger: logger}
}

func (s *IntegrationServer) GetGitHubConfig(ctx context.Context, req *integrationv1.GetGitHubConfigRequest) (*integrationv1.GetGitHubConfigResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	return &integrationv1.GetGitHubConfigResponse{
		InstallUrl:  s.cfg.GitHubAppInstallURL,
		CallbackUrl: s.cfg.GitHubAppCallbackURL,
		WebhookUrl:  s.cfg.GitHubWebhookURL,
		Configured:  s.cfg.GitHubConfigured(),
	}, nil
}
