'use client';

import { use, useState, useEffect, useCallback } from 'react';
import { useAuth } from '@clerk/nextjs';
import Link from 'next/link';
import { Box, ArrowLeft, Loader2, GitBranch, Clock, ExternalLink, AlertCircle, RefreshCw, Send, FileCode2, ShieldCheck, Terminal, Search, GitPullRequest, LockKeyhole, Container, Clipboard, Trash2 } from 'lucide-react';
import {
  getProjects,
  createProject,
  deleteProject,
  getWorkspace,
  updateWorkspaceSettings,
  getGitHubIntegrationConfig,
  createGitHubInstallSession,
  listGitHubInstallations,
  listGitHubRepositories,
  attachRepository,
  getRepositoryConnection,
  Project,
  Workspace,
  GitHubIntegrationConfig,
  GitHubInstallation,
  GitHubRepository,
  RepositoryConnection,
  IngestionJob,
  ProjectAnswer,
  queryProject,
  IncidentAnalysisResponse,
  analyzeProjectLogs,
  FixProposalResponse,
  proposeIncidentFix,
  FixApprovalDecisionResponse,
  decideFixApproval,
  LogProviderConnection,
  listLogProviderConnections,
  saveLogProviderConnection,
  collectAndAnalyzeProjectLogs,
  IncidentHistoryItem,
  getIncidentHistory,
  DeploymentReadinessReport,
  GeneratedDeploymentFiles,
  scanDeploymentReadiness,
  generateDeploymentFiles,
  WorkspaceUsageSummary,
  getWorkspaceUsage
} from '@/lib/api';
import { ingestionBadge } from '@/lib/ingestion';

const GithubIcon = ({ size = 20, className = "" }: { size?: number; className?: string }) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className}
  >
    <path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" />
    <path d="M9 18c-4.51 2-5-2-7-2" />
  </svg>
);


