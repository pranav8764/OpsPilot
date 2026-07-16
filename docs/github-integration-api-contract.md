# HTTP API contract

All `/api/v1` routes except GitHub callback/webhook require `Authorization: Bearer <Clerk JWT>`. JSON errors use `{ "error": "CODE", "message": "safe message" }`; upstream credentials, raw provider errors, and model internals are not exposed. `X-Request-ID` is returned on gateway requests.

## Workspaces and projects

| Method | Route | Purpose |
| --- | --- | --- |
| `GET/POST` | `/api/v1/workspaces` | List/create workspaces |
| `GET` | `/api/v1/workspaces/{workspaceId}` | Workspace including role/plan/retention |
| `PUT` | `/api/v1/workspaces/{workspaceId}/settings` | Owner/admin log retention, 1–365 days |
| `GET` | `/api/v1/workspaces/{workspaceId}/usage` | Plan entitlements and atomic usage |
| `GET/POST` | `/api/v1/workspaces/{workspaceId}/projects` | List/create projects |
| `DELETE` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}` | Owner/admin permanent cascading deletion |

## GitHub

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/integrations/github/config` | Frontend-safe configuration status/URLs |
| `POST` | `/api/v1/workspaces/{workspaceId}/integrations/github/install-session` | Create one-time ten-minute opaque install state |
| `GET` | `/api/v1/integrations/github/callback?installation_id=…&state=…` | Public one-time callback, then controlled redirect |
| `POST` | `/api/v1/webhooks/github` | Public HMAC-verified webhook receiver |
| `GET` | `/api/v1/workspaces/{workspaceId}/integrations/github/installations` | List active workspace installations |
| `GET` | `/api/v1/workspaces/{workspaceId}/integrations/github/installations/{id}/repositories` | List repositories with short-lived installation token |
| `GET/POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/repository` | Read/attach canonical server-validated repository and queue ingestion |

Repository attachment accepts selected IDs/branch from the UI as hints. The Integration Service refetches canonical GitHub metadata, verifies the installation/workspace/project, and stores trusted owner/name/URLs/branch.

## Log providers and incidents

| Method | Route | Purpose |
| --- | --- | --- |
| `GET/POST` | `/api/v1/workspaces/{workspaceId}/integrations/log-providers` | List metadata/save encrypted Vercel or Railway credential |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/analyze` | Redact/analyze ≤1 MiB manual logs |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/collect` | Server-side bounded provider collection + analysis |
| `GET` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents` | Durable incident/workflow/proposal/approval/PR history |

Provider-save responses return connection ID/provider/display metadata only. Tokens and encrypted envelopes are never returned.

Example collection request:

```json
{
  "integration_id": "uuid",
  "log_kind": "runtime",
  "limit": 300
}
```

## Repository intelligence and fix workflow

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/query` | Cited tenant-scoped Q&A, top-k 1–20 |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/incidents/{incidentId}/fix-proposal` | Pinned policy/sandbox-verified diff + pending approval |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/approvals/{approvalId}/decision` | Owner/admin approve or reject exact action |

Approval request:

```json
{
  "decision": "approve",
  "reason": "Reviewed exact diff and sandbox evidence"
}
```

Approval does not accept a replacement diff, branch, repository, or commit. Those come from the hashed stored proposal. A successful approval returns the draft PR URL/number/branch. Rejected, expired, replayed, stale, or hash-mismatched approvals produce no repository write.

## Deployment readiness

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/deployment/readiness` | Deterministic pinned-source findings/score |
| `POST` | `/api/v1/workspaces/{workspaceId}/projects/{projectId}/deployment/generate` | Dockerfile/Compose/env-name drafts; no write |

## Limits

- Question: 4,000 characters; `top_k` ≤20.
- Manual logs: 1 MiB.
- Provider logs: maximum 300 normalized records per request.
- Repository: 3,000 safe text files, 30 MiB total, 512 KiB/file.
- Worker response: 5 MiB at gateway.
- Approval lifetime: ten minutes for install state; configured expiry for fix actions.
- Rate limit: 120 requests/minute by default per bearer fingerprint/IP.
- Monthly plan limits and indexed storage are enforced atomically and surfaced by `/usage`.

## Internal APIs

Gateway → AI endpoints require `AI_WORKER_INTERNAL_TOKEN`; AI → runner requires the distinct `SANDBOX_RUNNER_INTERNAL_TOKEN`. They are private-network APIs and fail closed if tokens are missing. `/health`, `/ready`, and `/metrics` contain no tenant payloads but should still remain private in production.
