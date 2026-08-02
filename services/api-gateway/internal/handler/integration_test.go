package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opspilot/api-gateway/internal/middleware"
	authv1 "github.com/opspilot/gen/proto/auth/v1"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubAuthClient resolves every request to a fixed user so handler tests reach
// their Integration Service call instead of stopping at the 401 guard.
type stubAuthClient struct {
	authv1.AuthServiceClient
}

func (stubAuthClient) ValidateToken(_ context.Context, _ *authv1.ValidateTokenRequest, _ ...grpc.CallOption) (*authv1.ValidateTokenResponse, error) {
	return &authv1.ValidateTokenResponse{Valid: true, UserId: "user-1"}, nil
}

// failingIntegrationClient returns the same error from every method under test.
// The embedded interface leaves unused methods nil, which is safe because no
// test calls them.
type failingIntegrationClient struct {
	integrationv1.IntegrationServiceClient
	err error
}

func (c failingIntegrationClient) CreateGitHubInstallSession(_ context.Context, _ *integrationv1.CreateGitHubInstallSessionRequest, _ ...grpc.CallOption) (*integrationv1.CreateGitHubInstallSessionResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) GetGitHubConfig(_ context.Context, _ *integrationv1.GetGitHubConfigRequest, _ ...grpc.CallOption) (*integrationv1.GetGitHubConfigResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) CompleteGitHubInstallSession(_ context.Context, _ *integrationv1.CompleteGitHubInstallSessionRequest, _ ...grpc.CallOption) (*integrationv1.CompleteGitHubInstallSessionResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) ListInstallations(_ context.Context, _ *integrationv1.ListInstallationsRequest, _ ...grpc.CallOption) (*integrationv1.ListInstallationsResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) ListRepositories(_ context.Context, _ *integrationv1.ListRepositoriesRequest, _ ...grpc.CallOption) (*integrationv1.ListRepositoriesResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) AttachRepository(_ context.Context, _ *integrationv1.AttachRepositoryRequest, _ ...grpc.CallOption) (*integrationv1.AttachRepositoryResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) GetRepositoryConnection(_ context.Context, _ *integrationv1.GetRepositoryConnectionRequest, _ ...grpc.CallOption) (*integrationv1.GetRepositoryConnectionResponse, error) {
	return nil, c.err
}

func (c failingIntegrationClient) ListRepositoryConnections(_ context.Context, _ *integrationv1.ListRepositoryConnectionsRequest, _ ...grpc.CallOption) (*integrationv1.ListRepositoryConnectionsResponse, error) {
	return nil, c.err
}

