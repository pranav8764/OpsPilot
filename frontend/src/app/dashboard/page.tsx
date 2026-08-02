'use client';

import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '@clerk/nextjs';
import Link from 'next/link';
import { Plus, Building2, ArrowRight, Loader2, GitBranch, ExternalLink, FolderGit2, Unplug } from 'lucide-react';
import {
  getWorkspaces,
  createWorkspace,
  listConnectedRepositories,
  isRevokedConnection,
  describeRequestError,
  Workspace,
  RepositoryConnectionSummary,
} from '@/lib/api';
import { ingestionBadge } from '@/lib/ingestion';

export default function WorkspacesPage() {
  const { getToken } = useAuth();
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loading, setLoading] = useState(true);
  const [isCreating, setIsCreating] = useState(false);
  const [newWorkspaceName, setNewWorkspaceName] = useState('');
  const [error, setError] = useState<string | null>(null);

  const fetchWorkspaces = useCallback(async () => {
    setError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session has expired. Sign in again.');
      const res = await getWorkspaces(token);
      setWorkspaces(res.workspaces || []);
    } catch (error) {
      console.error('Error fetching workspaces:', error);
      setError(error instanceof Error ? error.message : 'Workspaces could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, [getToken]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchWorkspaces();
  }, [fetchWorkspaces]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!newWorkspaceName.trim()) return;

    setIsCreating(true);
    setError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session has expired. Sign in again.');

      const res = await createWorkspace(token, newWorkspaceName);
      setWorkspaces((current) => [res.workspace, ...current]);
      setNewWorkspaceName('');
    } catch (error) {
      console.error('Error creating workspace:', error);
      setError(error instanceof Error ? error.message : 'Workspace could not be created.');
    } finally {
      setIsCreating(false);
    }
  }

  if (loading) {
    return (
      <div className="flex-center h-full">
        <Loader2 className="animate-spin text-[var(--accent-primary)]" size={32} />
      </div>
    );
  }

  return (
    <div className="max-w-6xl mx-auto">
      <div className="flex-between mb-8 animate-fade-in">
        <div>
          <h1 className="text-3xl font-bold font-display text-[var(--text-primary)] mb-2">Workspaces</h1>
          <p className="text-[var(--text-secondary)]">Manage your organizations and projects.</p>
        </div>
      </div>

      {error && (
        <div role="alert" className="mb-6 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border border-[var(--error)]/25 bg-[var(--error)]/10 p-4 text-sm text-[var(--error)]">
          <span>{error}</span>
          <button type="button" onClick={fetchWorkspaces} className="btn btn-secondary self-start sm:self-auto">
            Retry
          </button>
        </div>
      )}

      <ConnectedRepositories />

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6 animate-fade-in animate-delay-1">
        {/* Create New Workspace Card */}
        <div className="glass-card p-6 flex flex-col justify-between border-dashed border-2 border-[var(--border-medium)] bg-[var(--bg-glass)] hover:bg-[var(--bg-tertiary)]">
          <div>
            <div className="w-10 h-10 rounded-full bg-[var(--accent-primary)]/20 flex-center mb-4">
              <Plus size={20} className="text-[var(--accent-primary)]" />
            </div>
            <h3 className="text-lg font-medium text-[var(--text-primary)] mb-2">Create Workspace</h3>
            <p className="text-sm text-[var(--text-secondary)] mb-6">Start a new isolated environment for your team&apos;s projects.</p>
          </div>

          <form onSubmit={handleCreate} className="flex flex-col gap-3">
            <input
              type="text"
              aria-label="Workspace name"
              placeholder="Workspace Name"
              className="input-field"
              value={newWorkspaceName}
              onChange={(e) => setNewWorkspaceName(e.target.value)}
              disabled={isCreating}
            />
            <button
              type="submit"
              className="btn btn-primary w-full"
              disabled={isCreating || !newWorkspaceName.trim()}
            >
              {isCreating ? <Loader2 size={16} className="animate-spin" /> : 'Create'}
            </button>
          </form>
        </div>

        {/* Workspace List */}
        {workspaces.map((ws) => (
          <Link href={`/dashboard/${ws.id}`} key={ws.id} className="glass-card p-6 flex flex-col group">
            <div className="flex-between mb-4">
              <div className="w-10 h-10 rounded-lg bg-[var(--accent-primary)]/20 flex-center">
                <Building2 size={20} className="text-[var(--accent-primary)]" />
              </div>
              <span className="badge badge-neutral">{ws.plan.toUpperCase()}</span>
            </div>

            <h3 className="text-xl font-medium text-[var(--text-primary)] mb-1 group-hover:text-[var(--accent-primary)] transition-colors">
              {ws.name}
            </h3>
            <p className="text-sm text-[var(--text-muted)] mb-6 font-mono">{ws.slug}</p>

            <div className="mt-auto pt-4 border-t border-[var(--border-light)] flex-between text-sm">
              <span className="text-[var(--text-secondary)]">Role: <span className="text-[var(--text-primary)] capitalize">{ws.role}</span></span>
              <ArrowRight size={16} className="text-[var(--text-muted)] group-hover:text-[var(--accent-primary)] group-hover:translate-x-1 transition-all" />
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

function ConnectedRepositories() {
  const { getToken } = useAuth();
  const [connections, setConnections] = useState<RepositoryConnectionSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchConnections = useCallback(async () => {
    setError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session has expired. Sign in again.');
      const res = await listConnectedRepositories(token);
      setConnections(res.connections || []);
    } catch (requestError) {
      console.error('Error fetching connected repositories:', requestError);
      setError(describeRequestError(requestError, 'Repositories could not be loaded.'));
    } finally {
      setLoading(false);
    }
  }, [getToken]);

  useEffect(() => {
    // Deferred so the state update lands outside the effect body, matching the
    // pattern the rest of this app uses to satisfy react-hooks/set-state-in-effect.
    const timer = setTimeout(fetchConnections, 0);
    return () => clearTimeout(timer);
  }, [fetchConnections]);

  return (
    <section className="glass-card p-6 mb-8 animate-fade-in">
      <div className="flex-between mb-4">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-lg bg-[var(--accent-primary)]/20 flex-center">
            <FolderGit2 size={18} className="text-[var(--accent-primary)]" />
          </div>
          <div>
            <h2 className="text-lg font-medium text-[var(--text-primary)]">Connected Repositories</h2>
            <p className="text-xs text-[var(--text-muted)]">Every repository linked across your workspaces.</p>
          </div>
        </div>
        {!loading && (
          <span className="badge badge-neutral text-xs">{connections.length}</span>
        )}
      </div>

      {loading ? (
        <div className="flex items-center gap-2 text-sm text-[var(--text-muted)] py-4">
          <Loader2 size={14} className="animate-spin" /> Loading repositories…
        </div>
      ) : error ? (
        <div role="alert" className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border border-[var(--error)]/25 bg-[var(--error)]/10 p-4 text-sm text-[var(--error)]">
          <span>{error}</span>
          <button type="button" onClick={fetchConnections} className="btn btn-secondary self-start sm:self-auto">
            Retry
          </button>
        </div>
      ) : connections.length === 0 ? (
        <p className="text-sm text-[var(--text-muted)] py-2">
          No repositories connected yet. Open a workspace and use{' '}
          <span className="text-[var(--text-secondary)]">Connect Repository</span> to link one.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {connections.map((summary) => {
            const conn = summary.repository_connection;
            const badge = ingestionBadge(summary.latest_ingestion_job?.status);
            // Access removed from the GitHub App: say so instead of showing a
            // stale "Indexed" badge for a repository we can no longer read.
            const revoked = isRevokedConnection(conn.status);
            return (
              <li
                key={conn.id}
                className="flex flex-wrap items-center gap-3 p-3 rounded-lg border border-[var(--border-light)] hover:bg-[var(--bg-tertiary)] transition-colors"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 min-w-0">
                    <a
                      href={conn.html_url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-sm text-[var(--accent-primary)] hover:text-[var(--accent-primary-hover)] hover:underline truncate flex items-center gap-1"
                    >
                      {conn.full_name} <ExternalLink size={12} className="shrink-0" />
                    </a>
                  </div>
                  <div className="text-[10px] text-[var(--text-muted)] truncate">
                    {summary.workspace_name} / {summary.project_name}
                  </div>
                </div>

                <span className="flex items-center gap-1 text-[10px] text-[var(--text-secondary)] font-mono">
                  <GitBranch size={11} /> {conn.selected_branch}
                </span>

                {conn.last_indexed_commit_sha ? (
                  <span className="hidden sm:inline text-[10px] text-[var(--text-muted)] font-mono">
                    {conn.last_indexed_commit_sha.substring(0, 7)}
                  </span>
                ) : (
                  <span className="hidden sm:inline text-[10px] text-[var(--text-muted)]">Never indexed</span>
                )}

                {revoked ? (
                  <span className="badge badge-error text-[10px] flex items-center gap-1">
                    <Unplug size={10} /> Reconnect required
                  </span>
                ) : (
                  <span className={`badge ${badge.className} text-[10px]`}>{badge.label}</span>
                )}

                <Link
                  href={`/dashboard/${conn.workspace_id}`}
                  className="btn btn-secondary text-[10px] py-1 px-2"
                  aria-label={revoked ? `Reconnect ${conn.full_name}` : `Open ${summary.workspace_name}`}
                >
                  {revoked ? 'Reconnect' : <ArrowRight size={14} />}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
