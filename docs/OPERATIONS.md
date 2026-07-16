# Operations and production checklist

## Local prerequisites

- Docker Desktop with the daemon running. The sandbox runner uses the Docker socket.
- Go 1.22+, Python 3.12+, Node.js 22+, npm, and `buf` for local development.
- A Clerk application and GitHub App.
- Optional Groq and Gemini keys. With `EMBEDDING_PROVIDER=local` and no Groq key, OpsPilot uses deterministic local embeddings and extractive answers.

## Required secrets

Copy `.env.example` to `.env`. Generate independent values:

```bash
openssl rand -base64 32  # CREDENTIAL_ENCRYPTION_KEY: exactly 32 decoded bytes
openssl rand -hex 32     # AI_WORKER_INTERNAL_TOKEN
openssl rand -hex 32     # SANDBOX_RUNNER_INTERNAL_TOKEN
openssl rand -hex 32     # GITHUB_APP_WEBHOOK_SECRET, if generating yourself
```

Do not reuse these values. `CREDENTIAL_ENCRYPTION_KEY` cannot be changed casually: existing provider connections must be decrypted/re-encrypted during a planned rotation.

For container deployment, prefer `GITHUB_APP_PRIVATE_KEY_BASE64` from a secret manager. Path mode is convenient for a locally run service, but a non-root container must have read permission on the mounted file.

```bash
base64 < secrets/github-app.private-key.pem | tr -d '\n'
```

Never put production values in `NEXT_PUBLIC_*`; those are bundled into browser code.

## Start and migrate

```bash
docker compose up --build -d
make migrate
make health
```

Migration files are ordered and idempotent. Always run `psql` with `ON_ERROR_STOP=1` in production and snapshot the database before schema changes. Initial container startup applies migrations only to a new PostgreSQL volume; `make migrate` applies them to an existing volume.

Compose starts the production-style frontend at `http://localhost:3000`. For frontend hot reload, stop only that service and run the development server:

```bash
docker compose stop frontend
cd frontend
npm ci
npm run dev
```

## Health and metrics

| Service | Liveness | Readiness/notes |
| --- | --- | --- |
| Gateway | `GET :8080/health` | Compose waits for all downstream health checks |
| Auth | `GET :8081/health` | Includes database connectivity |
| Workspace | `GET :8082/health` | Includes database connectivity |
| Project | `GET :8083/health` | Includes database connectivity |
| Integration | `GET :8084/health` | Includes database connectivity |
| AI worker | `GET :8090/health` | `GET /ready` checks PostgreSQL and sandbox runner |
| Sandbox runner | `GET :8092/health` | `ok` only with configured token and usable Docker daemon |

Gateway and AI worker expose Prometheus text on `/metrics`. Labels are intentionally limited to HTTP method and status, so workspace/project IDs never become metric labels. Scrape these endpoints only on the private network.

Recommended alerts:

- readiness unavailable for 2 minutes;
- 5xx > 2% over 5 minutes;
- p95 request latency above endpoint SLO;
- ingestion jobs queued > 10 minutes or in `dead_letter`;
- workflow/tool failures or approval publish failures;
- sandbox timeouts/unavailability;
- provider 401/403 increases;
- PostgreSQL storage/connection saturation and NATS consumer lag;
- retention cleanup failure;
- unexpected rate-limit/usage-limit spikes.

The gateway validates or generates a safe `X-Request-ID`, logs it, returns it, and forwards it to the AI worker. Incident/workflow/tool IDs provide durable business-level correlation.

## Log retention and deletion

- Each workspace defaults to 30 days for stored log bodies and can select 1–365 days in the dashboard.
- The AI background worker deletes expired `logs` rows hourly by default (`RETENTION_CLEANUP_SECONDS`). Incident summaries retain their evidence metadata and set the deleted log reference to null.
- Owners/admins can permanently delete a project after typing its exact name. The database cascades indexed source, logs, incidents, workflows, proposals, approvals, usage links, and integration associations. The audit row retains only project ID/name metadata.
- Workspace deletion is owner-only and cascades workspace data.

For stricter customers, add a no-persistence/zero-day mode only after changing the schema and incident evidence contract; the current minimum is one day.

## Backups and restore

For production use managed PostgreSQL with point-in-time recovery. At minimum:

```bash
pg_dump --format=custom --no-owner --dbname="$DATABASE_URL" > opspilot.dump
pg_restore --clean --if-exists --no-owner --dbname="$RESTORE_DATABASE_URL" opspilot.dump
```

Test restoration into an isolated database on a schedule. Confirm row counts and sample workspace/project/incident relationships, then run application smoke tests. Backups contain private code chunks and redacted log content and must be encrypted and access-controlled.

