export const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

/**
 * A non-2xx response from the gateway. It still extends Error, so every caller
 * that only reads `.message` keeps working unchanged; callers that need to
 * branch on the failure can read `status` and the machine-readable `code`.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(message: string, status: number, code = '') {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

/**
 * The gateway reports this when the workspace's stored installation belongs to
 * a GitHub App that can no longer mint tokens, so it must be reconnected.
 */
export const INSTALLATION_REVOKED_CODE = 'INSTALLATION_REVOKED';

export const INSTALLATION_REVOKED_MESSAGE =
  'This workspace is linked to a different GitHub App installation, so Archiatros can no longer read its repositories. Reconnect the GitHub App to restore access.';

export function isInstallationRevokedError(error: unknown): boolean {
  return isApiError(error) && error.code === INSTALLATION_REVOKED_CODE;
}

/**
 * Turns any request failure into text a user can act on. Every non-2xx keeps
 * its HTTP status and gateway error code, so a 500 can never be mistaken for
 * "this installation has no repositories".
 */
export function describeRequestError(error: unknown, fallback = 'The request failed.'): string {
  if (isInstallationRevokedError(error)) return INSTALLATION_REVOKED_MESSAGE;
  if (isApiError(error)) {
    const detail = error.code ? `HTTP ${error.status} ${error.code}` : `HTTP ${error.status}`;
    return `${error.message} (${detail})`;
  }
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

/**
 * Resolves to a discriminated result instead of rejecting. An empty list and a
 * failed call lead to opposite screens, so callers that fan out with
 * Promise.all must be able to tell them apart.
 */
export async function settle<T>(promise: Promise<T>): Promise<{ ok: true; value: T } | { ok: false; error: unknown }> {
  try {
    return { ok: true, value: await promise };
  } catch (error) {
    return { ok: false, error };
  }
}

interface ErrorEnvelope {
  message?: string;
  // The gateway's writeError() puts the machine-readable code under `error`;
  // `code` is read first so a renamed envelope keeps working.
  code?: string;
  error?: string;
}

async function fetchAPI(endpoint: string, token: string, options: RequestInit = {}) {
  const headers: HeadersInit = {
    'Authorization': `Bearer ${token}`,
    'Accept': 'application/json',
    'Content-Type': 'application/json',
    ...options.headers,
  };

  const res = await fetch(`${API_URL}${endpoint}`, {
    ...options,
    headers,
  });

  if (!res.ok) {
    // A body of literal `null` parses to null, so `?? {}` is load-bearing:
    // without it the property reads below would throw a TypeError and no
    // caller would ever see an ApiError.
    const errorBody: ErrorEnvelope = (await res.json().catch(() => null)) ?? {};
    const rawCode = errorBody.code ?? errorBody.error;
    throw new ApiError(
      errorBody.message || `API Error: ${res.status}`,
      res.status,
      typeof rawCode === 'string' ? rawCode : ''
    );
  }

  if (res.status === 204) return null;
  return res.json();
}

// ── Users ─────────────────────────────────────────────────────────

export async function syncUser(token: string, data: { email: string; name: string; avatar_url: string }) {
  return fetchAPI('/api/v1/users/sync', token, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// ── Workspaces ────────────────────────────────────────────────────

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  owner_id: string;
  plan: string;
  role: string;
  created_at: string;
  log_retention_days: number;
}

export async function getWorkspaces(token: string): Promise<{ workspaces: Workspace[] }> {
  return fetchAPI('/api/v1/workspaces', token);
}

export async function createWorkspace(token: string, name: string): Promise<{ workspace: Workspace }> {
  return fetchAPI('/api/v1/workspaces', token, {
    method: 'POST',
    body: JSON.stringify({ name }),
  });
}

export async function getWorkspace(token: string, id: string): Promise<{ workspace: Workspace }> {
  return fetchAPI(`/api/v1/workspaces/${id}`, token);
}

export async function updateWorkspaceSettings(
  token: string,
  workspaceId: string,
  settings: { log_retention_days: number }
): Promise<{ workspace: Workspace }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/settings`, token, {
    method: 'PUT',
    body: JSON.stringify(settings),
  });
}

export interface WorkspaceUsageSummary {
  plan: 'free' | 'pro' | 'team';
  usage: Record<string, { used: number; limit: number; remaining: number; period: string }>;
}

export async function getWorkspaceUsage(token: string, workspaceId: string): Promise<WorkspaceUsageSummary> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/usage`, token);
}