export default function WorkspaceDetail({ params }: { params: Promise<{ workspaceId: string }> }) {
  const { workspaceId } = use(params);
  const { getToken } = useAuth();

  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [isCreating, setIsCreating] = useState(false);

  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectDesc, setNewProjectDesc] = useState('');

  // GitHub connection states
  const [githubConfig, setGithubConfig] = useState<GitHubIntegrationConfig | null>(null);
  const [installations, setInstallations] = useState<GitHubInstallation[]>([]);
  const [selectedInst, setSelectedInst] = useState<string>('');
  const [repositories, setRepositories] = useState<GitHubRepository[]>([]);
  const [selectedRepoId, setSelectedRepoId] = useState<string>('');
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepository | null>(null);
  const [loadingConfig, setLoadingConfig] = useState(true);
  const [loadingRepos, setLoadingRepos] = useState(false);
  const [startingInstall, setStartingInstall] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const [logProviders, setLogProviders] = useState<LogProviderConnection[]>([]);
  const [usage, setUsage] = useState<WorkspaceUsageSummary | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const token = await getToken();
      if (!token) return;

      const [wsRes, projRes, configRes, instsRes, providersRes, usageRes] = await Promise.all([
        getWorkspace(token, workspaceId),
        getProjects(token, workspaceId),
        getGitHubIntegrationConfig(token).catch(e => { console.error(e); return null; }),
        listGitHubInstallations(token, workspaceId).catch(e => { console.error(e); return { installations: [] }; }),
        listLogProviderConnections(token, workspaceId).catch(e => { console.error(e); return { connections: [] }; }),
        getWorkspaceUsage(token, workspaceId).catch(e => { console.error(e); return null; })
      ]);

      setWorkspace(wsRes.workspace);
      setProjects(projRes.projects || []);
      if (configRes) setGithubConfig(configRes);
      if (instsRes) setInstallations(instsRes.installations || []);
      setLogProviders(providersRes.connections || []);
      setUsage(usageRes);

      if (instsRes?.installations?.length > 0) {
        setSelectedInst(instsRes.installations[0].id);
      }
    } catch (error) {
      console.error('Error fetching workspace data:', error);
    } finally {
      setLoading(false);
      setLoadingConfig(false);
    }
  }, [getToken, workspaceId]);

  useEffect(() => {
    // Remote bootstrap is intentionally effect-driven because Clerk's session
    // token is only available in this interactive client boundary.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchData();
  }, [fetchData]);

  // Load repositories when installation changes
  useEffect(() => {
    async function loadRepos() {
      if (!selectedInst) {
        setRepositories([]);
        return;
      }
      setLoadingRepos(true);
      try {
        const token = await getToken();
        if (!token) return;
        const res = await listGitHubRepositories(token, workspaceId, selectedInst);
        setRepositories(res.repositories || []);
        setSelectedRepoId('');
        setSelectedRepo(null);
      } catch (e) {
        console.error('Error loading repositories:', e);
        setRepositories([]);
      } finally {
        setLoadingRepos(false);
      }
    }
    loadRepos();
  }, [selectedInst, getToken, workspaceId]);

  const handleSelectRepo = (repoIdStr: string) => {
    setSelectedRepoId(repoIdStr);
    const repo = repositories.find(r => r.repository_id.toString() === repoIdStr);
    setSelectedRepo(repo || null);
    if (repo) {
      setNewProjectName(repo.name);
    } else {
      setNewProjectName('');
    }
  };

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!newProjectName.trim()) return;

    setIsCreating(true);
    setCreateError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again before creating a project.');

      // 1. Create project
      const res = await createProject(token, workspaceId, {
        name: newProjectName,
        description: newProjectDesc
      });

      const newProj = res.project;

      // 2. Attach repository if selected. A failure here leaves a project with
      // no repository, so it must surface instead of only reaching the console.
      if (selectedRepo) {
        try {
          await attachRepository(token, workspaceId, newProj.id, {
            github_installation_id: selectedInst,
            repository_id: selectedRepo.repository_id,
            owner: selectedRepo.owner,
            name: selectedRepo.name,
            full_name: selectedRepo.full_name,
            html_url: selectedRepo.html_url,
            clone_url: selectedRepo.clone_url,
            default_branch: selectedRepo.default_branch,
            selected_branch: selectedRepo.default_branch,
            private: selectedRepo.private
          });
        } catch (attachErr) {
          console.error('Error attaching repository:', attachErr);
          setCreateError(
            `Project "${newProj.name}" was created, but the repository could not be attached: ` +
            (attachErr instanceof Error ? attachErr.message : 'unknown error')
          );
        }
      }

      setNewProjectName('');
      setNewProjectDesc('');
      setSelectedRepoId('');
      setSelectedRepo(null);

      // Reload projects list to get connection status updated
      const projRes = await getProjects(token, workspaceId);
      setProjects(projRes.projects || []);
    } catch (error) {
      console.error('Error creating project:', error);
      setCreateError(error instanceof Error ? error.message : 'Project could not be created.');
    } finally {
      setIsCreating(false);
    }
  }

  if (loading) {
    return (
      <div className="flex-center h-full">
        <Loader2 className="animate-spin text-indigo-500" size={32} />
      </div>
    );
  }

  if (!workspace) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-zinc-400">
        <h2 className="text-xl mb-4">Workspace not found</h2>
        <Link href="/dashboard" className="btn btn-secondary"><ArrowLeft size={16} /> Back to Dashboard</Link>
      </div>
    );
  }

  async function startGitHubInstall() {
    setStartingInstall(true);
    setInstallError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again before connecting GitHub.');
      const session = await createGitHubInstallSession(token, workspaceId);
      window.location.assign(session.install_url);
    } catch (error) {
      setInstallError(error instanceof Error ? error.message : 'Failed to prepare GitHub installation.');
      setStartingInstall(false);
    }
  }

  return (
    <div className="max-w-6xl mx-auto">
      <Link href="/dashboard" className="inline-flex items-center gap-2 text-zinc-400 hover:text-white mb-6 transition-colors text-sm font-medium">
        <ArrowLeft size={16} /> Back to Workspaces
      </Link>

      <div className="flex-between mb-8 animate-fade-in">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <h1 className="text-3xl font-bold font-display text-white">{workspace.name}</h1>
            <span className="badge badge-neutral">{workspace.plan.toUpperCase()}</span>
          </div>
          <p className="text-zinc-400 font-mono text-sm">{workspace.slug}</p>
        </div>
      </div>

      {usage && <WorkspaceUsage usage={usage} />}

      <WorkspaceRetentionSettings
        workspace={workspace}
        onUpdated={(updated) => setWorkspace(updated)}
      />

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6 animate-fade-in animate-delay-1">
        {/* Connection & Project Creation Card */}
        <div className="glass-card p-6 border-dashed border-2 border-[var(--border-medium)] bg-[var(--bg-glass)] hover:bg-[#ffffff02] transition-colors">
          <div className="mb-4">
            <div className="w-10 h-10 rounded-full bg-indigo-500/20 flex-center mb-3">
              <GithubIcon size={20} className="text-indigo-400" />
            </div>
            <h3 className="text-lg font-medium text-white">Connect Repository</h3>
            <p className="text-xs text-zinc-400 mt-1">Connect GitHub repository to initialize AI context.</p>
          </div>

          {!loadingConfig && githubConfig && !githubConfig.configured ? (
            <div className="bg-red-500/10 border border-red-500/20 rounded-lg p-3 text-xs text-red-400 flex items-start gap-2 mb-4">
              <AlertCircle size={16} className="shrink-0 mt-0.5" />
              <span>GitHub App is not configured on the backend. Please check your settings.</span>
            </div>
          ) : installations.length === 0 ? (
            <div className="flex flex-col gap-3">
              <p className="text-xs text-zinc-500">Attach OpsPilot GitHub App to list your account/org repositories.</p>
              {installError && <p className="text-xs text-red-400">{installError}</p>}
              <button
                type="button"
                onClick={startGitHubInstall}
                disabled={startingInstall}
                className="btn btn-primary w-full flex items-center justify-center gap-2"
              >
                {startingInstall ? <Loader2 size={16} className="animate-spin" /> : <GithubIcon size={16} />}
                {startingInstall ? 'Preparing…' : 'Install OpsPilot App'}
              </button>
              <button onClick={fetchData} className="btn btn-secondary w-full text-xs flex items-center justify-center gap-1">
                <RefreshCw size={12} /> Refresh Connection
              </button>
            </div>
          ) : (
            <form onSubmit={handleCreate} className="flex flex-col gap-3 text-xs">
              {createError && (
                <div className="bg-red-500/10 border border-red-500/20 rounded-lg p-3 text-xs text-red-400 flex items-start gap-2">
                  <AlertCircle size={14} className="shrink-0 mt-0.5" />
                  <span>{createError}</span>
                </div>
              )}

              <div className="flex flex-col gap-1">
                <label className="text-zinc-400 font-medium">GitHub Organization / Account</label>
                <select
                  className="input-field py-1.5 px-2 bg-zinc-900 border-[#ffffff15] text-white rounded cursor-pointer"
                  value={selectedInst}
                  onChange={(e) => setSelectedInst(e.target.value)}
                >
                  {installations.map(inst => (
                    <option key={inst.id} value={inst.id}>
                      {inst.account_login} ({inst.account_type})
                    </option>
                  ))}
                </select>
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-zinc-400 font-medium">Repository</label>
                {loadingRepos ? (
                  <div className="flex items-center gap-2 py-1.5 text-zinc-400">
                    <Loader2 size={12} className="animate-spin" /> Loading repositories...
                  </div>
                ) : (
                  <select
                    className="input-field py-1.5 px-2 bg-zinc-900 border-[#ffffff15] text-white rounded cursor-pointer"
                    value={selectedRepoId}
                    onChange={(e) => handleSelectRepo(e.target.value)}
                  >
                    <option value="">-- Select Repository --</option>
                    {repositories.map(repo => (
                      <option key={repo.repository_id} value={repo.repository_id}>
                        {repo.full_name} {repo.private ? '(Private)' : '(Public)'}
                      </option>
                    ))}
                  </select>
                )}
              </div>

              <div className="flex flex-col gap-1 mt-2">
                <label className="text-zinc-400 font-medium">Project Name</label>
                <input
                  type="text"
                  placeholder="e.g. core-api"
                  className="input-field"
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  disabled={isCreating}
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-zinc-400 font-medium">Description (Optional)</label>
                <textarea
                  placeholder="App details..."
                  className="input-field resize-none h-14"
                  value={newProjectDesc}
                  onChange={(e) => setNewProjectDesc(e.target.value)}
                  disabled={isCreating}
                />
              </div>

              <button
                type="submit"
                className="btn btn-primary w-full mt-2"
                disabled={isCreating || !newProjectName.trim()}
              >
                {isCreating ? <Loader2 size={14} className="animate-spin" /> : 'Create & Connect Project'}
              </button>
            </form>
          )}
        </div>

        <LogProviderSettings
          workspaceId={workspaceId}
          connections={logProviders}
          onSaved={(connection) => setLogProviders((current) => [connection, ...current.filter((item) => item.id !== connection.id)])}
        />

        {/* Project List */}
        {projects.map((p) => (
          <ProjectCard
            key={p.id}
            project={p}
            workspaceId={workspaceId}
            logProviders={logProviders}
            onDeleted={() => {
              setProjects((current) => current.filter((item) => item.id !== p.id));
              void getToken().then((token) => token ? getWorkspaceUsage(token, workspaceId) : null).then((summary) => {
                if (summary) setUsage(summary);
              });
            }}
          />
        ))}
      </div>
    </div>
  );
}

