import Link from "next/link";
import { PublicPage } from "@/components/public-page";

const steps = [
  ["1. Connect", "Install the least-privilege GitHub App, select a repository and branch, then let OpsPilot index a pinned commit."],
  ["2. Investigate", "Paste logs or connect a Vercel or Railway deployment. Logs are bounded, normalized, redacted, and joined with cited repository evidence."],
  ["3. Review", "Inspect the durable incident, root-cause confidence, evidence, proposed file changes, exact diff, and sandbox results."],
  ["4. Approve", "An owner or admin can approve the exact payload before OpsPilot revalidates the base commit and opens a draft FIX PR."],
];

export default function DocsPage() {
  return (
    <PublicPage
      eyebrow="Product guide"
      title="A safe operating loop for AI-assisted fixes"
      description="OpsPilot is built around evidence, bounded tools, durable state, and explicit approval—not autonomous production access."
    >
      <section className="glass-panel p-6 md:p-8">
        <h2 className="text-2xl mb-5">Core workflow</h2>
        <ol className="space-y-5">
          {steps.map(([title, detail]) => (
            <li key={title}>
              <h3 className="text-lg mb-1">{title}</h3>
              <p className="text-zinc-400">{detail}</p>
            </li>
          ))}
        </ol>
      </section>

      <section className="grid grid-cols-1 md:grid-cols-2 gap-5">
        <div className="glass-card p-6">
          <h2 className="text-xl">What the agent can do</h2>
          <ul className="mt-4 space-y-2 text-zinc-400 list-disc pl-5">
            <li>Read approved repository files at a pinned commit.</li>
            <li>Collect bounded runtime or build logs.</li>
            <li>Create cited RCA and deployment-readiness reports.</li>
            <li>Propose up to five safe text-file changes.</li>
            <li>Run detected tests in a hardened Docker sandbox.</li>
            <li>Open a new branch and draft PR after approval.</li>
          </ul>
        </div>
        <div className="glass-card p-6">
          <h2 className="text-xl">What it cannot do</h2>
          <ul className="mt-4 space-y-2 text-zinc-400 list-disc pl-5">
            <li>Read secret files or send detected secrets to a model.</li>
            <li>Modify workflows, keys, lockfiles, or generated files.</li>
            <li>Push to the base branch or merge a pull request.</li>
            <li>Deploy, roll back, or mutate cloud infrastructure.</li>
            <li>Reuse an expired, changed, or consumed approval.</li>
            <li>Run tests with network access.</li>
          </ul>
        </div>
      </section>

      <section className="glass-panel p-6 md:p-8">
        <h2 className="text-2xl">Operator documentation</h2>
        <p className="text-zinc-400 mb-5">
          Setup, architecture, API contracts, production hardening, backups, and the real-provider acceptance checklist live with the source repository.
        </p>
        <a
          href="https://github.com/pranav8764/OpsPilot#readme"
          className="btn btn-primary"
          target="_blank"
          rel="noreferrer"
        >
          Open repository documentation
        </a>
      </section>

      <div>
        <Link href="/sign-up" className="btn btn-primary">Create an account</Link>
      </div>
    </PublicPage>
  );
}