NATS is a wakeup/retry accelerator, not the source of truth for ingestion. Losing its volume is recoverable from queued database jobs. PostgreSQL is not.

## Secret rotation

- **Clerk key:** rotate in Clerk, deploy all validating services, invalidate old key per Clerk procedure.
- **AI/runner internal tokens:** deploy consumers accepting the new token in a coordinated window; current code accepts one value, so use rolling replacement with no public exposure.
- **GitHub webhook secret/private key:** add a new GitHub private key, deploy, then revoke the old key. Coordinate webhook-secret change with GitHub App settings.
- **Provider tokens:** save a new connection/credential through the UI, verify collection, then revoke the old provider token.
- **Credential encryption key:** requires an explicit re-encryption migration; do not merely change the environment variable.

Audit that no old secret appears in logs, images, Compose output, shell history, or version control.

## Production topology

The supplied Compose file is for development/single-host evaluation. A production deployment should:

- terminate TLS at a trusted ingress and set `GATEWAY_ENV=production` for HSTS;
- use exact HTTPS `FRONTEND_URL` and `CORS_ALLOWED_ORIGINS` values;
- keep gRPC, NATS, AI, metrics, and health ports private; expose only frontend and gateway;
- use managed PostgreSQL/pgvector, encrypted storage, PITR, connection pooling, and restricted database roles;
- use a secret manager/workload identity instead of `.env` and bind mounts;
- run images by digest, scan/sign them, generate SBOMs, and patch base images;
- run gateway/domain/AI containers read-only, non-root, capability-dropped, and with CPU/memory/PID limits;
- place sandbox runners on dedicated disposable nodes with no database/provider/Clerk secrets;
- replace the Docker socket with a dedicated sandbox service or isolated VM/microVM pool as scale/risk grows;
- use distributed rate limiting, OpenTelemetry collection, centralized JSON logs, dashboards, and SLOs;
- configure egress allowlists: integration → GitHub/Vercel/Railway, AI → approved model APIs, runner setup → package registries, test phase → none;
- configure network policy so the runner is callable only by the AI worker.

The runner’s Docker socket is equivalent to host-root authority. `read_only`, dropped capabilities, and API validation reduce accidental container-level risk but do not neutralize a compromised runner. Isolation onto a disposable host is mandatory for serious production use.

## GitHub production controls

- Install the GitHub App only on selected repositories.
- Grant `Metadata: read`, `Contents: read/write`, and `Pull requests: read/write`; do not grant workflows or administration.
- Protect the default branch with required reviews and checks.
- Prevent the OpsPilot identity from bypassing branch protection.
- Require CODEOWNERS where appropriate.
- Consider GitHub Actions’ approval controls for first-time/agent-authored workflow runs.
- Monitor app-installation, ref-creation, and PR audit events.

## Verification commands

```bash
# Go + Python policy/unit/contract suites
make test

# Frontend component, static, and production-build verification
(cd frontend && npm test && npm run lint && npm run build && npm audit --omit=dev --audit-level=high)

# Protobuf reproducibility
buf lint
buf generate
git diff --exit-code -- proto gen/go

# Compose schema/interpolation
CLERK_SECRET_KEY=test-only \
AI_WORKER_INTERNAL_TOKEN=test-only \
SANDBOX_RUNNER_INTERNAL_TOKEN=test-only \
docker compose config --quiet
```

With Docker running, also build every image and execute a live test repository whose baseline fails and patched version passes. Confirm that:

1. Raw provider token/logs never appear in browser/network responses or service logs.
2. The incident has cited source and durable steps.
3. The proposal is pinned and cannot publish before approval.
4. Rejection/expiry/replay produces no GitHub write.
5. A base-branch change after review makes publication fail.
6. Approval creates only a new `opspilot/fix-*` ref and a draft PR.
7. The default branch SHA never changes.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Compose rejects configuration | Both internal tokens and Clerk key are required; use `docker compose config` |
| AI worker is unhealthy | PostgreSQL, runner `/health`, Docker daemon, token equality, and `SANDBOX_RUNNER_URL` |
| Ingestion stays queued | AI background worker, NATS stream/consumer, database job attempts/events, GitHub key/app ID |
| GitHub callback fails | One-time state has not expired/been consumed; callback URL exactly matches; installation is valid |
| Provider collection fails | Token type/scope, deployment/project IDs, provider status; upstream response bodies are intentionally hidden |
| Fix is blocked | Incident confidence/evidence, indexed SHA, forbidden path/secret policy, detected tests, runner availability/output |
| Approval cannot publish | Owner/admin role, approval expiry/status/hash, base SHA/file hashes, GitHub Contents/PR write permission |
| Integration cannot read PEM | Prefer base64 secret; otherwise ensure the non-root container UID can read the mounted file |