function WorkspaceUsage({ usage }: { usage: WorkspaceUsageSummary }) {
  const labels: Record<string, string> = {
    ai_query: 'AI questions',
    incident_analysis: 'Incident analyses',
    fix_proposal: 'Fix proposals',
    deployment_analysis: 'Deployment analyses',
    projects: 'Projects',
    indexed_storage_bytes: 'Indexed storage',
  };
  return (
    <details className="glass-card p-4 mb-6 animate-fade-in">
      <summary className="cursor-pointer list-none flex items-center gap-2 text-xs text-zinc-300">
        <ShieldCheck size={14} className="text-indigo-300" /> {usage.plan.toUpperCase()} plan usage
        <span className="ml-auto text-[10px] text-zinc-500">Atomic workspace limits</span>
      </summary>
      <div className="mt-3 grid grid-cols-2 lg:grid-cols-3 gap-3">
        {Object.entries(usage.usage).map(([key, item]) => {
          const isBytes = key === 'indexed_storage_bytes';
          const ratio = item.limit ? Math.min(100, Math.round((item.used / item.limit) * 100)) : 0;
          const display = isBytes
            ? `${(item.used / 1024 / 1024).toFixed(1)} / ${(item.limit / 1024 / 1024).toFixed(0)} MiB`
            : `${item.used} / ${item.limit}`;
          return (
            <div key={key} className="rounded bg-black/20 p-2 text-[10px]">
              <div className="flex justify-between gap-2 text-zinc-400"><span>{labels[key] || key}</span><span className="font-mono">{display}</span></div>
              <div className="mt-1.5 h-1 rounded bg-white/5 overflow-hidden"><div className={`h-full ${ratio >= 90 ? 'bg-red-400' : 'bg-indigo-400'}`} style={{ width: `${ratio}%` }} /></div>
            </div>
          );
        })}
      </div>
    </details>
  );
}

function WorkspaceRetentionSettings({ workspace, onUpdated }: { workspace: Workspace; onUpdated: (workspace: Workspace) => void }) {
  const { getToken } = useAuth();
  const [days, setDays] = useState(workspace.log_retention_days || 30);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const canManage = workspace.role === 'owner' || workspace.role === 'admin';

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!canManage || days < 1 || days > 365) return;
    setSaving(true);
    setMessage('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again before updating retention.');
      const response = await updateWorkspaceSettings(token, workspace.id, { log_retention_days: days });
      onUpdated(response.workspace);
      setMessage('Saved. Expired log bodies are removed by the background lifecycle job.');
    } catch (requestError) {
      setMessage(requestError instanceof Error ? requestError.message : 'Retention could not be updated.');
    } finally {
      setSaving(false);
    }
  }

  return (
    <details className="glass-card p-4 mb-6 animate-fade-in">
      <summary className="cursor-pointer list-none flex items-center gap-2 text-xs text-zinc-300">
        <Clock size={14} className="text-cyan-300" /> Log data lifecycle
        <span className="ml-auto text-[10px] text-zinc-500">{workspace.log_retention_days || 30} day retention</span>
      </summary>
      <form onSubmit={save} className="mt-3 flex flex-wrap items-end gap-3 text-xs">
        <label className="space-y-1">
          <span className="block text-zinc-400">Retain stored log bodies (1–365 days)</span>
          <input
            type="number"
            className="input-field w-36"
            min={1}
            max={365}
            value={days}
            onChange={(event) => setDays(Number(event.target.value))}
            disabled={!canManage || saving}
          />
        </label>
        <button type="submit" className="btn btn-secondary" disabled={!canManage || saving || days < 1 || days > 365}>
          {saving ? <Loader2 size={13} className="animate-spin" /> : <ShieldCheck size={13} />}
          Save retention
        </button>
        {!canManage && <span className="text-zinc-500">Only workspace owners and admins can change this.</span>}
      </form>
      <p className="mt-2 text-[10px] text-zinc-500">Incident summaries, fix history, and audit records remain after log bodies expire.</p>
      {message && <p className="mt-2 text-[10px] text-zinc-300">{message}</p>}
    </details>
  );
}

