package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/cors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/opspilot/api-gateway/internal/config"
	"github.com/opspilot/api-gateway/internal/handler"
	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	projectv1 "github.com/opspilot/gen/proto/project/v1"
	workspacev1 "github.com/opspilot/gen/proto/workspace/v1"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// ── Connect to downstream gRPC services ─────────────────────
	authConn, err := dialGRPC(cfg.AuthServiceAddr)
	if err != nil {
		logger.Error("failed to connect to auth service", "error", err, "addr", cfg.AuthServiceAddr)
		os.Exit(1)
	}
	defer authConn.Close()

	workspaceConn, err := dialGRPC(cfg.WorkspaceServiceAddr)
	if err != nil {
		logger.Error("failed to connect to workspace service", "error", err)
		os.Exit(1)
	}
	defer workspaceConn.Close()

	projectConn, err := dialGRPC(cfg.ProjectServiceAddr)
	if err != nil {
		logger.Error("failed to connect to project service", "error", err)
		os.Exit(1)
	}
	defer projectConn.Close()

	integrationConn, err := dialGRPC(cfg.IntegrationServiceAddr)
	if err != nil {
		logger.Error("failed to connect to integration service", "error", err)
		os.Exit(1)
	}
	defer integrationConn.Close()

	// ── gRPC clients ─────────────────────────────────────────────
	authClient := authv1.NewAuthServiceClient(authConn)
	workspaceClient := workspacev1.NewWorkspaceServiceClient(workspaceConn)
	projectClient := projectv1.NewProjectServiceClient(projectConn)
	integrationClient := integrationv1.NewIntegrationServiceClient(integrationConn)

	// ── Handlers ─────────────────────────────────────────────────
	userHandler := handler.NewUserHandler(authClient, logger)
	workspaceHandler := handler.NewWorkspaceHandler(authClient, workspaceClient, logger)
	projectHandler := handler.NewProjectHandler(authClient, projectClient, logger)
	integrationHandler := handler.NewIntegrationHandler(authClient, integrationClient, logger)

	// ── Router ───────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Health check (public)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": "api-gateway",
			"version": "1.0.0",
		})
	})

	// Auth middleware for protected routes
	authMiddleware := middleware.RequireAuth(cfg.ClerkSecretKey, logger)

	// User routes
	mux.Handle("/api/v1/users/sync", authMiddleware(http.HandlerFunc(userHandler.Sync)))
	mux.Handle("/api/v1/users/me", authMiddleware(http.HandlerFunc(userHandler.Me)))

	// Integration routes
	mux.Handle("/api/v1/integrations/github/config", authMiddleware(http.HandlerFunc(integrationHandler.GitHubConfig)))
	mux.Handle("/api/v1/integrations/github/callback", authMiddleware(http.HandlerFunc(integrationHandler.GitHubCallback)))
	mux.Handle("/api/v1/webhooks/github", http.HandlerFunc(integrationHandler.GitHubWebhook))

	// Workspace routes
	mux.Handle("/api/v1/workspaces", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			workspaceHandler.List(w, r)
		case http.MethodPost:
			workspaceHandler.Create(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	mux.Handle("/api/v1/workspaces/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GitHub installation list
		if isGitHubInstallationsPath(r.URL.Path) {
			integrationHandler.ListInstallations(w, r)
			return
		}
		// GitHub repository list
		if isGitHubRepositoriesPath(r.URL.Path) {
			integrationHandler.ListRepositories(w, r)
			return
		}
		// Project repository connection
		if isProjectRepositoryPath(r.URL.Path) {
			switch r.Method {
			case http.MethodGet:
				integrationHandler.GetRepository(w, r)
			case http.MethodPost:
				integrationHandler.AttachRepository(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		// /api/v1/workspaces/{id}/projects
		if isProjectsPath(r.URL.Path) {
			switch r.Method {
			case http.MethodGet:
				projectHandler.List(w, r)
			case http.MethodPost:
				projectHandler.Create(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		// /api/v1/workspaces/{id}
		if r.Method == http.MethodGet {
			workspaceHandler.Get(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})))


	// ── CORS ──────────────────────────────────────────────────────
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "https://*.vercel.app"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
	})

	// ── Apply global middleware ──────────────────────────────────
	handler := middleware.RequestID(
		middleware.Logging(logger)(
			corsHandler.Handler(mux),
		),
	)

	// ── HTTP Server ──────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.GatewayPort),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("API Gateway started ✅",
		"port", cfg.GatewayPort,
		"env", cfg.Env,
		"auth_service", cfg.AuthServiceAddr,
		"workspace_service", cfg.WorkspaceServiceAddr,
		"project_service", cfg.ProjectServiceAddr,
		"integration_service", cfg.IntegrationServiceAddr,
	)

	// ── Graceful shutdown ────────────────────────────────────────
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		logger.Info("shutting down api gateway...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func dialGRPC(addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

func isProjectsPath(path string) bool {
	// Check if path ends in /projects or /projects/
	parts := splitPath(path)
	return len(parts) > 0 && (parts[len(parts)-1] == "projects" || (len(parts) > 1 && parts[len(parts)-2] == "projects"))
}

func isGitHubInstallationsPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/integrations/github/installations
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "integrations" && parts[5] == "github" && parts[6] == "installations"
}

func isGitHubRepositoriesPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/integrations/github/installations/{installationId}/repositories
	parts := splitPath(path)
	return len(parts) == 9 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "integrations" && parts[5] == "github" && parts[6] == "installations" && parts[8] == "repositories"
}

func isProjectRepositoryPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/repository
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "repository"
}


func splitPath(path string) []string {
	var parts []string
	for _, p := range splitSlash(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitSlash(s string) []string {
	result := []string{}
	start := 0
	for i, c := range s {
		if c == '/' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