// ── Projects ──────────────────────────────────────────────────────

export interface Project {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  repo_url: string;
  repo_provider: string;
  default_branch: string;
  status: string;
  indexing_status: string;
  created_at: string;
  updated_at: string;
}

export async function getProjects(token: string, workspaceId: string): Promise<{ projects: Project[] }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects`, token);
}

export async function createProject(token: string, workspaceId: string, data: { name: string; description: string }): Promise<{ project: Project }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects`, token, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function deleteProject(token: string, workspaceId: string, projectId: string): Promise<void> {
  await fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}`, token, {
    method: 'DELETE',
  });
}

// ── Integrations ────────────────────────────────────────────────

export interface GitHubIntegrationConfig {
  install_url: string;
  callback_url: string;
  webhook_url: string;
  configured: boolean;
}

export interface GitHubInstallation {
  id: string;
  installation_id: number;
  account_login: string;
  account_type: string;
  repository_selection: string;
  status: string;
  installed_at: string;
}

export interface GitHubRepository {
  repository_id: number;
  owner: string;
  name: string;
  full_name: string;
  html_url: string;
  clone_url: string;
  default_branch: string;
  private: boolean;
  // Absent today. Populated once the integration service annotates repositories
  // whose access the installation no longer grants.
  status?: string;
}

// github_installations.status, per infra/migrations/002_github_ingestion_foundation.sql.
// 'error' is transient and stays selectable; these three are not.
const UNUSABLE_INSTALLATION_STATUSES = new Set(['revoked', 'suspended', 'deleted']);

/** A missing status means an older payload, which must keep working. */
export function isInstallationUsable(status?: string | null): boolean {
  return !UNUSABLE_INSTALLATION_STATUSES.has((status ?? '').trim().toLowerCase());
}

export type RepositoryConnectionHealth = 'connected' | 'revoked' | 'unhealthy';

// repository_connections.status. 'revoked' is added by the installation-status
// change; the rest come from migration 002.
const UNHEALTHY_CONNECTION_STATUSES = new Set(['disabled', 'archived', 'error']);

/**
 * Missing or unrecognised statuses read as connected on purpose: `status` is
 * being added server-side by a separate change, and this UI must not regress
 * against payloads that predate it.
 */
export function repositoryConnectionHealth(status?: string | null): RepositoryConnectionHealth {
  const normalized = (status ?? '').trim().toLowerCase();
  if (normalized === 'revoked') return 'revoked';
  if (UNHEALTHY_CONNECTION_STATUSES.has(normalized)) return 'unhealthy';
  return 'connected';
}

export function isRevokedConnection(status?: string | null): boolean {
  return repositoryConnectionHealth(status) === 'revoked';
}

export interface RepositoryConnection {
  id: string;
  workspace_id: string;
  project_id: string;
  full_name: string;
  html_url: string;
  selected_branch: string;
  status: string;
  // protobuf json tags carry omitempty, so these keys are ABSENT (not empty
  // strings) until the repository has been indexed at least once.
  last_indexed_commit_sha?: string;
  last_indexed_at?: string;
}

export interface IngestionJob {
  id: string;
  workspace_id: string;
  project_id: string;
  status: string;
  source_branch: string;
  source_commit_sha: string;
  started_at: string;
  finished_at: string;
}

export async function getGitHubIntegrationConfig(token: string): Promise<GitHubIntegrationConfig> {
  return fetchAPI('/api/v1/integrations/github/config', token);
}

export async function createGitHubInstallSession(token: string, workspaceId: string): Promise<{ install_url: string; expires_at: string }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/integrations/github/install-session`, token, { method: 'POST' });
}