function LogProviderSettings({
  workspaceId,
  connections,
  onSaved,
}: {
  workspaceId: string;
  connections: LogProviderConnection[];
  onSaved: (connection: LogProviderConnection) => void;
}) {
  const { getToken } = useAuth();
  const [provider, setProvider] = useState<'vercel' | 'railway'>('vercel');
  const [displayName, setDisplayName] = useState('');
  const [providerToken, setProviderToken] = useState('');
  const [providerProjectId, setProviderProjectId] = useState('');
  const [deploymentId, setDeploymentId] = useState('');
  const [scopeId, setScopeId] = useState('');
  const [tokenKind, setTokenKind] = useState<'bearer' | 'project_token'>('bearer');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  async function handleSave(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again to save the provider connection.');
      const response = await saveLogProviderConnection(token, workspaceId, {
        provider,
        display_name: displayName,
        token: providerToken,
        project_id: providerProjectId,
        deployment_id: deploymentId,
        scope_id: scopeId,
        token_kind: provider === 'vercel' ? 'bearer' : tokenKind,
      });
      onSaved(response.connection);
      setProviderToken('');
      setError('');
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Provider connection could not be saved.');
    } finally {
      setSaving(false);
    }
  }

  return (
    <details className="glass-card p-6 md:col-span-2">
      <summary className="cursor-pointer list-none flex items-center gap-3">
        <div className="w-10 h-10 rounded-full bg-emerald-500/15 flex-center"><Terminal size={19} className="text-emerald-300" /></div>
        <div>
          <h3 className="text-lg font-medium text-white">Production log sources</h3>
          <p className="text-xs text-zinc-400">Vercel and Railway tokens are encrypted and never returned.</p>
        </div>
        <span className="ml-auto badge badge-neutral text-[10px]">{connections.length} connected</span>
      </summary>
      <form onSubmit={handleSave} className="mt-5 grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
        <label className="space-y-1">
          <span className="text-zinc-400">Provider</span>
          <select className="input-field bg-zinc-900" value={provider} onChange={(event) => setProvider(event.target.value as 'vercel' | 'railway')}>
            <option value="vercel">Vercel</option>
            <option value="railway">Railway</option>
          </select>
        </label>
        <label className="space-y-1">
          <span className="text-zinc-400">Connection name</span>
          <input className="input-field" value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="Production API" maxLength={120} />
        </label>
        <label className="space-y-1 sm:col-span-2">
          <span className="text-zinc-400">Access token (write-only)</span>
          <input className="input-field font-mono" type="password" autoComplete="new-password" value={providerToken} onChange={(event) => setProviderToken(event.target.value)} required minLength={8} maxLength={4096} />
        </label>
        <label className="space-y-1">
          <span className="text-zinc-400">{provider === 'vercel' ? 'Vercel project ID' : 'Railway project ID (optional)'}</span>
          <input className="input-field font-mono" value={providerProjectId} onChange={(event) => setProviderProjectId(event.target.value)} required={provider === 'vercel'} maxLength={256} />
        </label>
        <label className="space-y-1">
          <span className="text-zinc-400">Deployment ID</span>
          <input className="input-field font-mono" value={deploymentId} onChange={(event) => setDeploymentId(event.target.value)} required maxLength={256} />
        </label>
        {provider === 'vercel' ? (
          <label className="space-y-1">
            <span className="text-zinc-400">Team ID (optional)</span>
            <input className="input-field font-mono" value={scopeId} onChange={(event) => setScopeId(event.target.value)} maxLength={256} />
          </label>
        ) : (
          <label className="space-y-1">
            <span className="text-zinc-400">Token type</span>
            <select className="input-field bg-zinc-900" value={tokenKind} onChange={(event) => setTokenKind(event.target.value as 'bearer' | 'project_token')}>
              <option value="project_token">Project access token (recommended)</option>
              <option value="bearer">Account/workspace bearer token</option>
            </select>
          </label>
        )}
        <div className="sm:col-span-2 flex items-center gap-3">
          <button className="btn btn-primary" type="submit" disabled={saving || !providerToken || !deploymentId || (provider === 'vercel' && !providerProjectId)}>
            {saving ? <Loader2 size={14} className="animate-spin" /> : <ShieldCheck size={14} />}
            {saving ? 'Encrypting…' : 'Save log source'}
          </button>
          <span className="text-[10px] text-zinc-500">Read-only log access is recommended.</span>
        </div>
        {error && <p className="sm:col-span-2 text-red-300">{error}</p>}
      </form>
      {connections.length > 0 && (
        <div className="mt-4 flex flex-wrap gap-2">
          {connections.map((connection) => (
            <span key={connection.id} className="badge badge-success text-[10px]">
              {connection.display_name} · {connection.provider} · {connection.deployment_id.slice(0, 12)}
            </span>
          ))}
        </div>
      )}
    </details>
  );
}