func newIntegrationHandler(err error) *IntegrationHandler {
	return NewIntegrationHandler(
		stubAuthClient{},
		failingIntegrationClient{err: err},
		"https://app.example.test",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func authedRequest(method, path, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	return request.WithContext(context.WithValue(request.Context(), middleware.TokenKey, "test-token"))
}

const repositoriesPath = "/api/v1/workspaces/ws-1/integrations/github/installations/42/repositories"

// ListConnectedRepositories serves two routes: workspace-scoped and, when the
// path carries no workspace segment, across every workspace the caller belongs
// to. Both must map identically.
const (
	connectedRepositoriesPath          = "/api/v1/repositories"
	workspaceConnectedRepositoriesPath = "/api/v1/workspaces/ws-1/repositories"
)

// TestListRepositoriesMapsGRPCCodesToHTTPStatuses is the core of this unit: an
// installation belonging to a different GitHub App must reach the browser as a
// 409 INSTALLATION_REVOKED, not as an indistinguishable 500.
func TestListRepositoriesMapsGRPCCodesToHTTPStatuses(t *testing.T) {
	for _, test := range []struct {
		name       string
		grpcCode   codes.Code
		wantStatus int
		wantCode   string
	}{
		{"failed precondition becomes conflict", codes.FailedPrecondition, http.StatusConflict, "INSTALLATION_REVOKED"},
		{"not found", codes.NotFound, http.StatusNotFound, "NOT_FOUND"},
		{"permission denied", codes.PermissionDenied, http.StatusForbidden, "FORBIDDEN"},
		{"unauthenticated", codes.Unauthenticated, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"invalid argument", codes.InvalidArgument, http.StatusBadRequest, "INVALID_REQUEST"},
		{"unavailable", codes.Unavailable, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE"},
		{"internal falls through to handler default", codes.Internal, http.StatusInternalServerError, "LIST_REPOS_FAILED"},
		{"unknown falls through to handler default", codes.Unknown, http.StatusInternalServerError, "LIST_REPOS_FAILED"},
		{"deadline exceeded falls through to handler default", codes.DeadlineExceeded, http.StatusInternalServerError, "LIST_REPOS_FAILED"},
		{"resource exhausted falls through to handler default", codes.ResourceExhausted, http.StatusInternalServerError, "LIST_REPOS_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newIntegrationHandler(status.Error(test.grpcCode, "failed to authorize with github: ghs_secret_token"))
			recorder := httptest.NewRecorder()
			handler.ListRepositories(recorder, authedRequest(http.MethodGet, repositoriesPath, ""))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `"error":"`+test.wantCode+`"`) {
				t.Fatalf("error code missing from body: %s", body)
			}
			if strings.Contains(body, "ghs_secret_token") || strings.Contains(body, "rpc error") {
				t.Fatalf("upstream error text leaked to client: %s", body)
			}
		})
	}
}

// TestListConnectedRepositoriesMapsGRPCCodesToHTTPStatuses covers the endpoint
// the dashboard calls to list connected repositories — the exact path in the
// original bug report. A repository connected through a stale GitHub App must
// surface as 409 INSTALLATION_REVOKED rather than an opaque 502, on both the
// workspace-scoped and cross-workspace routes.
func TestListConnectedRepositoriesMapsGRPCCodesToHTTPStatuses(t *testing.T) {
	for _, route := range []struct {
		name string
		path string
	}{
		{"cross workspace route", connectedRepositoriesPath},
		{"workspace scoped route", workspaceConnectedRepositoriesPath},
	} {
		for _, test := range []struct {
			name       string
			grpcCode   codes.Code
			wantStatus int
			wantCode   string
		}{
			{"failed precondition becomes conflict", codes.FailedPrecondition, http.StatusConflict, "INSTALLATION_REVOKED"},
			{"not found", codes.NotFound, http.StatusNotFound, "NOT_FOUND"},
			{"permission denied", codes.PermissionDenied, http.StatusForbidden, "FORBIDDEN"},
			{"unauthenticated", codes.Unauthenticated, http.StatusUnauthorized, "UNAUTHORIZED"},
			{"invalid argument", codes.InvalidArgument, http.StatusBadRequest, "INVALID_REQUEST"},
			{"unavailable", codes.Unavailable, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE"},
			{"internal falls through to handler default", codes.Internal, http.StatusBadGateway, "REPOSITORY_LIST_FAILED"},
			{"unknown falls through to handler default", codes.Unknown, http.StatusBadGateway, "REPOSITORY_LIST_FAILED"},
			{"deadline exceeded falls through to handler default", codes.DeadlineExceeded, http.StatusBadGateway, "REPOSITORY_LIST_FAILED"},
		} {
			t.Run(route.name+"/"+test.name, func(t *testing.T) {
				handler := newIntegrationHandler(status.Error(test.grpcCode, "reconnect required: ghs_secret_token"))
				recorder := httptest.NewRecorder()
				handler.ListConnectedRepositories(recorder, authedRequest(http.MethodGet, route.path, ""))

				if recorder.Code != test.wantStatus {
					t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body.String())
				}
				body := recorder.Body.String()
				if !strings.Contains(body, `"error":"`+test.wantCode+`"`) {
					t.Fatalf("error code missing from body: %s", body)
				}
				if strings.Contains(body, "ghs_secret_token") || strings.Contains(body, "rpc error") {
					t.Fatalf("upstream error text leaked to client: %s", body)
				}
			})
		}
	}
}

// TestIntegrationHandlersPreserveTheirOwnDefaults guards the fall-through: an
// unmapped code must produce exactly the status and error code each handler
// returned before the mapping existed.
func TestIntegrationHandlersPreserveTheirOwnDefaults(t *testing.T) {
	for _, test := range []struct {
		name       string
		invoke     func(*IntegrationHandler, http.ResponseWriter)
		wantStatus int
		wantCode   string
	}{
		{
			name: "create install session",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.CreateGitHubInstallSession(w, authedRequest(http.MethodPost, "/api/v1/workspaces/ws-1/integrations/github/install-session", ""))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "GITHUB_INSTALL_SESSION_FAILED",
		},
		{
			name: "github config",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.GitHubConfig(w, authedRequest(http.MethodGet, "/api/v1/integrations/github/config", ""))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "GITHUB_CONFIG_FAILED",
		},
		{
			name: "github callback",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.GitHubCallback(w, authedRequest(http.MethodGet, "/api/v1/integrations/github/callback?installation_id=42&state=opaque", ""))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "CALLBACK_FAILED",
		},
		{
			name: "list installations",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.ListInstallations(w, authedRequest(http.MethodGet, "/api/v1/workspaces/ws-1/integrations/github/installations", ""))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "LIST_FAILED",
		},
		{
			name: "list repositories",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.ListRepositories(w, authedRequest(http.MethodGet, repositoriesPath, ""))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "LIST_REPOS_FAILED",
		},
		{
			name: "attach repository",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.AttachRepository(w, authedRequest(http.MethodPost, "/api/v1/workspaces/ws-1/projects/proj-1/repository", `{"owner":"acme","name":"api"}`))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "ATTACH_FAILED",
		},
		{
			name: "get repository",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.GetRepository(w, authedRequest(http.MethodGet, "/api/v1/workspaces/ws-1/projects/proj-1/repository", ""))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "FETCH_FAILED",
		},
		{
			name: "list connected repositories",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.ListConnectedRepositories(w, authedRequest(http.MethodGet, connectedRepositoriesPath, ""))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "REPOSITORY_LIST_FAILED",
		},
		{
			name: "list connected repositories for one workspace",
			invoke: func(h *IntegrationHandler, w http.ResponseWriter) {
				h.ListConnectedRepositories(w, authedRequest(http.MethodGet, workspaceConnectedRepositoriesPath, ""))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "REPOSITORY_LIST_FAILED",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newIntegrationHandler(status.Error(codes.Internal, "database is down: dsn=postgres://user:pw@db"))
			recorder := httptest.NewRecorder()
			test.invoke(handler, recorder)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `"error":"`+test.wantCode+`"`) {
				t.Fatalf("error code missing from body: %s", body)
			}
			if strings.Contains(body, "postgres://") || strings.Contains(body, "rpc error") {
				t.Fatalf("upstream error text leaked to client: %s", body)
			}
		})
	}
}

