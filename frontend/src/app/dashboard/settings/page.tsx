'use client';

import { useState, useEffect, useCallback } from 'react';
import { useAuth, useUser } from '@clerk/nextjs';
import Link from 'next/link';
import { Building2, ArrowRight, Loader2, ShieldCheck, Clock, AlertCircle } from 'lucide-react';
import {
  getWorkspaces,
  getGitHubIntegrationConfig,
  Workspace,
  GitHubIntegrationConfig,
} from '@/lib/api';

export default function SettingsPage() {
  const { getToken } = useAuth();
  const { user } = useUser();

  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [githubConfig, setGithubConfig] = useState<GitHubIntegrationConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setError(null);
    try {
      const token = await getToken();
      if (!token) throw new Error('Your session has expired. Sign in again.');

      const [wsRes, configRes] = await Promise.all([
        getWorkspaces(token),
        getGitHubIntegrationConfig(token).catch((e) => {
          console.error(e);
          return null;
        }),
      ]);

      setWorkspaces(wsRes.workspaces || []);
      setGithubConfig(configRes);
    } catch (requestError) {
      console.error('Error fetching settings:', requestError);
      setError(requestError instanceof Error ? requestError.message : 'Settings could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, [getToken]);

  useEffect(() => {
    // Remote bootstrap is effect-driven because Clerk's session token is only
    // available in this interactive client boundary.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchData();
  }, [fetchData]);

  if (loading) {
    return (
      <div className="flex-center h-full">
        <Loader2 className="animate-spin text-indigo-500" size={32} />
      </div>
    );
  }

  return (
    <div className="max-w-4xl mx-auto">
      <div className="mb-8 animate-fade-in">
        <h1 className="text-3xl font-bold font-display text-white mb-2">Settings</h1>
        <p className="text-zinc-400">Account details, integration status, and per-workspace configuration.</p>
      </div>

      {error && (
        <div className="glass-card p-4 mb-6 border border-red-500/20 bg-red-500/10 text-sm text-red-300 flex items-start gap-2">
          <AlertCircle size={16} className="shrink-0 mt-0.5" />
          <span>{error}</span>
        </div>
      )}

      <div className="glass-card p-6 mb-6 animate-fade-in">
        <h2 className="text-lg font-medium text-white mb-4">Account</h2>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-zinc-500 text-xs mb-1">Name</dt>
            <dd className="text-zinc-200">{user?.fullName || '—'}</dd>
          </div>
          <div>
            <dt className="text-zinc-500 text-xs mb-1">Email</dt>
            <dd className="text-zinc-200 font-mono text-xs">
              {user?.primaryEmailAddress?.emailAddress || '—'}
            </dd>
          </div>
        </dl>
      </div>

      <div className="glass-card p-6 mb-6 animate-fade-in animate-delay-1">
        <h2 className="text-lg font-medium text-white mb-4">GitHub App</h2>
        {githubConfig?.configured ? (
          <div className="flex items-start gap-2 text-sm text-emerald-300">
            <ShieldCheck size={16} className="shrink-0 mt-0.5" />
            <span>
              Configured on the backend. Install it per workspace from that workspace&apos;s{' '}
              <span className="text-zinc-300">Connect Repository</span> card.
            </span>
          </div>
        ) : (
          <div className="flex items-start gap-2 text-sm text-red-400">
            <AlertCircle size={16} className="shrink-0 mt-0.5" />
            <span>
              GitHub App is not configured on the backend. Set <code className="font-mono text-xs">GITHUB_APP_ID</code>{' '}
              and the private key before connecting repositories.
            </span>
          </div>
        )}
      </div>

      <div className="glass-card p-6 animate-fade-in animate-delay-2">
        <h2 className="text-lg font-medium text-white mb-1">Workspace settings</h2>
        <p className="text-xs text-zinc-500 mb-4">
          Log retention, usage limits, and repository connections are configured per workspace.
        </p>

        {workspaces.length === 0 ? (
          <p className="text-sm text-zinc-500">
            No workspaces yet.{' '}
            <Link href="/dashboard" className="text-indigo-400 hover:text-indigo-300">
              Create one
            </Link>
            .
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {workspaces.map((workspace) => (
              <li key={workspace.id}>
                <Link
                  href={`/dashboard/${workspace.id}`}
                  className="flex items-center gap-3 p-3 rounded-lg border border-[var(--border-light)] hover:bg-[#ffffff05] transition-colors group"
                >
                  <div className="w-8 h-8 rounded bg-indigo-500/20 flex-center shrink-0">
                    <Building2 size={16} className="text-indigo-400" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm text-white truncate">{workspace.name}</div>
                    <div className="text-[10px] text-zinc-500 font-mono truncate">{workspace.slug}</div>
                  </div>
                  <span className="badge badge-neutral text-[10px]">{workspace.plan.toUpperCase()}</span>
                  <span className="hidden sm:flex items-center gap-1 text-[10px] text-zinc-500">
                    <Clock size={11} /> {workspace.log_retention_days || 30}d
                  </span>
                  <ArrowRight size={16} className="text-zinc-600 group-hover:text-white transition-colors shrink-0" />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
