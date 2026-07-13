import Link from "next/link";
import { UserButton } from "@clerk/nextjs";
import { auth } from "@clerk/nextjs/server";
import { ArrowRight, Terminal, Bot, Shield, Zap } from "lucide-react";

export default async function LandingPage() {
  const { userId } = await auth();

  return (
    <div className="page-wrapper relative">
      {/* Background glowing effects */}
      <div className="absolute top-[-20%] left-[-10%] w-[50%] h-[50%] bg-indigo-600/20 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute top-[20%] right-[-10%] w-[40%] h-[40%] bg-pink-600/10 rounded-full blur-[100px] pointer-events-none" />

      {/* Header */}
      <header className="container py-6 flex-between relative z-10">
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded bg-gradient-to-br from-indigo-500 to-pink-500 flex-center">
            <Bot size={20} className="text-white" />
          </div>
          <span className="font-display font-bold text-xl tracking-tight">OpsPilot</span>
        </div>

        <nav className="flex items-center gap-2 sm:gap-6">
          <Link href="#features" className="hidden sm:inline text-sm font-medium text-zinc-400 hover:text-white transition-colors">Features</Link>
          <Link href="/docs" className="hidden sm:inline text-sm font-medium text-zinc-400 hover:text-white transition-colors">Docs</Link>
          {userId ? (
            <div className="flex items-center gap-4">
              <Link href="/dashboard" className="btn btn-secondary">
                Go to Dashboard
              </Link>
              <UserButton />
            </div>
          ) : (
            <div className="flex items-center gap-3">
              <Link href="/sign-in" className="btn btn-ghost">Sign In</Link>
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
          <span className="text-emerald-400">Live</span><span>Evidence-backed incident-to-fix workflows</span>
        </div>

        <h1 className="text-5xl md:text-7xl font-bold max-w-4xl leading-tight mb-6 animate-fade-in animate-delay-1">
          Turn production evidence into a <span className="text-gradient-accent">reviewed fix</span>
        </h1>

        <p className="text-xl text-zinc-400 max-w-2xl mb-10 animate-fade-in animate-delay-2">
          OpsPilot connects repository context with redacted runtime logs, proposes a bounded patch, verifies it in an isolated sandbox, and opens a draft FIX pull request only after human approval.
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
            <div className="w-12 h-12 rounded-lg bg-indigo-500/20 flex-center mb-6">
              <Terminal size={24} className="text-indigo-400" />
            </div>
            <h3 className="text-xl mb-3 text-white">Smart Repositories</h3>
            <p className="text-zinc-400 text-sm">
              Connect GitHub repositories. OpsPilot indexes pinned commits and answers with file-and-line evidence.
            </p>
          </div>

          <div className="glass-card p-8 text-left">
            <div className="w-12 h-12 rounded-lg bg-pink-500/20 flex-center mb-6">
              <Zap size={24} className="text-pink-400" />
            </div>
            <h3 className="text-xl mb-3 text-white">Agentic Automation</h3>
            <p className="text-zinc-400 text-sm">
              Collect Vercel or Railway logs, redact secrets, verify a targeted patch, and open an approval-gated draft PR.
            </p>
          </div>

          <div className="glass-card p-8 text-left">
            <div className="w-12 h-12 rounded-lg bg-emerald-500/20 flex-center mb-6">
              <Shield size={24} className="text-emerald-400" />
            </div>
            <h3 className="text-xl mb-3 text-white">Controlled by Design</h3>
            <p className="text-zinc-400 text-sm">
              Built with workspace RBAC, encrypted provider credentials, immutable approvals, and isolated test sandboxes.
            </p>
          </div>
        </div>

        <section id="workflow" className="container mt-32 scroll-mt-8">
          <div className="max-w-3xl mx-auto mb-12">
            <div className="badge badge-neutral mb-4">Guardrailed workflow</div>
            <h2 className="text-3xl md:text-5xl font-bold mb-4">From signal to draft PR, with evidence at every step</h2>
            <p className="text-zinc-400 text-lg">
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
                <span className="text-xs font-semibold tracking-[0.2em] text-indigo-400">{number}</span>
                <h3 className="text-lg mt-3 mb-2">{title}</h3>
                <p className="text-sm text-zinc-400">{detail}</p>
              </li>
            ))}
          </ol>
          <div className="mt-10">
            <Link href="/docs" className="btn btn-secondary">Read the workflow and guardrails</Link>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t border-[#ffffff1a] py-8 mt-20 relative z-10">
        <div className="container flex-between">
          <div className="text-zinc-500 text-sm">
            &copy; 2026 OpsPilot. All rights reserved.
          </div>
          <div className="flex gap-4 text-sm text-zinc-500">
            <Link href="/privacy" className="hover:text-white transition-colors">Privacy</Link>
            <Link href="/terms" className="hover:text-white transition-colors">Terms</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
