'use client';

import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '@clerk/nextjs';
import Link from 'next/link';
import { Plus, Building2, ArrowRight, Loader2, GitBranch, ExternalLink, FolderGit2 } from 'lucide-react';
import {
  getWorkspaces,
  createWorkspace,
  listConnectedRepositories,
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
        <Loader2 className="animate-spin text-indigo-500" size={32} />
      </div>
    );
  }

  return (
    <div className="max-w-6xl mx-auto">
      <div className="flex-between mb-8 animate-fade-in">
        <div>
          <h1 className="text-3xl font-bold font-display text-white mb-2">Workspaces</h1>
          <p className="text-zinc-400">Manage your organizations and projects.</p>
        </div>
      </div>

      {error && (
        <div role="alert" className="mb-6 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border border-red-500/25 bg-red-500/10 p-4 text-sm text-red-300">
          <span>{error}</span>
          <button type="button" onClick={fetchWorkspaces} className="btn btn-secondary self-start sm:self-auto">
            Retry
          </button>
        </div>
      )}

      <ConnectedRepositories />

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6 animate-fade-in animate-delay-1">
        {/* Create New Workspace Card */}
        <div className="glass-card p-6 flex flex-col justify-between border-dashed border-2 border-[var(--border-medium)] bg-[var(--bg-glass)] hover:bg-[#ffffff05]">
          <div>
            <div className="w-10 h-10 rounded-full bg-indigo-500/20 flex-center mb-4">
              <Plus size={20} className="text-indigo-400" />
            </div>
            <h3 className="text-lg font-medium text-white mb-2">Create Workspace</h3>
            <p className="text-sm text-zinc-400 mb-6">Start a new isolated environment for your team&apos;s projects.</p>
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
              <div className="w-10 h-10 rounded-lg bg-pink-500/20 flex-center">
                <Building2 size={20} className="text-pink-400" />
              </div>
              <span className="badge badge-neutral">{ws.plan.toUpperCase()}</span>
            </div>

            <h3 className="text-xl font-medium text-white mb-1 group-hover:text-indigo-400 transition-colors">
              {ws.name}
            </h3>
            <p className="text-sm text-zinc-500 mb-6 font-mono">{ws.slug}</p>

            <div className="mt-auto pt-4 border-t border-[var(--border-light)] flex-between text-sm">
              <span className="text-zinc-400">Role: <span className="text-white capitalize">{ws.role}</span></span>
              <ArrowRight size={16} className="text-zinc-500 group-hover:text-indigo-400 group-hover:translate-x-1 transition-all" />
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
      setError(requestError instanceof Error ? requestError.message : 'Repositories could not be loaded.');
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
          <div className="w-9 h-9 rounded-lg bg-indigo-500/20 flex-center">
            <FolderGit2 size={18} className="text-indigo-400" />
          </div>
          <div>
            <h2 className="text-lg font-medium text-white">Connected Repositories</h2>
            <p className="text-xs text-zinc-500">Every repository linked across your workspaces.</p>
          </div>
        </div>
        {!loading && (
          <span className="badge badge-neutral text-xs">{connections.length}</span>
        )}
      </div>

      {loading ? (
        <div className="flex items-center gap-2 text-sm text-zinc-500 py-4">
          <Loader2 size={14} className="animate-spin" /> Loading repositories…
        </div>
      ) : error ? (
        <div role="alert" className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border border-red-500/25 bg-red-500/10 p-4 text-sm text-red-300">
          <span>{error}</span>
          <button type="button" onClick={fetchConnections} className="btn btn-secondary self-start sm:self-auto">
            Retry
          </button>
        </div>
      ) : connections.length === 0 ? (
        <p className="text-sm text-zinc-500 py-2">
          No repositories connected yet. Open a workspace and use{' '}
          <span className="text-zinc-300">Connect Repository</span> to link one.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {connections.map((summary) => {
            const conn = summary.repository_connection;
            const badge = ingestionBadge(summary.latest_ingestion_job?.status);
            return (
              <li
                key={conn.id}
                className="flex flex-wrap items-center gap-3 p-3 rounded-lg border border-[var(--border-light)] hover:bg-[#ffffff05] transition-colors"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 min-w-0">
                    <a
                      href={conn.html_url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-sm text-indigo-400 hover:underline truncate flex items-center gap-1"
                    >
                      {conn.full_name} <ExternalLink size={12} className="shrink-0" />
                    </a>
                  </div>
                  <div className="text-[10px] text-zinc-500 truncate">
                    {summary.workspace_name} / {summary.project_name}
                  </div>
                </div>

                <span className="flex items-center gap-1 text-[10px] text-zinc-400 font-mono">
                  <GitBranch size={11} /> {conn.selected_branch}
                </span>

                {conn.last_indexed_commit_sha ? (
                  <span className="hidden sm:inline text-[10px] text-zinc-500 font-mono">
                    {conn.last_indexed_commit_sha.substring(0, 7)}
                  </span>
                ) : (
                  <span className="hidden sm:inline text-[10px] text-zinc-600">Never indexed</span>
                )}

                <span className={`badge ${badge.className} text-[10px]`}>{badge.label}</span>

                <Link
                  href={`/dashboard/${conn.workspace_id}`}
                  className="text-zinc-600 hover:text-white transition-colors"
                  aria-label={`Open ${summary.workspace_name}`}
                >
                  <ArrowRight size={16} />
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
