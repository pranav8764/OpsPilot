import { PublicPage } from "@/components/public-page";

export default function PrivacyPage() {
  return (
    <PublicPage
      eyebrow="Privacy notice · 13 July 2026"
      title="Your operational data stays scoped to your workspace"
      description="This notice describes the data handled by the OpsPilot early-access product and the controls built into the service."
    >
      <section className="glass-panel p-6 md:p-8 space-y-6">
        <div>
          <h2 className="text-xl">Data we process</h2>
          <p className="text-zinc-400">Account identifiers, workspace and project metadata, approved repository content, user questions, deployment logs, incident records, workflow history, and usage counters.</p>
        </div>
        <div>
          <h2 className="text-xl">Secrets and credentials</h2>
          <p className="text-zinc-400">Known secret files are excluded from indexing. Secret-like values are redacted before storage or model use. Provider credentials are encrypted at rest and are write-only through the product API.</p>
        </div>
        <div>
          <h2 className="text-xl">Service providers</h2>
          <p className="text-zinc-400">OpsPilot may communicate with Clerk for identity, GitHub for repository access and draft pull requests, configured Vercel or Railway accounts for logs, and the model provider selected by the operator.</p>
        </div>
        <div>
          <h2 className="text-xl">Retention and deletion</h2>
          <p className="text-zinc-400">Workspace administrators can choose log retention from 1 to 365 days; the default is 30 days. Authorized project deletion cascades through project-scoped source, logs, incidents, workflows, proposals, and usage records.</p>
        </div>
        <div>
          <h2 className="text-xl">Access and responsibility</h2>
          <p className="text-zinc-400">Workspace role checks scope product data. Workspace owners are responsible for granting repository and provider access only to authorized team members and for configuring any additional legal or regional requirements before production use.</p>
        </div>
      </section>
    </PublicPage>
  );
}
