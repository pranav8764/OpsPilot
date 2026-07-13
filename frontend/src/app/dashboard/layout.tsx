import Link from 'next/link';
import { UserButton } from '@clerk/nextjs';
import { Bot, Home, Settings } from 'lucide-react';
import SyncUser from '@/components/SyncUser';

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-primary)]">
      <SyncUser />

      {/* Sidebar */}
      <aside className="w-64 border-r border-[var(--border-light)] bg-[var(--bg-secondary)] flex flex-col">
        <div className="p-6 flex items-center gap-3 border-b border-[var(--border-light)]">
          <div className="w-8 h-8 rounded bg-gradient-to-br from-indigo-500 to-pink-500 flex-center">
            <Bot size={20} className="text-white" />
          </div>
          <span className="font-display font-bold text-xl">OpsPilot</span>
        </div>

        <nav className="flex-1 p-4 flex flex-col gap-2">
          <Link href="/dashboard" className="flex items-center gap-3 px-3 py-2 rounded-md hover:bg-[#ffffff0d] text-[var(--text-secondary)] hover:text-white transition-colors">
            <Home size={18} />
            <span className="text-sm font-medium">Workspaces</span>
          </Link>
          <Link href="/dashboard/settings" className="flex items-center gap-3 px-3 py-2 rounded-md hover:bg-[#ffffff0d] text-[var(--text-secondary)] hover:text-white transition-colors mt-auto">
            <Settings size={18} />
            <span className="text-sm font-medium">Settings</span>
          </Link>
        </nav>
      </aside>

      {/* Main Content */}
      <main className="flex-1 flex flex-col overflow-hidden relative">
        {/* Background glow */}
        <div className="absolute top-0 right-0 w-[40%] h-[40%] bg-indigo-600/5 rounded-full blur-[100px] pointer-events-none" />

        <header className="h-16 border-b border-[var(--border-light)] flex items-center justify-between px-8 bg-[var(--bg-glass)] backdrop-blur-md z-10">
          <h2 className="font-display font-medium text-lg">Dashboard</h2>
          <UserButton />
        </header>

        <div className="flex-1 overflow-auto p-8 z-10">
          {children}
        </div>
      </main>
    </div>
  );
}