function ProjectCard({
  project,
  workspaceId,
  logProviders,
  onDeleted,
}: {
  project: Project;
  workspaceId: string;
  logProviders: LogProviderConnection[];
  onDeleted: () => void;
}) {
  const { getToken } = useAuth();
  const [repoConn, setRepoConn] = useState<RepositoryConnection | null>(null);
  const [latestJob, setLatestJob] = useState<IngestionJob | null>(null);
  const [loading, setLoading] = useState(true);
  const [deleteConfirmation, setDeleteConfirmation] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState('');

  async function handleDelete() {
    if (deleteConfirmation !== project.name || deleting) return;
    setDeleting(true);
    setDeleteError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again before deleting this project.');
      await deleteProject(token, workspaceId, project.id);
      onDeleted();
    } catch (requestError) {
      setDeleteError(requestError instanceof Error ? requestError.message : 'Project deletion failed.');
      setDeleting(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    let pollTimer: ReturnType<typeof setTimeout> | undefined;

    async function loadRepoConn() {
      try {
        const token = await getToken();
        if (!token) return;
        const res = await getRepositoryConnection(token, workspaceId, project.id);
        if (res && !cancelled) {
          setRepoConn(res.repository_connection);
          setLatestJob(res.latest_ingestion_job);
          if (res.latest_ingestion_job && ['queued', 'running'].includes(res.latest_ingestion_job.status)) {
            pollTimer = setTimeout(loadRepoConn, 3000);
          }
        }
      } catch (e) {
        console.error('Error loading repo connection:', e);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    pollTimer = setTimeout(loadRepoConn, 0);
    return () => {
      cancelled = true;
      if (pollTimer) clearTimeout(pollTimer);
    };
  }, [project.id, workspaceId, getToken]);

  return (
    <div className="glass-card p-6 flex flex-col group justify-between">
      <div>
        <div className="flex-between mb-4">
          <div className="w-10 h-10 rounded-lg bg-[#ffffff1a] border border-[#ffffff26] flex-center">
            <Box size={20} className="text-white" />
          </div>
          {loading ? (
            <Loader2 size={14} className="animate-spin text-zinc-500" />
          ) : repoConn ? (
            repoConn.status === 'connected' ? (
              <span className="badge badge-success flex items-center gap-1 text-xs">
                <div className="w-1.5 h-1.5 rounded-full bg-emerald-400" /> Connected
              </span>
            ) : (
              <span className="badge badge-error text-xs capitalize">{repoConn.status}</span>
            )
          ) : (
            <span className="badge badge-neutral text-xs">No Repository</span>
          )}
        </div>

        <h3 className="text-xl font-medium text-white mb-2 group-hover:text-indigo-400 transition-colors">
          {project.name}
        </h3>
        <p className="text-sm text-zinc-400 mb-6 line-clamp-2">
          {project.description || 'No description provided.'}
        </p>

        {repoConn && (
          <div className="bg-[#ffffff05] rounded-lg p-3 border border-[#ffffff0a] mb-6 text-xs">
            <div className="flex items-center justify-between text-zinc-300 mb-1.5">
              <span className="font-semibold text-zinc-400">Repository:</span>
              <a
                href={repoConn.html_url}
                target="_blank"
                rel="noreferrer"
                className="text-indigo-400 hover:underline flex items-center gap-1"
              >
                {repoConn.full_name} <ExternalLink size={12} />
              </a>
            </div>
            <div className="flex items-center justify-between text-zinc-300">
              <span className="font-semibold text-zinc-400">Branch:</span>
              <span className="font-mono flex items-center gap-1"><GitBranch size={12} /> {repoConn.selected_branch}</span>
            </div>
            {latestJob && (
              <div className="flex items-center justify-between mt-2 pt-2 border-t border-[#ffffff0a] text-zinc-300">
                <span className="font-semibold text-zinc-400 font-display">Ingestion:</span>
                <span className={`badge ${ingestionBadge(latestJob.status).className} text-xs`}>
                  {ingestionBadge(latestJob.status).label}
                </span>
              </div>
            )}
          </div>
        )}

        {repoConn && latestJob?.status === 'succeeded' && (
          <>
            <DeploymentReadinessPanel workspaceId={workspaceId} projectId={project.id} />
            <ProjectQAPanel workspaceId={workspaceId} projectId={project.id} />
            <LogAnalysisPanel workspaceId={workspaceId} projectId={project.id} logProviders={logProviders} />
          </>
        )}
      </div>

      <div className="pt-4 border-t border-[var(--border-light)] flex items-center justify-between text-xs text-zinc-500 font-mono mt-auto">
        <span className="flex items-center gap-1"><Clock size={12} /> {new Date(project.created_at).toLocaleDateString()}</span>
        {repoConn?.last_indexed_commit_sha && (
          <span className="font-mono text-zinc-400">SHA: {repoConn.last_indexed_commit_sha.substring(0, 7)}</span>
        )}
      </div>
      <details className="mt-3 rounded border border-red-400/15 bg-red-500/5 p-2 text-xs">
        <summary className="cursor-pointer list-none flex items-center gap-2 text-red-300">
          <Trash2 size={13} /> Delete project data
        </summary>
        <p className="mt-2 text-[10px] leading-4 text-zinc-500">
          Permanently deletes this project, indexed code, logs, incidents, approvals, and provider associations. Workspace audit history keeps only the project name and ID.
        </p>
        <label className="mt-2 block space-y-1">
          <span className="text-[10px] text-zinc-400">Type <strong className="font-mono text-zinc-200">{project.name}</strong> to confirm</span>
          <input
            className="input-field py-1.5 text-xs"
            value={deleteConfirmation}
            onChange={(event) => setDeleteConfirmation(event.target.value)}
            disabled={deleting}
            autoComplete="off"
          />
        </label>
        <button
          type="button"
          className="btn mt-2 w-full border border-red-400/25 bg-red-500/10 text-red-200 hover:bg-red-500/20"
          onClick={handleDelete}
          disabled={deleting || deleteConfirmation !== project.name}
        >
          {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
          {deleting ? 'Deleting…' : 'Permanently delete'}
        </button>
        {deleteError && <p className="mt-2 text-red-300">{deleteError}</p>}
      </details>
    </div>
  );
}

function DeploymentReadinessPanel({ workspaceId, projectId }: { workspaceId: string; projectId: string }) {
  const { getToken } = useAuth();
  const [report, setReport] = useState<DeploymentReadinessReport | null>(null);
  const [generated, setGenerated] = useState<GeneratedDeploymentFiles | null>(null);
  const [running, setRunning] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState('');

  async function scan() {
    setRunning(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again to scan this repository.');
      setReport(await scanDeploymentReadiness(token, workspaceId, projectId));
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Readiness scan failed.');
    } finally {
      setRunning(false);
    }
  }

  async function generate() {
    setGenerating(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again to generate deployment files.');
      setGenerated(await generateDeploymentFiles(token, workspaceId, projectId));
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Deployment files could not be generated.');
    } finally {
      setGenerating(false);
    }
  }

  async function copyBundle() {
    if (!generated) return;
    await navigator.clipboard.writeText(`--- Dockerfile ---\n${generated.dockerfile}\n--- compose.yaml ---\n${generated.compose_yaml}\n--- .env.example ---\n${generated.env_example}`);
  }

  return (
    <details className="rounded-lg border border-cyan-400/20 bg-cyan-500/5 p-3 mb-5">
      <summary className="flex items-center gap-2 text-xs font-semibold text-cyan-100 cursor-pointer list-none">
        <Container size={14} /> Deployment readiness
        <span className="ml-auto text-[10px] uppercase tracking-wide text-zinc-500">Pinned repo analysis</span>
      </summary>
      <div className="mt-3 space-y-3">
        <button type="button" className="btn btn-secondary w-full" onClick={scan} disabled={running}>
          {running ? <Loader2 size={14} className="animate-spin" /> : <Search size={14} />}
          {running ? 'Scanning repository…' : 'Run readiness scan'}
        </button>
        {error && <p className="text-xs text-red-300">{error}</p>}
        {report && (
          <div className="space-y-3 text-xs">
            <div className="flex items-center justify-between rounded bg-black/20 p-2">
              <span className="text-zinc-300">{report.stack.framework} · {report.stack.root}</span>
              <span className={`badge ${report.ready ? 'badge-success' : 'text-amber-300 bg-amber-500/10'}`}>{report.score}/100</span>
            </div>
            {report.findings.length === 0 ? (
              <p className="text-emerald-300">No common deployment blockers detected.</p>
            ) : (
              <div className="space-y-1.5">
                {report.findings.map((finding) => (
                  <div key={finding.code} className="rounded border border-white/5 bg-black/15 p-2">
                    <div className="flex items-center gap-2">
                      <span className={finding.severity === 'blocker' ? 'text-red-300' : 'text-amber-300'}>{finding.severity.toUpperCase()}</span>
                      <span className="text-zinc-200">{finding.title}</span>
                    </div>
                    <p className="mt-1 text-zinc-500">{finding.detail} {finding.remediation}</p>
                    {finding.citations.length > 0 && <p className="mt-1 font-mono text-[9px] text-cyan-400">{finding.citations.join(', ')}</p>}
                  </div>
                ))}
              </div>
            )}
            <div className="flex flex-wrap gap-1">
              {report.provider_recommendations.map((item) => <span key={item.provider} className="badge badge-neutral text-[9px]" title={item.reason}>{item.provider}: {item.fit} fit</span>)}
            </div>
            <button type="button" className="btn btn-primary w-full" onClick={generate} disabled={generating || report.stack.kind === 'unknown'}>
              {generating ? <Loader2 size={14} className="animate-spin" /> : <FileCode2 size={14} />}
              {generating ? 'Generating reviewed drafts…' : 'Generate Docker deployment drafts'}
            </button>
          </div>
        )}
        {generated && (
          <div className="space-y-2 text-[10px]">
            <div className="flex items-center justify-between">
              <span className="text-zinc-400">Generated from {generated.commit_sha.slice(0, 8)}; secrets omitted</span>
              <button type="button" className="text-indigo-300 hover:text-indigo-200 flex items-center gap-1" onClick={copyBundle}><Clipboard size={11} /> Copy all</button>
            </div>
            <details className="rounded bg-black/30 p-2"><summary className="cursor-pointer text-zinc-300">Dockerfile</summary><pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap text-zinc-400">{generated.dockerfile}</pre></details>
            <details className="rounded bg-black/30 p-2"><summary className="cursor-pointer text-zinc-300">compose.yaml</summary><pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap text-zinc-400">{generated.compose_yaml}</pre></details>
            <details className="rounded bg-black/30 p-2"><summary className="cursor-pointer text-zinc-300">.env.example</summary><pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap text-zinc-400">{generated.env_example}</pre></details>
          </div>
        )}
      </div>
    </details>
  );
}

function ProjectQAPanel({ workspaceId, projectId }: { workspaceId: string; projectId: string }) {
  const { getToken } = useAuth();
  const [question, setQuestion] = useState('');
  const [answer, setAnswer] = useState<ProjectAnswer | null>(null);
  const [asking, setAsking] = useState(false);
  const [error, setError] = useState('');

  async function handleAsk(event: React.FormEvent) {
    event.preventDefault();
    const nextQuestion = question.trim();
    if (!nextQuestion || asking) return;

    setAsking(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session is not available. Please sign in again.');
      const response = await queryProject(token, workspaceId, projectId, nextQuestion);
      setAnswer(response);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Project query failed.');
    } finally {
      setAsking(false);
    }
  }

  return (
    <div className="rounded-lg border border-indigo-400/20 bg-indigo-500/5 p-3 mb-5">
      <div className="flex items-center gap-2 text-xs font-semibold text-indigo-200 mb-2">
        <ShieldCheck size={14} /> Ask indexed project context
      </div>
      <form onSubmit={handleAsk} className="flex gap-2">
        <input
          className="input-field min-w-0 py-2 text-xs"
          value={question}
          onChange={(event) => setQuestion(event.target.value)}
          placeholder="How is this service deployed?"
          maxLength={4000}
          disabled={asking}
          aria-label="Project question"
        />
        <button
          type="submit"
          className="btn btn-primary px-3"
          disabled={asking || !question.trim()}
          aria-label="Ask project question"
        >
          {asking ? <Loader2 size={14} className="animate-spin" /> : <Send size={14} />}
        </button>
      </form>

      {error && (
        <p className="mt-2 text-xs text-red-300 flex items-start gap-1.5">
          <AlertCircle size={13} className="shrink-0 mt-0.5" /> {error}
        </p>
      )}

      {answer && (
        <div className="mt-3 pt-3 border-t border-indigo-300/10">
          <div className="flex items-center justify-between mb-2">
            <span className="text-[11px] uppercase tracking-wide text-zinc-500">Grounded answer</span>
            <span className={`badge text-[10px] ${answer.enough_context ? 'badge-success' : 'badge-neutral'}`}>
              {answer.confidence} confidence
            </span>
          </div>
          <p className="text-xs leading-5 text-zinc-300 whitespace-pre-wrap">{answer.answer}</p>
          {answer.sources.length > 0 && (
            <div className="mt-3 space-y-1.5">
              <span className="text-[11px] text-zinc-500">Evidence</span>
              {answer.sources.slice(0, 5).map((source) => (
                <div key={source.id} className="flex items-center justify-between gap-2 text-[11px] text-zinc-400 bg-black/20 rounded px-2 py-1.5">
                  <span className="flex items-center gap-1.5 min-w-0">
                    <FileCode2 size={12} className="shrink-0 text-indigo-300" />
                    <span className="truncate" title={source.citation}>{source.citation}</span>
                  </span>
                  <span className="font-mono text-zinc-600">{Math.round(source.score * 100)}%</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function LogAnalysisPanel({ workspaceId, projectId, logProviders }: { workspaceId: string; projectId: string; logProviders: LogProviderConnection[] }) {
  const { getToken } = useAuth();
  const [logs, setLogs] = useState('');
  const [source, setSource] = useState('manual');
  const [logKind, setLogKind] = useState<'runtime' | 'build'>('runtime');
  const [result, setResult] = useState<IncidentAnalysisResponse | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [proposal, setProposal] = useState<FixProposalResponse | null>(null);
  const [proposing, setProposing] = useState(false);
  const [approvalConfirmed, setApprovalConfirmed] = useState(false);
  const [deciding, setDeciding] = useState(false);
  const [decisionResult, setDecisionResult] = useState<FixApprovalDecisionResponse | null>(null);
  const [error, setError] = useState('');

  async function handleAnalyze(event: React.FormEvent) {
    event.preventDefault();
    if ((source === 'manual' && !logs.trim()) || analyzing) return;
    setAnalyzing(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session is not available. Please sign in again.');
      const response = source === 'manual'
        ? await analyzeProjectLogs(token, workspaceId, projectId, logs, 'manual')
        : await collectAndAnalyzeProjectLogs(token, workspaceId, projectId, source, logKind);
      setResult(response);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Log analysis failed.');
    } finally {
      setAnalyzing(false);
    }
  }

  async function handleProposeFix() {
    if (!result || proposing) return;
    setProposing(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session is not available. Please sign in again.');
      const response = await proposeIncidentFix(token, workspaceId, projectId, result.incident_id);
      setProposal(response);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'A safe fix proposal could not be generated.');
    } finally {
      setProposing(false);
    }
  }

  async function handleDecision(decision: 'approve' | 'reject') {
    if (!proposal || deciding || (decision === 'approve' && !approvalConfirmed)) return;
    setDeciding(true);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session is not available. Please sign in again.');
      const response = await decideFixApproval(
        token,
        workspaceId,
        projectId,
        proposal.approval_id,
        decision,
        decision === 'approve' ? 'Reviewed in OpsPilot incident cockpit' : 'Rejected from OpsPilot incident cockpit'
      );
      setDecisionResult(response);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Approval decision failed.');
    } finally {
      setDeciding(false);
    }
  }

  return (
    <details className="rounded-lg border border-amber-400/20 bg-amber-500/5 p-3 mb-5 group/incident">
      <summary className="flex items-center gap-2 text-xs font-semibold text-amber-100 cursor-pointer list-none">
        <Terminal size={14} /> Analyze production logs
        <span className="ml-auto text-[10px] uppercase tracking-wide text-zinc-500">Redacted before AI</span>
      </summary>
      <form onSubmit={handleAnalyze} className="mt-3 space-y-2">
        <div className="flex items-center gap-2">
          <label htmlFor={`source-${projectId}`} className="text-[11px] text-zinc-500">Source</label>
          <select
            id={`source-${projectId}`}
            className="input-field py-1 px-2 text-xs bg-zinc-900"
            value={source}
            onChange={(event) => setSource(event.target.value)}
            disabled={analyzing}
          >
            <option value="manual">Manual paste</option>
            {logProviders.map((connection) => (
              <option key={connection.id} value={connection.id}>{connection.display_name} ({connection.provider})</option>
            ))}
          </select>
          {source !== 'manual' && (
            <select className="input-field py-1 px-2 text-xs bg-zinc-900" value={logKind} onChange={(event) => setLogKind(event.target.value as 'runtime' | 'build')} disabled={analyzing}>
              <option value="runtime">Runtime logs</option>
              <option value="build">Build logs</option>
            </select>
          )}
        </div>
        {source === 'manual' ? (
          <textarea
            className="input-field min-h-28 resize-y text-xs font-mono leading-5"
            value={logs}
            onChange={(event) => setLogs(event.target.value)}
            placeholder="Paste a stack trace or deployment logs (maximum 1 MiB)..."
            maxLength={1_048_576}
            disabled={analyzing}
            aria-label="Logs to analyze"
          />
        ) : (
          <p className="rounded bg-black/20 p-2 text-[11px] text-zinc-400">OpsPilot will fetch up to 300 lines server-to-server, redact secrets, and create an incident. Raw provider logs are not sent back to this browser.</p>
        )}
        <button type="submit" className="btn btn-primary w-full" disabled={analyzing || (source === 'manual' && !logs.trim())}>
          {analyzing ? <Loader2 size={14} className="animate-spin" /> : <Search size={14} />}
          {analyzing ? 'Building incident evidence...' : source === 'manual' ? 'Analyze & create incident' : 'Collect, redact & analyze'}
        </button>
      </form>

      {error && (
        <p className="mt-2 text-xs text-red-300 flex items-start gap-1.5">
          <AlertCircle size={13} className="shrink-0 mt-0.5" /> {error}
        </p>
      )}

      {result && (
        <div className="mt-3 pt-3 border-t border-amber-300/10 space-y-3 text-xs">
          <div className="flex items-center justify-between gap-2">
            <span className="font-semibold text-white truncate" title={result.analysis.title}>{result.analysis.title}</span>
            <span className={`badge text-[10px] ${result.severity === 'critical' ? 'text-red-300 bg-red-500/10' : 'badge-neutral'}`}>
              {result.severity}
            </span>
          </div>
          <div>
            <p className="text-[10px] uppercase tracking-wide text-zinc-500 mb-1">Probable root cause</p>
            <p className="text-zinc-300 leading-5 whitespace-pre-wrap">{result.analysis.root_cause}</p>
          </div>
          <div>
            <p className="text-[10px] uppercase tracking-wide text-zinc-500 mb-1">Recommended next step</p>
            <p className="text-zinc-300 leading-5 whitespace-pre-wrap">{result.analysis.fix_recommendation}</p>
          </div>
          <div className="flex flex-wrap gap-2 text-[10px] text-zinc-500">
            <span>Confidence: {result.analysis.confidence}</span>
            <span>•</span>
            <span>{result.redactions} secret value{result.redactions === 1 ? '' : 's'} redacted</span>
            <span>•</span>
            <span className="font-mono">INC {result.incident_id.slice(0, 8)}</span>
          </div>
          {result.evidence.length > 0 && (
            <div className="space-y-1">
              {result.evidence.slice(0, 5).map((item) => (
                <div key={item.id} className="flex items-center justify-between bg-black/20 rounded px-2 py-1.5 text-[11px] text-zinc-400">
                  <span className="truncate" title={item.citation}>{item.citation}</span>
                  <span className="font-mono text-zinc-600">{Math.round(item.score * 100)}%</span>
                </div>
              ))}
            </div>
          )}
          {!proposal && result.analysis.confidence !== 'insufficient' && (
            <button className="btn btn-secondary w-full" onClick={handleProposeFix} disabled={proposing}>
              {proposing ? <Loader2 size={14} className="animate-spin" /> : <GitPullRequest size={14} />}
              {proposing ? 'Generating bounded diff...' : 'Propose code fix'}
            </button>
          )}
          {proposal && (
            <div className="rounded-lg border border-emerald-400/20 bg-emerald-500/5 p-3 space-y-2">
              <div className="flex items-center justify-between gap-2">
                <span className="flex items-center gap-1.5 font-semibold text-emerald-200">
                  <LockKeyhole size={13} /> Approval required
                </span>
                <span className="badge badge-neutral text-[10px]">No GitHub write yet</span>
              </div>
              <p className="text-zinc-400 leading-5">
                The diff is pinned to <span className="font-mono text-zinc-300">{proposal.base_commit_sha.slice(0, 8)}</span> and passed path, secret, and static checks.
              </p>
              <pre className="max-h-56 overflow-auto rounded bg-black/40 p-2 text-[10px] leading-4 text-zinc-300 whitespace-pre-wrap">
                {proposal.unified_diff}
              </pre>
              <p className="text-[10px] text-amber-300">
                Tests: {proposal.verification.tests.status.replace('_', ' ')} — {proposal.verification.tests.reason}. Approval can publish only a draft PR.
              </p>
              {!decisionResult && (
                <>
                  <label className="flex items-start gap-2 text-[11px] text-zinc-300 cursor-pointer">
                    <input
                      type="checkbox"
                      className="mt-0.5 accent-indigo-500"
                      checked={approvalConfirmed}
                      onChange={(event) => setApprovalConfirmed(event.target.checked)}
                      disabled={deciding}
                    />
                    <span>I reviewed the exact diff and approve creating a new <span className="font-mono">opspilot/fix-*</span> branch and draft PR. The base branch will not be modified.</span>
                  </label>
                  <div className="grid grid-cols-2 gap-2">
                    <button className="btn btn-secondary" onClick={() => handleDecision('reject')} disabled={deciding}>
                      Reject
                    </button>
                    <button className="btn btn-primary" onClick={() => handleDecision('approve')} disabled={deciding || !approvalConfirmed}>
                      {deciding ? <Loader2 size={14} className="animate-spin" /> : <GitPullRequest size={14} />}
                      Approve draft PR
                    </button>
                  </div>
                </>
              )}
              {decisionResult?.approval_status === 'executed' && (
                <a
                  className="btn btn-primary w-full"
                  href={decisionResult.pull_request_url}
                  target="_blank"
                  rel="noreferrer"
                >
                  <ExternalLink size={14} /> Open draft PR #{decisionResult.pull_request_number}
                </a>
              )}
              {decisionResult?.approval_status === 'rejected' && (
                <p className="rounded bg-zinc-900/60 p-2 text-center text-zinc-400">Proposal rejected. No GitHub write occurred.</p>
              )}
            </div>
          )}
        </div>
      )}
      <IncidentHistory
        workspaceId={workspaceId}
        projectId={projectId}
        refreshKey={`${result?.incident_id || ''}:${proposal?.proposal_id || ''}:${decisionResult?.approval_status || ''}`}
      />
    </details>
  );
}

function IncidentHistory({ workspaceId, projectId, refreshKey }: { workspaceId: string; projectId: string; refreshKey: string }) {
  const { getToken } = useAuth();
  const [incidents, setIncidents] = useState<IncidentHistoryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [confirmedApproval, setConfirmedApproval] = useState('');
  const [decidingApproval, setDecidingApproval] = useState('');

  const loadHistory = useCallback(async () => {
    try {
      const token = await getToken();
      if (!token) return;
      const response = await getIncidentHistory(token, workspaceId, projectId);
      setIncidents(response.incidents || []);
      setError('');
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Incident history could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, [getToken, workspaceId, projectId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadHistory();
  }, [loadHistory, refreshKey]);

  async function decide(item: IncidentHistoryItem, decision: 'approve' | 'reject') {
    const approval = item.fix_proposal?.approval;
    if (!approval || decidingApproval || (decision === 'approve' && confirmedApproval !== approval.approval_id)) return;
    setDecidingApproval(approval.approval_id);
    setError('');
    try {
      const token = await getToken();
      if (!token) throw new Error('Sign in again to decide this approval.');
      await decideFixApproval(
        token, workspaceId, projectId, approval.approval_id, decision,
        decision === 'approve' ? 'Reviewed exact persisted diff in incident history' : 'Rejected from incident history'
      );
      setConfirmedApproval('');
      await loadHistory();
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Approval decision failed.');
    } finally {
      setDecidingApproval('');
    }
  }

  return (
    <div className="mt-4 pt-3 border-t border-amber-300/10">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[11px] font-semibold text-zinc-300">Incident timeline</span>
        <button type="button" className="text-[10px] text-indigo-300 hover:text-indigo-200" onClick={loadHistory}>Refresh</button>
      </div>
      {loading && <p className="text-[11px] text-zinc-500">Loading durable incident state…</p>}
      {error && <p className="text-[11px] text-red-300 mb-2">{error}</p>}
      {!loading && incidents.length === 0 && <p className="text-[11px] text-zinc-500">No incidents yet.</p>}
      <div className="space-y-2">
        {incidents.slice(0, 8).map((item) => {
          const proposal = item.fix_proposal;
          const approval = proposal?.approval;
          const pending = approval?.status === 'pending';
          const prURL = proposal?.publish_result?.pr_url;
          return (
            <details key={item.incident_id} className="rounded border border-white/10 bg-black/15 p-2 text-[11px]">
              <summary className="cursor-pointer list-none flex items-center gap-2">
                <span className={`w-1.5 h-1.5 rounded-full ${item.severity === 'critical' ? 'bg-red-400' : item.severity === 'error' ? 'bg-amber-400' : 'bg-zinc-500'}`} />
                <span className="truncate text-zinc-200" title={item.title}>{item.title}</span>
                <span className="ml-auto text-zinc-600">{item.source} · {new Date(item.created_at).toLocaleString()}</span>
              </summary>
              <div className="mt-2 space-y-2 border-t border-white/5 pt-2">
                <p className="text-zinc-400 whitespace-pre-wrap">{item.root_cause}</p>
                <div className="flex flex-wrap gap-1">
                  {item.analysis_workflow.steps.map((step) => (
                    <span key={`${item.incident_id}-${step.order}`} className="badge badge-neutral text-[9px]">{step.name}: {step.status}</span>
                  ))}
                </div>
                {proposal && (
                  <div className="rounded border border-emerald-400/15 bg-emerald-500/5 p-2 space-y-2">
                    <div className="flex items-center justify-between">
                      <span className="font-semibold text-emerald-200">Fix {proposal.status.replaceAll('_', ' ')}</span>
                      <span className="font-mono text-zinc-500">{proposal.base_commit_sha.slice(0, 8)}</span>
                    </div>
                    <pre className="max-h-44 overflow-auto rounded bg-black/40 p-2 text-[9px] leading-4 text-zinc-300 whitespace-pre-wrap">{proposal.unified_diff}</pre>
                    {pending && approval && (
                      <>
                        <label className="flex items-start gap-2 text-zinc-300">
                          <input type="checkbox" className="mt-0.5 accent-indigo-500" checked={confirmedApproval === approval.approval_id} onChange={(event) => setConfirmedApproval(event.target.checked ? approval.approval_id : '')} />
                          <span>I reviewed this exact persisted diff and approve a new draft PR branch only.</span>
                        </label>
                        <div className="grid grid-cols-2 gap-2">
                          <button type="button" className="btn btn-secondary" disabled={!!decidingApproval} onClick={() => decide(item, 'reject')}>Reject</button>
                          <button type="button" className="btn btn-primary" disabled={!!decidingApproval || confirmedApproval !== approval.approval_id} onClick={() => decide(item, 'approve')}>
                            {decidingApproval === approval.approval_id ? <Loader2 size={12} className="animate-spin" /> : <GitPullRequest size={12} />} Approve draft PR
                          </button>
                        </div>
                      </>
                    )}
                    {prURL && (
                      <a className="btn btn-primary w-full" href={prURL} target="_blank" rel="noreferrer"><ExternalLink size={12} /> Open draft PR #{proposal.publish_result.pr_number}</a>
                    )}
                    {approval && !pending && !prURL && <p className="text-zinc-500">Approval: {approval.status}</p>}
                  </div>
                )}
              </div>
            </details>
          );
        })}
      </div>
    </div>
  );
}