// TestIntegrationHandlersMapFailedPreconditionToConflict proves every migrated
// handler — not just ListRepositories — reports a revoked installation as 409.
func TestIntegrationHandlersMapFailedPreconditionToConflict(t *testing.T) {
	for _, test := range []struct {
		name   string
		invoke func(*IntegrationHandler, http.ResponseWriter)
	}{
		{"create install session", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.CreateGitHubInstallSession(w, authedRequest(http.MethodPost, "/api/v1/workspaces/ws-1/integrations/github/install-session", ""))
		}},
		{"github config", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.GitHubConfig(w, authedRequest(http.MethodGet, "/api/v1/integrations/github/config", ""))
		}},
		{"github callback", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.GitHubCallback(w, authedRequest(http.MethodGet, "/api/v1/integrations/github/callback?installation_id=42&state=opaque", ""))
		}},
		{"list installations", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.ListInstallations(w, authedRequest(http.MethodGet, "/api/v1/workspaces/ws-1/integrations/github/installations", ""))
		}},
		{"list repositories", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.ListRepositories(w, authedRequest(http.MethodGet, repositoriesPath, ""))
		}},
		{"attach repository", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.AttachRepository(w, authedRequest(http.MethodPost, "/api/v1/workspaces/ws-1/projects/proj-1/repository", `{"owner":"acme","name":"api"}`))
		}},
		{"get repository", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.GetRepository(w, authedRequest(http.MethodGet, "/api/v1/workspaces/ws-1/projects/proj-1/repository", ""))
		}},
		{"list connected repositories", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.ListConnectedRepositories(w, authedRequest(http.MethodGet, connectedRepositoriesPath, ""))
		}},
		{"list connected repositories for one workspace", func(h *IntegrationHandler, w http.ResponseWriter) {
			h.ListConnectedRepositories(w, authedRequest(http.MethodGet, workspaceConnectedRepositoriesPath, ""))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newIntegrationHandler(status.Error(codes.FailedPrecondition, "installation belongs to a different GitHub App"))
			recorder := httptest.NewRecorder()
			test.invoke(handler, recorder)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusConflict, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"error":"INSTALLATION_REVOKED"`) {
				t.Fatalf("error code missing from body: %s", recorder.Body.String())
			}
		})
	}
}

func TestResolveGRPCErrorFallsThroughForNonStatusErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{"nil error", nil},
		{"plain error", errors.New("connection reset by peer")},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotStatus, gotCode, gotMessage := resolveGRPCError(test.err, http.StatusInternalServerError, "LIST_REPOS_FAILED", "failed to list repositories")
			if gotStatus != http.StatusInternalServerError || gotCode != "LIST_REPOS_FAILED" || gotMessage != "failed to list repositories" {
				t.Fatalf("resolveGRPCError = (%d, %q, %q), want the caller defaults", gotStatus, gotCode, gotMessage)
			}
		})
	}
}

func TestResolveGRPCErrorUnwrapsWrappedStatusErrors(t *testing.T) {
	wrapped := fmt.Errorf("calling integration service: %w", status.Error(codes.Unavailable, "connection refused"))
	gotStatus, gotCode, gotMessage := resolveGRPCError(wrapped, http.StatusInternalServerError, "LIST_REPOS_FAILED", "failed to list repositories")
	if gotStatus != http.StatusServiceUnavailable || gotCode != "UPSTREAM_UNAVAILABLE" {
		t.Fatalf("resolveGRPCError = (%d, %q), want (503, UPSTREAM_UNAVAILABLE)", gotStatus, gotCode)
	}
	if strings.Contains(gotMessage, "connection refused") {
		t.Fatalf("upstream error text leaked into message: %q", gotMessage)
	}
}

// TestGRPCErrorMessagesAreFixed keeps the mapped messages free of upstream text
// by construction: every entry must carry a non-empty message of our own.
func TestGRPCErrorMessagesAreFixed(t *testing.T) {
	for code, response := range grpcErrorResponses {
		if response.message == "" || response.errorCode == "" || response.httpStatus < 400 {
			t.Fatalf("mapping for %s is incomplete: %+v", code, response)
		}
	}
}
