'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { UserButton } from '@clerk/nextjs';
import { Eye, Home, Settings, Gauge, CheckCircle2, Flame } from 'lucide-react';
import SyncUser from '@/components/SyncUser';
import { ThemeToggle } from '@/components/theme-toggle';

// Placeholder until real incident data is wired up to this shell — keeps the
// vitals strip functional-looking without inventing a data-fetching layer.
const CRITICAL_INCIDENT_COUNT = 0;

function navLinkClasses(active: boolean) {
  return [
    'flex items-center gap-3 px-3 py-2 rounded-md border-l-[3px] transition-colors',
    active
      ? 'border-[var(--accent-primary)] bg-[var(--bg-tertiary)] text-[var(--accent-primary)]'
      : 'border-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)] hover:text-[var(--text-primary)]',
  ].join(' ');
}

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();

  const isEvalActive = pathname?.startsWith('/dashboard/eval') ?? false;
  const isSettingsActive = pathname?.startsWith('/dashboard/settings') ?? false;
  const isWorkspacesActive =
    !isEvalActive && !isSettingsActive && (pathname?.startsWith('/dashboard') ?? false);

  const isNominal = CRITICAL_INCIDENT_COUNT === 0;

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-primary)]">
      <SyncUser />

      {/* Sidebar */}
      <aside className="w-64 border-r border-[var(--border-light)] bg-[var(--bg-secondary)] flex flex-col">
        <div className="p-6 flex items-center gap-3 border-b border-[var(--border-light)]">
          <div className="brand-medallion">
            <Eye size={18} />
          </div>
          <span className="font-display font-bold text-xl">Archiatros</span>
        </div>

        <nav className="flex-1 p-4 flex flex-col gap-2">
          <Link href="/dashboard" className={navLinkClasses(isWorkspacesActive)}>
            <Home size={18} />
            <span className="text-sm font-medium">Workspaces</span>
          </Link>
          <Link href="/dashboard/eval" className={navLinkClasses(isEvalActive)}>
            <Gauge size={18} />
            <span className="text-sm font-medium">Offline Eval</span>
          </Link>
          <Link href="/dashboard/settings" className={`${navLinkClasses(isSettingsActive)} mt-auto`}>
            <Settings size={18} />
            <span className="text-sm font-medium">Settings</span>
          </Link>
        </nav>
      </aside>

      {/* Main Content */}
      <main className="flex-1 flex flex-col overflow-hidden relative">
        {/* Background glow */}
        <div className="absolute top-0 right-0 w-[40%] h-[40%] bg-[var(--accent-primary)]/5 rounded-full blur-[100px] pointer-events-none" />

        <header className="h-16 border-b border-[var(--border-light)] flex items-center justify-between px-8 bg-[var(--bg-glass)] backdrop-blur-md z-10">
          <h2 className="font-display font-medium text-lg">Dashboard</h2>

          <div className="flex items-center gap-4">
            {/* Vitals strip — reframes overall system status */}
            <div
              className={`flex items-center gap-2 px-3 py-1.5 rounded-full border text-xs font-semibold tracking-wide ${
                isNominal
                  ? 'border-[var(--success)] bg-[var(--success-bg)] text-[var(--success)]'
                  : 'border-[var(--error)] bg-[var(--error-bg)] text-[var(--error)]'
              }`}
            >
              {isNominal ? (
                <>
                  <span className="status-dot-live" aria-hidden="true" />
                  <CheckCircle2 size={14} aria-hidden="true" />
                  <span>ALL SYSTEMS NOMINAL</span>
                </>
              ) : (
                <>
                  <Flame size={14} aria-hidden="true" />
                  <span>{CRITICAL_INCIDENT_COUNT} CRITICAL</span>
                </>
              )}
            </div>

            <ThemeToggle />
            <UserButton />
          </div>
        </header>

        <div className="flex-1 overflow-auto p-8 z-10">
          {children}
        </div>
      </main>
    </div>
  );
}
