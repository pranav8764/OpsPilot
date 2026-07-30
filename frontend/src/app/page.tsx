import Link from "next/link";
import { UserButton } from "@clerk/nextjs";
import { auth } from "@clerk/nextjs/server";
import { ArrowRight, Terminal, Eye, Shield, Zap, Construction, Sparkles } from "lucide-react";
import { ThemeToggle } from "@/components/theme-toggle";

export default async function LandingPage() {
  const { userId } = await auth();

  return (
    <div className="page-wrapper relative bg-[var(--bg-primary)]">
      {/* Soft pastel ambient wash — a single restrained bloom per side, not a dense field */}
      <div className="absolute inset-0 overflow-hidden pointer-events-none" aria-hidden="true">
        <div
          className="absolute top-[-15%] left-[-10%] w-[50%] h-[50%] rounded-full blur-[140px]"
          style={{ background: "var(--accent-primary)", opacity: 0.14 }}
        />
        <div
          className="absolute bottom-[-20%] right-[-10%] w-[45%] h-[45%] rounded-full blur-[140px]"
          style={{ background: "var(--accent-secondary)", opacity: 0.12 }}
        />
      </div>

      {/* Header */}
      <header className="container py-6 flex-between relative z-10">
        <div className="flex items-center gap-2">
          <span className="brand-medallion">
            <Eye size={18} />
          </span>
          <span
            className="font-semibold text-xl tracking-wide"
            style={{ fontFamily: "var(--font-serif)" }}
          >
            Archiatros
          </span>
        </div>

        <nav className="flex items-center gap-2 sm:gap-6">
          <Link href="#features" className="hidden sm:inline text-sm font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors">Features</Link>
          <Link href="/docs" className="hidden sm:inline text-sm font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors">Docs</Link>
          <ThemeToggle />
          {userId ? (
            <div className="flex items-center gap-2 sm:gap-4">
              <Link href="/dashboard" className="hidden sm:inline-flex btn btn-secondary">
                Go to Dashboard
              </Link>
              <UserButton />
            </div>
          ) : (
            <div className="flex items-center gap-2 sm:gap-3">
              <Link href="/sign-in" className="hidden sm:inline-flex btn btn-ghost">Sign In</Link>
              <Link href="/sign-up" className="btn btn-primary">
                Get Started
              </Link>
            </div>
          )}
        </nav>
      </header>

      {/* Hero Section */}
      <main className="main-content flex flex-col items-center justify-center text-center mt-12 relative z-10">
        <div className="badge badge-neutral gap-2 mb-8 animate-fade-in">
          <span style={{ color: "var(--success)" }}>Live</span><span>Evidence-backed incident-to-fix workflows</span>
        </div>

        <h1
          className="text-5xl md:text-7xl font-bold max-w-4xl leading-tight mb-6 animate-fade-in animate-delay-1"
          style={{ fontFamily: "var(--font-serif)" }}
        >
          The Oracle Sees Your Incidents <span className="text-gradient-accent">Before You Do.</span>
        </h1>

        <p className="text-xl max-w-2xl mb-10 animate-fade-in animate-delay-2" style={{ color: "var(--text-secondary)" }}>
          Archiatros — Greek for chief physician — diagnoses production incidents in seconds and proposes a verified fix pull request, so on-call stops meaning 3am guesswork.
        </p>

        <div className="flex flex-wrap justify-center gap-4 animate-fade-in animate-delay-3">
          <Link href={userId ? "/dashboard" : "/sign-up"} className="btn btn-primary text-base px-8 py-4">
            Start Building Free <ArrowRight size={18} />
          </Link>
          <a href="#workflow" className="btn btn-secondary text-base px-8 py-4">
            See How It Works
          </a>
        </div>

        {/* Feature grid */}
        <div id="features" className="container mt-32 grid grid-cols-1 md:grid-cols-3 gap-6 animate-fade-in scroll-mt-8" style={{ animationDelay: '400ms' }}>
          <div className="glass-card p-8 text-left">
            <div className="w-12 h-12 rounded-lg bg-[var(--accent-primary)]/20 flex-center mb-6">
              <Terminal size={24} className="text-[var(--accent-primary-hover)]" />
            </div>
            <h3 className="text-xl mb-3">Smart Repositories</h3>
            <p className="text-sm" style={{ color: "var(--text-secondary)" }}>
              Connect GitHub repositories. Archiatros indexes pinned commits and answers with file-and-line evidence.
            </p>
          </div>

          <div className="glass-card p-8 text-left">
            <div className="w-12 h-12 rounded-lg bg-[var(--accent-secondary)]/20 flex-center mb-6">
              <Zap size={24} className="text-[var(--accent-secondary)]" />
            </div>
            <h3 className="text-xl mb-3">Agentic Automation</h3>
            <p className="text-sm" style={{ color: "var(--text-secondary)" }}>
              Collect Vercel or Railway logs, redact secrets, verify a targeted patch, and open an approval-gated draft PR.
            </p>
          </div>

          <div className="glass-card p-8 text-left">
            <div className="w-12 h-12 rounded-lg bg-[var(--accent-gold)]/20 flex-center mb-6">
              <Shield size={24} className="text-[var(--accent-gold)]" />
            </div>
            <h3 className="text-xl mb-3">Controlled by Design</h3>
            <p className="text-sm" style={{ color: "var(--text-secondary)" }}>
              Built with workspace RBAC, encrypted provider credentials, immutable approvals, and isolated test sandboxes.
            </p>
          </div>
        </div>

        <section id="workflow" className="container mt-32 scroll-mt-8">
          <div className="max-w-3xl mx-auto mb-12">
            <div className="badge badge-neutral mb-4">Guardrailed workflow</div>
            <h2 className="text-3xl md:text-5xl font-bold mb-4">From signal to draft PR, with evidence at every step</h2>
            <p className="text-lg" style={{ color: "var(--text-secondary)" }}>
              The agent can investigate and prepare a fix. It cannot merge, deploy, roll back, or run an unrestricted cloud shell.
            </p>
          </div>
          <ol className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 text-left">
            {[
              ["01", "Collect", "Fetch bounded Vercel or Railway logs, or accept pasted logs."],
              ["02", "Ground", "Retrieve tenant-scoped repository and incident evidence with citations."],
              ["03", "Diagnose", "Persist the RCA, confidence, evidence, workflow, and ordered steps."],
              ["04", "Propose", "Generate a small policy-compliant diff pinned to the indexed commit."],
              ["05", "Verify", "Run baseline and patched tests in an isolated, no-network test phase."],
              ["06", "Approve", "Revalidate hashes, then create a new branch and draft PR after approval."],
            ].map(([number, title, detail]) => (
              <li key={number} className="glass-card p-6">
                <span className="text-xs font-semibold tracking-[0.2em] text-[var(--accent-secondary)]">{number}</span>
                <h3 className="text-lg mt-3 mb-2">{title}</h3>
                <p className="text-sm" style={{ color: "var(--text-secondary)" }}>{detail}</p>
              </li>
            ))}
          </ol>
          <div className="mt-10">
            <Link href="/docs" className="btn btn-secondary">Read the workflow and guardrails</Link>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="py-8 mt-20 relative z-10" style={{ borderTop: "1px solid var(--border-light)" }}>
        <div className="container flex-between flex-wrap gap-4">
          <div className="flex items-center gap-4 flex-wrap">
            <span className="text-sm" style={{ color: "var(--text-muted)" }}>
              &copy; 2026 Archiatros. All rights reserved.
            </span>
            <span className="under-construction-badge">
              <Construction size={12} />
              Calibrating
              <Sparkles size={12} />
            </span>
          </div>
          <div className="flex gap-4 text-sm" style={{ color: "var(--text-muted)" }}>
            <Link href="/privacy" className="hover:text-[var(--text-primary)] transition-colors">Privacy</Link>
            <Link href="/terms" className="hover:text-[var(--text-primary)] transition-colors">Terms</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