export async function listGitHubInstallations(token: string, workspaceId: string): Promise<{ installations: GitHubInstallation[] }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/integrations/github/installations`, token);
}

export async function listGitHubRepositories(token: string, workspaceId: string, installationId: string): Promise<{ repositories: GitHubRepository[] }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/integrations/github/installations/${installationId}/repositories`, token);
}

export interface LogProviderConnection {
  id: string;
  provider: 'vercel' | 'railway';
  display_name: string;
  project_id: string;
  deployment_id: string;
  scope_id: string;
  token_kind: 'bearer' | 'project_token';
  status: string;
  created_at: string;
  updated_at: string;
}

export async function listLogProviderConnections(token: string, workspaceId: string): Promise<{ connections: LogProviderConnection[] }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/integrations/log-providers`, token);
}

export async function saveLogProviderConnection(
  token: string,
  workspaceId: string,
  connection: {
    provider: 'vercel' | 'railway';
    display_name: string;
    token: string;
    project_id: string;
    deployment_id: string;
    scope_id: string;
    token_kind: 'bearer' | 'project_token';
  }
): Promise<{ connection: LogProviderConnection }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/integrations/log-providers`, token, {
    method: 'POST',
    body: JSON.stringify(connection),
  });
}

export async function attachRepository(
  token: string,
  workspaceId: string,
  projectId: string,
  data: {
    github_installation_id: string;
    repository_id: number;
    owner: string;
    name: string;
    full_name: string;
    html_url: string;
    clone_url: string;
    default_branch: string;
    selected_branch: string;
    private: boolean;
  }
): Promise<{ repository_connection: RepositoryConnection; ingestion_job: IngestionJob }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/repository`, token, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getRepositoryConnection(
  token: string,
  workspaceId: string,
  projectId: string
): Promise<{ repository_connection: RepositoryConnection | null; latest_ingestion_job: IngestionJob | null }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/repository`, token);
}

// ── Connected repositories (dashboard overview) ─────────────────

export interface RepositoryConnectionSummary {
  repository_connection: RepositoryConnection;
  // Absent from the payload when the project has never been indexed.
  latest_ingestion_job?: IngestionJob | null;
  project_name: string;
  workspace_name: string;
}

/**
 * Lists every repository connection the caller can see. Omit workspaceId to
 * span every workspace the caller belongs to; the integration service bounds
 * results by workspace membership either way.
 */
export async function listConnectedRepositories(
  token: string,
  workspaceId?: string
): Promise<{ connections: RepositoryConnectionSummary[] }> {
  const path = workspaceId
    ? `/api/v1/workspaces/${workspaceId}/repositories`
    : '/api/v1/repositories';
  return fetchAPI(path, token);
}

// ── Project intelligence ───────────────────────────────────────

export interface RAGSource {
  id: string;
  file_path: string;
  text: string;
  score: number;
  start_line: number;
  end_line: number;
  citation: string;
}

export interface ProjectAnswer {
  answer: string;
  confidence: 'high' | 'medium' | 'low' | 'insufficient';
  enough_context: boolean;
  sources: RAGSource[];
}

export async function queryProject(
  token: string,
  workspaceId: string,
  projectId: string,
  question: string
): Promise<ProjectAnswer> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/query`, token, {
    method: 'POST',
    body: JSON.stringify({ question, top_k: 8 }),
  });
}

export interface IncidentAnalysisResponse {
  incident_id: string;
  workflow_id: string;
  log_id: string;
  severity: 'info' | 'warning' | 'error' | 'critical';
  error_fingerprint: string;
  redactions: number;
  analysis: {
    title: string;
    summary: string;
    root_cause: string;
    fix_recommendation: string;
    confidence: 'high' | 'medium' | 'low' | 'insufficient';
    assumptions: string[];
  };
  evidence: Array<{
    id: string;
    citation: string;
    file_path: string;
    score: number;
  }>;
}

export async function analyzeProjectLogs(
  token: string,
  workspaceId: string,
  projectId: string,
  content: string,
  source = 'manual'
): Promise<IncidentAnalysisResponse> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/incidents/analyze`, token, {
    method: 'POST',
    body: JSON.stringify({ source, content }),
  });
}

