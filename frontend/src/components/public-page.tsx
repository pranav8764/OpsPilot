import type { ReactNode } from "react";
import Link from "next/link";
import { Bot } from "lucide-react";

export function PublicPage({
  eyebrow,
  title,
  description,
  children,
}: {
  eyebrow: string;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <div className="page-wrapper">
      <header className="container py-6 flex-between">
        <Link href="/" className="flex items-center gap-2" aria-label="OpsPilot home">
          <span className="w-8 h-8 rounded bg-gradient-to-br from-indigo-500 to-pink-500 flex-center">
            <Bot size={20} className="text-white" />
          </span>
          <span className="font-display font-bold text-xl">OpsPilot</span>
        </Link>
        <nav className="flex items-center gap-2 sm:gap-5 text-sm">
          <Link href="/docs" className="btn btn-ghost">Docs</Link>
          <Link href="/sign-in" className="btn btn-secondary">Sign in</Link>
        </nav>
      </header>
      <main className="container flex-1 py-16 md:py-24">
        <div className="max-w-3xl">
          <div className="badge badge-neutral mb-5">{eyebrow}</div>
          <h1 className="text-4xl md:text-6xl font-bold leading-tight mb-5">{title}</h1>
          <p className="text-lg md:text-xl text-zinc-400 leading-relaxed">{description}</p>
        </div>
        <div className="max-w-4xl mt-14 space-y-8 text-zinc-300 leading-relaxed">{children}</div>
      </main>
      <footer className="border-t border-[#ffffff1a] py-8">
        <div className="container flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 text-sm text-zinc-500">
          <span>&copy; 2026 OpsPilot.</span>
          <div className="flex gap-5">
            <Link href="/privacy" className="hover:text-white">Privacy</Link>
            <Link href="/terms" className="hover:text-white">Terms</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
