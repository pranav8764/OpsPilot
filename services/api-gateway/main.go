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
	integrationHandler := handler.NewIntegrationHandler(authClient, integrationClient, cfg.FrontendURL, logger)
	aiHandler := handler.NewAIHandler(authClient, projectClient, workspaceClient, integrationClient, cfg.AIWorkerURL, cfg.AIWorkerInternalToken, logger)

	// ── Router ───────────────────────────────────────────────────
	mux := http.NewServeMux()
	httpMetrics := middleware.NewHTTPMetrics()

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
	mux.HandleFunc("/metrics", httpMetrics.Handler)

	// Auth middleware for protected routes
	authMiddleware := middleware.RequireAuth(cfg.ClerkSecretKey, logger)

	// User routes
	mux.Handle("/api/v1/users/sync", authMiddleware(http.HandlerFunc(userHandler.Sync)))
	mux.Handle("/api/v1/users/me", authMiddleware(http.HandlerFunc(userHandler.Me)))

	// Evaluation routes
	mux.Handle("/api/v1/eval/report", authMiddleware(http.HandlerFunc(aiHandler.GetEvalReport)))

	// Integration routes
	mux.Handle("/api/v1/integrations/github/config", authMiddleware(http.HandlerFunc(integrationHandler.GitHubConfig)))
	mux.Handle("/api/v1/integrations/github/callback", http.HandlerFunc(integrationHandler.GitHubCallback))
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
		if isWorkspaceSettingsPath(r.URL.Path) {
			if r.Method == http.MethodPut {
				workspaceHandler.UpdateSettings(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if isProjectPath(r.URL.Path) {
			if r.Method == http.MethodDelete {
				projectHandler.Delete(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if isWorkspaceUsagePath(r.URL.Path) {
			aiHandler.UsageSummary(w, r)
			return
		}
		if isLogProviderConnectionsPath(r.URL.Path) {
			switch r.Method {
			case http.MethodGet:
				integrationHandler.ListLogProviders(w, r)
			case http.MethodPost:
				integrationHandler.UpsertLogProvider(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if isGitHubInstallSessionPath(r.URL.Path) {
			integrationHandler.CreateGitHubInstallSession(w, r)
			return
		}
		// Explicit approval/rejection for a pinned draft FIX PR action.
		if isFixApprovalDecisionPath(r.URL.Path) {
			integrationHandler.DecideFixApproval(w, r)
			return
		}
		// Pinned code diff proposal. Publishing remains separately approval-gated.
		if isIncidentFixProposalPath(r.URL.Path) {
			aiHandler.ProposeFix(w, r)
			return
		}
		// Evidence-backed manual/provider log analysis.
		if isProjectLogAnalysisPath(r.URL.Path) {
			aiHandler.AnalyzeLogs(w, r)
			return
		}
		if isProjectLogCollectionPath(r.URL.Path) {
			aiHandler.CollectAndAnalyzeLogs(w, r)
			return
		}
		if isProjectIncidentsPath(r.URL.Path) {
			aiHandler.ListIncidents(w, r)
			return
		}
		// Tenant-scoped project Q&A.
		if isProjectQueryPath(r.URL.Path) {
			aiHandler.Query(w, r)
			return
		}
		if isProjectDeploymentReadinessPath(r.URL.Path) {
			aiHandler.DeploymentAnalysis(w, r, "readiness")
			return
		}
		if isProjectDeploymentGeneratePath(r.URL.Path) {
			aiHandler.DeploymentAnalysis(w, r, "generate")
			return
		}
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
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
	})

	// ── Apply global middleware ──────────────────────────────────
	handler := middleware.RequestID(
		httpMetrics.Middleware(middleware.Logging(logger)(
			middleware.SecurityHeaders(cfg.Env == "production")(
				middleware.RateLimit(cfg.RateLimitRequests, time.Minute, logger)(corsHandler.Handler(mux)),
			),
		)),
	)

	// ── HTTP Server ──────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.GatewayPort),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 90 * time.Second,
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
	parts := splitPath(path)
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects"
}

func isProjectPath(path string) bool {
	parts := splitPath(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects"
}

func isWorkspaceUsagePath(path string) bool {
	parts := splitPath(path)
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "usage"
}

func isWorkspaceSettingsPath(path string) bool {
	parts := splitPath(path)
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "settings"
}

func isGitHubInstallationsPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/integrations/github/installations
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "integrations" && parts[5] == "github" && parts[6] == "installations"
}

func isGitHubInstallSessionPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/integrations/github/install-session
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "integrations" && parts[5] == "github" && parts[6] == "install-session"
}

func isLogProviderConnectionsPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/integrations/log-providers
	parts := splitPath(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "integrations" && parts[5] == "log-providers"
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

func isProjectQueryPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/query
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "query"
}

func isProjectDeploymentReadinessPath(path string) bool {
	parts := splitPath(path)
	return len(parts) == 8 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "deployment" && parts[7] == "readiness"
}

func isProjectDeploymentGeneratePath(path string) bool {
	parts := splitPath(path)
	return len(parts) == 8 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "deployment" && parts[7] == "generate"
}

func isProjectLogAnalysisPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/analyze
	parts := splitPath(path)
	return len(parts) == 8 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "incidents" && parts[7] == "analyze"
}

func isProjectLogCollectionPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/collect
	parts := splitPath(path)
	return len(parts) == 8 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "incidents" && parts[7] == "collect"
}

func isProjectIncidentsPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents
	parts := splitPath(path)
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "incidents"
}

func isIncidentFixProposalPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/{incidentId}/fix-proposal
	parts := splitPath(path)
	return len(parts) == 9 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "incidents" && parts[8] == "fix-proposal"
}

func isFixApprovalDecisionPath(path string) bool {
	// matches /api/v1/workspaces/{workspaceId}/projects/{projectId}/approvals/{approvalId}/decision
	parts := splitPath(path)
	return len(parts) == 9 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "workspaces" && parts[4] == "projects" && parts[6] == "approvals" && parts[8] == "decision"
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