export async function collectAndAnalyzeProjectLogs(
  token: string,
  workspaceId: string,
  projectId: string,
  integrationId: string,
  logKind: 'runtime' | 'build' = 'runtime'
): Promise<IncidentAnalysisResponse> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/incidents/collect`, token, {
    method: 'POST',
    body: JSON.stringify({ integration_id: integrationId, log_kind: logKind, limit: 300 }),
  });
}

export interface WorkflowStepSummary {
  name: string;
  status: string;
  order: number;
  error: string | null;
}

export interface IncidentHistoryItem {
  incident_id: string;
  title: string;
  status: string;
  severity: 'info' | 'warning' | 'error' | 'critical';
  summary: string;
  root_cause: string;
  confidence: 'high' | 'medium' | 'low' | 'insufficient';
  fix_recommendation: string;
  error_fingerprint: string;
  source: string;
  created_at: string;
  analysis_workflow: { workflow_id: string; status: string; steps: WorkflowStepSummary[] };
  fix_proposal: null | {
    proposal_id: string;
    workflow_id: string;
    workflow_status: string;
    status: string;
    base_commit_sha: string;
    unified_diff: string;
    verification: { tests?: { status: string; reason: string }; verified_fix?: boolean };
    changes: Array<{ path: string; reason: string; additions: number; deletions: number }>;
    steps: WorkflowStepSummary[];
    approval: null | { approval_id: string; status: string; expires_at: string | null };
    publish_result: { pr_url?: string; pr_number?: number; branch?: string; draft?: boolean };
  };
}

export async function getIncidentHistory(token: string, workspaceId: string, projectId: string): Promise<{ incidents: IncidentHistoryItem[] }> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/incidents`, token);
}

export interface DeploymentReadinessReport {
  commit_sha: string;
  score: number;
  ready: boolean;
  stack: { kind: string; framework: string; root: string; build: string; start: string; port: number; manifest?: string };
  required_env_names: string[];
  findings: Array<{
    code: string;
    severity: 'blocker' | 'warning' | 'info';
    title: string;
    detail: string;
    remediation: string;
    citations: string[];
  }>;
  provider_recommendations: Array<{ provider: string; fit: string; reason: string }>;
}

export interface GeneratedDeploymentFiles {
  commit_sha: string;
  stack: DeploymentReadinessReport['stack'];
  dockerfile: string;
  compose_yaml: string;
  env_example: string;
  assumptions: string[];
  citations: string[];
}

export async function scanDeploymentReadiness(token: string, workspaceId: string, projectId: string): Promise<DeploymentReadinessReport> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/deployment/readiness`, token, { method: 'POST' });
}

export async function generateDeploymentFiles(token: string, workspaceId: string, projectId: string): Promise<GeneratedDeploymentFiles> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/deployment/generate`, token, { method: 'POST' });
}

export interface FixProposalResponse {
  proposal_id: string;
  approval_id: string;
  workflow_id: string;
  approval_status: 'pending';
  approval_expires_at: string;
  base_commit_sha: string;
  unified_diff: string;
  changes: Array<{
    path: string;
    reason: string;
    additions: number;
    deletions: number;
  }>;
  verification: {
    base_commit_pinned: boolean;
    path_policy: string;
    secret_scan: string;
    static_validation: string;
    tests: { status: string; reason: string };
    verified_fix: boolean;
  };
}

export async function proposeIncidentFix(
  token: string,
  workspaceId: string,
  projectId: string,
  incidentId: string
): Promise<FixProposalResponse> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/incidents/${incidentId}/fix-proposal`, token, {
    method: 'POST',
  });
}

export interface FixApprovalDecisionResponse {
  approval_id: string;
  approval_status: 'rejected' | 'executed';
  pull_request_url: string;
  pull_request_number: number;
  branch: string;
}

export async function decideFixApproval(
  token: string,
  workspaceId: string,
  projectId: string,
  approvalId: string,
  decision: 'approve' | 'reject',
  reason = ''
): Promise<FixApprovalDecisionResponse> {
  return fetchAPI(`/api/v1/workspaces/${workspaceId}/projects/${projectId}/approvals/${approvalId}/decision`, token, {
    method: 'POST',
    body: JSON.stringify({ decision, reason }),
  });
}
