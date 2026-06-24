# OpsPilot Implementation Plan Update

Date: 2026-06-24

This document captures the build decisions that emerged after reviewing the project Markdown files and the current codebase. It is meant to be the practical execution plan, while `instructions.md` remains the broader product and architecture reference.

## Core Direction

OpsPilot should be built as a guided DevOps control plane, not as an unbounded autonomous deployer.

The system should:

- inspect repositories and deployment context,
- generate evidence-backed plans,
- collect missing secrets safely,
- require approval before risky actions,
- execute provider-specific steps through adapters,
- show a clear workflow timeline,
- create pull requests for code/config changes instead of silently mutating repos.

The AI should help plan, explain, generate, and diagnose. Deterministic services should own permissions, execution, retries, approval checks, audit logs, and provider state.

## Scalable MVP Architecture

The existing foundation is good:

- Next.js frontend with Clerk auth
- Go API Gateway
- Go gRPC services for auth, workspace, and project
- PostgreSQL with pgvector
- Redis
- NATS
- Python AI worker skeleton

The next architecture step should not be a full split into many production microservices. Instead, keep the current service-oriented shape and add only the domains required for the core loop.

Recommended next domains:

| Domain | Form for MVP | Responsibility |
| --- | --- | --- |
| Integration | Dedicated Go Integration Service | GitHub App, provider accounts, encrypted credentials |
| Ingestion | Python or Go worker | Fetch repo files, filter secrets, parse and chunk |
| RAG | Python worker/API | Embed chunks, query pgvector, generate cited answers |
| Workflow | Go service or gateway-backed module first | Durable plans, approvals, timeline, status |
| Provider adapters | Go package/service | GitHub, Vercel, Railway capability-based adapters |

The architecture should preserve service boundaries even if some domains start inside an existing service for speed. Promote them to separate services only when scaling, ownership, or failure isolation demands it.

## Product Design Direction

The UI should feel like an operations cockpit rather than a generic SaaS dashboard.

Recommended surfaces:

- Project home with repo status, indexing status, readiness score, latest deploys, incidents, and active workflows.
- Connect flow that clearly shows GitHub permissions and selected repositories.
- Workflow timeline with every step, status, evidence, tool call, approval, and result.
- Evidence panel showing source files, logs, retrieved chunks, generated diffs, and provider events.
- Approval screen that shows what will change, why, risk level, rollback/cancel behavior, and exact provider/repo impact.
- Chat as a command and analysis surface with citations, confidence, and "not enough context" states.

Design rules for OpsPilot:

- prefer dense, scannable operational layouts;
- avoid excessive glow/glass effects on workflow-critical screens;
- make statuses and risks visually obvious;
- keep secrets out of visible logs, prompts, and AI context;
- show source evidence beside AI conclusions.

## Next Build Milestone

The next milestone should be:

```text
GitHub-connected project -> repository ingestion -> source-cited project Q&A
```

This is the right next step because every later feature depends on trustworthy repo context:

- deployment planning needs repo structure,
- Docker generation needs manifests/configs,
- log analysis needs related code/config retrieval,
- PR fixes need GitHub access,
- provider deployment needs repo metadata and build settings.

## Milestone Acceptance Criteria

The milestone is complete when a user can:

1. Install/connect the OpsPilot GitHub App.
2. Select one or more accessible repositories.
3. Attach a repository to an OpsPilot project.
4. Start an indexing job.
5. See indexing status in the project UI.
6. Exclude `.env`, secrets, binaries, `.git`, `node_modules`, build outputs, and large generated files.
7. Store safe documents and chunks in PostgreSQL/pgvector.
8. Ask a project question.
9. Receive an answer with cited files/chunks.
10. See a clear "not enough context" response when retrieval is weak.

## Concrete Implementation Order

### 1. Database [Completed]

Add a migration for GitHub and ingestion state:

- `integrations`
- `github_installations`
- `repository_connections`
- `ingestion_jobs`
- `ingestion_events`
- optionally `secret_redaction_events`

Keep credentials encrypted. Store installation IDs and repo metadata, not raw long-lived user tokens.

### 2. GitHub App Backend

Add GitHub App support:

- app installation callback,
- webhook receiver with signature verification,
- installation token exchange,
- list installations/repositories,
- persist selected repo metadata,
- revoke/disable handling when installation is removed.

Use a GitHub App as the default integration. Personal access tokens should remain a fallback, not the product path.

### 3. Project Repo Connection UI

Replace the current "Connect Repository" placeholder form with a real flow:

- connect GitHub,
- choose installation/account,
- choose repository,
- attach repo to project,
- show branch, last indexed commit, and indexing status.

### 4. Ingestion Worker

Create a NATS-backed indexing job:

```text
repo.connected
  -> ingestion.started
  -> file_tree.fetched
  -> files.filtered
  -> chunks.created
  -> embeddings.created
  -> repo.indexed
```

Start with safe file types:

- Markdown
- package manifests
- Docker/deployment configs
- TypeScript/JavaScript
- Go
- Python
- Java
- YAML/JSON/TOML

### 5. Embeddings and Retrieval

Implement Gemini embeddings into `document_chunks.embedding` using the existing 768-dimensional pgvector schema.

Add retrieval with:

- workspace/project filters,
- top-k vector search,
- file metadata,
- basic keyword boosting for filenames, errors, and config files,
- source citations in the response.

### 6. Project Q&A UI

Add a project-level Q&A panel:

- prompt input,
- streaming or loading state,
- answer body,
- cited source list,
- confidence/evidence indicator,
- feedback buttons.

### 7. Safety and Observability

Add minimum production-grade controls early:

- request IDs in every log,
- audit events for integration, indexing, and AI query actions,
- per-workspace rate limits,
- failed job retry count,
- dead-letter state for failed indexing,
- redaction before persistence and before model calls.

## What Not To Build Yet

Delay these until the repo-context loop works:

- autonomous deployment execution,
- AWS EC2 adapter,
- Kubernetes support,
- billing UI,
- complex incident memory,
- fully separated database per service,
- advanced multi-agent orchestration,
- automatic rollback.

These are important, but they should sit on top of a reliable GitHub + RAG foundation.

## Immediate Next Step

Build the GitHub App connection and repository selection flow first.

Supporting docs:

- GitHub App setup values and permissions: [docs/github-app-setup.md](./docs/github-app-setup.md)
- HTTP API contract: [docs/github-integration-api-contract.md](./docs/github-integration-api-contract.md)
- Local milestone checklist: [docs/local-github-milestone-checklist.md](./docs/local-github-milestone-checklist.md)

Suggested engineering slice:

```text
Migration -> GitHub App config -> install callback -> repo list API -> attach repo to project -> project UI status
```

Once that works, the ingestion worker becomes straightforward because the system will already know which repository, branch, installation, workspace, and project it is allowed to operate on.
