# OpsPilot

> Agentic AI DevOps Assistant for Small Teams

OpsPilot helps small engineering teams deploy, monitor, debug, and recover applications using Agentic AI + RAG.

## Architecture

```
Frontend (Next.js)  →  API Gateway (Go :8080)  →  gRPC Services
                                                    ├── Auth Service     :9001
                                                    ├── Workspace Service :9002
                                                    ├── Project Service  :9003
                                                    └── Integration Service :9004

Infrastructure: PostgreSQL + pgvector | Redis | NATS
AI Worker: Python FastAPI (Phase 3)
```

## Current Build Plan

The practical implementation plan is tracked in [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md).

Current build target:

```
GitHub-connected project -> repository ingestion -> source-cited project Q&A
```

The GitHub App connection slice is working locally: OpsPilot can install a GitHub App, store the installation, list accessible repositories, attach a repository to a project, and create a queued ingestion job. The next major work is processing queued ingestion jobs by fetching repository files, indexing/chunking them, then adding embeddings, retrieval, and a cited project Q&A UI.

GitHub milestone docs:

- [GitHub App setup](./docs/github-app-setup.md)
- [GitHub API contract](./docs/github-integration-api-contract.md)
- [Local GitHub milestone checklist](./docs/local-github-milestone-checklist.md)

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Docker Desktop | Latest | [docker.com](https://docker.com) |
| Go | 1.22+ | `brew install go` |
| Node.js | 20+ | `brew install node` |
| Python | 3.11+ | `brew install python@3.11` |

## Quick Start

### 1. Clone & Setup
```bash
cp .env.example .env
# Fill in your API keys (see .env.example for details)
```

### 2. Start Infrastructure + Backend
```bash
make up
```

For local GitHub App private-key loading, keep the `.pem` file under `secrets/`. Docker Compose mounts this directory into the Integration Service as read-only.

### 3. Run Migrations
```bash
make migrate
```

### 4. Start Frontend
```bash
cd frontend
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000)

## Environment Variables

See `.env.example` for all required variables. You need:
- **Clerk** keys → [clerk.com](https://clerk.com) (free)
- **GitHub App** values → [GitHub App setup](./docs/github-app-setup.md)
- **Groq** API key → [groq.com](https://groq.com) (free, Phase 3)
- **Gemini** API key → [aistudio.google.com](https://aistudio.google.com) (free, Phase 3)

For local GitHub setup:

```bash
openssl rand -base64 32
```

Use the output for `CREDENTIAL_ENCRYPTION_KEY`. Put the GitHub-downloaded `.pem` private key in `secrets/` and set `GITHUB_APP_PRIVATE_KEY_PATH` to that path. Leave `GITHUB_APP_PRIVATE_KEY_BASE64` empty unless you intentionally use the base64-encoded PEM option.

## Make Commands

```bash
make up          # Start all Docker services
make down        # Stop all services
make migrate     # Run database migrations
make health      # Check all service health endpoints
make logs        # Tail logs from all services
make clean       # Remove containers and volumes
```

## Phase Build Status

- [x] **Phase 1** — Foundation (Auth, Workspace, Project, Dashboard)
- [x] **Phase 2a** — GitHub App connection, repository selection, and queued ingestion job
- [ ] **Phase 2b** — Repository ingestion worker
- [ ] **Phase 3** — RAG Pipeline (Groq + Gemini Embeddings)
- [ ] **Phase 4** — AI Chat
- [ ] **Phase 5** — DevOps Workflows
- [ ] **Phase 6** — Log Analysis & Incident Memory
- [ ] **Phase 7** — Tool Calling & Approvals
- [ ] **Phase 8** — Billing & Usage Limits
- [ ] **Phase 9** — Production Hardening
