# OpsPilot implementation status and roadmap

Updated: 13 July 2026

## Product boundary

The current release completes this controlled loop:

```text
GitHub project
  -> pinned repository ingestion and cited retrieval
  -> manual/Vercel/Railway logs
  -> redaction and evidence-backed RCA
  -> policy-bounded patch
  -> isolated baseline/patched verification
  -> exact human approval
  -> revalidation
  -> new branch and draft FIX PR
```

The system is a guided DevOps control plane, not an autonomous deployer. Merge, deploy, rollback, infrastructure mutation, and arbitrary shell/cloud access remain excluded.

## Completed

- [x] Clerk identity, workspaces, roles, projects, usage limits, deletion, and audit.
- [x] Secure one-time GitHub App installation flow and canonical repository attachment.
- [x] Durable NATS/DB-recovered repository ingestion, filters/redaction, embeddings, pgvector retrieval, and citations.
- [x] Project Q&A with confidence/insufficient-context behavior.
- [x] AES-GCM Vercel and Railway log connections; server-side runtime/build collection.
- [x] Durable logs, incidents, RCA, workflows, ordered steps, evidence, and history UI.
- [x] Structured fix generation, path/size/secret policy, unified diff, and pinned file hashes.
- [x] Dedicated Docker runner with manifest-only dependency install and no-network non-root test phase.
- [x] Expiring single-use payload-hashed owner/admin approval.
- [x] GitHub writer that rechecks base/file hashes and opens only a new branch/draft PR.
- [x] Deployment readiness and deterministic Node/Python/Go deployment-file drafts.
- [x] Billing-ready subscriptions/atomic usage events and dashboard meters.
- [x] Configurable log retention and lifecycle cleanup.
- [x] Exact CORS, request/IP-token rate limit, security headers, request correlation, JSON logs, metrics, readiness, non-root/read-only images, and CI.
- [x] Unit/contract/policy/static frontend/Compose verification suites.
- [x] Tailwind v4 production styling, public workflow/docs/privacy/terms routes, Clerk theme layering, responsive browser QA, and authenticated-dashboard component tests.

## Release gate still requiring environment credentials

Run the live checklist in [docs/local-github-milestone-checklist.md](docs/local-github-milestone-checklist.md) with:

- running Docker daemon;
- real Clerk test tenant;
- disposable GitHub App/repository;
- optional Groq/Gemini test keys;
- disposable Vercel/Railway deployment/token.

No unit test can prove external credentials, GitHub installation policy, or provider account state. The repository contains fake-server contract tests and CI gates so the live pass is small and repeatable.

## Next roadmap

### Milestone A — measurable reliability

- Golden incident corpus and offline evaluation dashboard.
- Prompt-injection/tool-policy red-team suite.
- Full fake-provider end-to-end environment test.
- OpenTelemetry traces, SLO dashboards, restore drills, distributed rate limiting.

### Milestone B — better investigation

- Signed alert-trigger intake, grouping/deduplication, recurrence links.
- Parallel bounded hypotheses with a persisted supported/rejected/inconclusive evidence graph.
- Deployment-to-exact-commit/service/environment mapping.
- Sentry, Datadog/OpenTelemetry, Grafana/Loki, CloudWatch, and Kubernetes read-only adapters.
- Service catalog, ownership, runbooks, and criticality-aware policy.

### Milestone C — better fix lifecycle

- Regression-test synthesis and reproduce-before/fix-after evidence.
- Bounded CI/review iteration on existing draft PRs with fresh approval on expanded scope.
- AST-aware patch minimization, dependency/migration risk policy, CODEOWNERS integration.
- Preview/canary evidence ingestion without deploy permission.

### Milestone D — team and enterprise controls

- Slack/on-call incident surface with signed deep-link approval.
- Verified incident feedback/memory and draft postmortems.
- SSO/SCIM, custom policy, audit export, customer-managed keys, residency/model controls.
- Optional ephemeral/no-source-retention mode.

### Explicit non-goal until safety data supports it

Production remediation may eventually expose predeclared, reversible runbook actions. It must never start as model access to a cloud shell. Require environment allowlists, preflight, idempotency, bounded blast radius, two-person production approval, health gates, automatic stop/rollback, and audit before enabling any such action.

See [product analysis](docs/PRODUCT_ANALYSIS.md) for competitor research and priority rationale, and [architecture](docs/ARCHITECTURE.md) for tool/guardrail contracts.
