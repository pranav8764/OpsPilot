import type { ReactNode } from "react";
import Link from "next/link";
import { Eye } from "lucide-react";
import { ThemeToggle } from "@/components/theme-toggle";

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
        <Link href="/" className="flex items-center gap-2" aria-label="Archiatros home">
          <div className="brand-medallion" aria-hidden="true">
            <Eye size={18} />
          </div>
          <span className="font-display font-semibold text-xl">Archiatros</span>
        </Link>
        <nav className="flex items-center gap-2 sm:gap-5 text-sm">
          <Link href="/docs" className="btn btn-ghost">Docs</Link>
          <ThemeToggle />
          <Link href="/sign-in" className="btn btn-secondary">Sign in</Link>
        </nav>
      </header>
      <main className="container flex-1 py-16 md:py-24">
        <div className="max-w-3xl">
          <div className="badge badge-neutral mb-5">{eyebrow}</div>
          <h1 className="inline-block [font-family:var(--font-serif)] text-4xl md:text-6xl font-semibold leading-tight mb-5 pb-3 border-b-2 border-[var(--accent-gold)]">
            {title}
          </h1>
          <p className="text-lg md:text-xl text-[var(--text-secondary)] leading-[1.7]">{description}</p>
        </div>
        <div className="max-w-4xl mt-14 space-y-8 text-[var(--text-primary)] leading-[1.7]">{children}</div>
      </main>
      <footer className="border-t border-[var(--border-light)] py-8">
        <div className="container flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 text-sm text-[var(--text-muted)]">
          <span>&copy; 2026 Archiatros.</span>
          <div className="flex gap-5">
            <Link href="/privacy" className="hover:text-[var(--text-primary)]">Privacy</Link>
            <Link href="/terms" className="hover:text-[var(--text-primary)]">Terms</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
