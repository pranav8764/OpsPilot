import { PublicPage } from "@/components/public-page";

export default function TermsPage() {
  return (
    <PublicPage
      eyebrow="Early-access terms · 13 July 2026"
      title="Use Archiatros as an assisted engineering tool"
      description="Archiatros prepares evidence and draft changes. Your team remains responsible for reviewing, testing, merging, deploying, and operating its software."
    >
      <section className="glass-panel p-6 md:p-8 space-y-6">
        <div>
          <h2 className="text-xl">Authorized use</h2>
          <p className="text-[var(--text-secondary)]">You may connect only repositories, logs, provider accounts, and workspaces that you are authorized to access. Do not use the service to probe third-party systems, introduce malicious code, or bypass security controls.</p>
        </div>
        <div>
          <h2 className="text-xl">Human review is required</h2>
          <p className="text-[var(--text-secondary)]">AI-generated analysis and patches may be incomplete or wrong. Approval permits only the described draft-PR action; it is not a warranty that a change is correct or safe to merge.</p>
        </div>
        <div>
          <h2 className="text-xl">No production control</h2>
          <p className="text-[var(--text-secondary)]">The current product does not merge pull requests, deploy applications, roll back releases, or provide an unrestricted shell. Do not treat readiness scores or sandbox results as a substitute for your normal review and CI controls.</p>
        </div>
        <div>
          <h2 className="text-xl">Early-access availability</h2>
          <p className="text-[var(--text-secondary)]">Features may change and service interruptions may occur. Operators should maintain backups, provider recovery procedures, branch protection, and independent audit records appropriate to their environment.</p>
        </div>
        <div>
          <h2 className="text-xl">Your content</h2>
          <p className="text-[var(--text-secondary)]">You retain responsibility for and rights to your code and operational content. You grant the service the limited permission needed to process that content for the workflows you request.</p>
        </div>
      </section>
    </PublicPage>
  );
}
