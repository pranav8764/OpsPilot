# GitHub Integration API Contract

This contract defines the first GitHub connection flow for OpsPilot. The backing implementation lives in the dedicated Integration Service, while the API Gateway exposes stable HTTP routes to the frontend.

## Flow Summary

```text
User clicks Connect GitHub
  -> GitHub App installation page
  -> GitHub redirects to callback with installation_id
  -> OpsPilot records installation metadata
  -> User lists accessible repositories
  -> User attaches one repository to an OpsPilot project
  -> OpsPilot creates an ingestion job
```

## Auth

All UI-facing API routes require the existing Clerk bearer token unless explicitly marked as GitHub webhook routes.

```http
Authorization: Bearer <clerk_jwt>
```

Webhook routes verify GitHub signatures with `GITHUB_APP_WEBHOOK_SECRET`.

## Endpoints

### Get GitHub App Connection Config

```http
GET /api/v1/integrations/github/config
```

Returns frontend-safe GitHub App connection information.

Response:

```json
{
  "install_url": "https://github.com/apps/opspilot/installations/new",
  "callback_url": "http://localhost:8080/api/v1/integrations/github/callback",
  "configured": true
}
```

Rules:

- Do not return private keys, webhook secrets, installation tokens, or encryption keys.
- `configured` is false if required server-side GitHub App values are missing.

### GitHub App Install Callback

```http
GET /api/v1/integrations/github/callback?installation_id=123&setup_action=install
```

GitHub redirects users here after app installation.

Behavior:

1. Validate the authenticated user.
2. Validate `installation_id`.
3. Exchange GitHub App JWT for an installation token.
4. Fetch installation/account metadata from GitHub.
5. Upsert `integrations`.
6. Upsert `github_installations`.
7. Write an `audit_logs` record.
8. Redirect user back to the dashboard or return JSON if called as an API.

Response, JSON mode:

```json
{
  "installation": {
    "id": "uuid",
    "installation_id": 123,
    "account_login": "octo-org",
    "account_type": "Organization",
    "repository_selection": "selected",
    "status": "active"
  }
}
```

### List Workspace GitHub Installations

```http
GET /api/v1/workspaces/{workspaceId}/integrations/github/installations
```

Response:

```json
{
  "installations": [
    {
      "id": "uuid",
      "installation_id": 123,
      "account_login": "octo-org",
      "account_type": "Organization",
      "repository_selection": "selected",
      "status": "active",
      "installed_at": "2026-06-24T10:00:00Z"
    }
  ]
}
```

Rules:

- User must be a member of the workspace.
- Viewer can read installation status.
- Only owner/admin should be allowed to disconnect integrations later.

### List Repositories For Installation

```http
GET /api/v1/workspaces/{workspaceId}/integrations/github/installations/{installationId}/repositories
```

Response:

```json
{
  "repositories": [
    {
      "repository_id": 123456789,
      "owner": "octo-org",
      "name": "api",
      "full_name": "octo-org/api",
      "html_url": "https://github.com/octo-org/api",
      "default_branch": "main",
      "private": true,
      "permissions": {
        "contents": "read",
        "pull_requests": "read"
      }
    }
  ]
}
```

Rules:

- Use a short-lived installation token.
- Do not persist installation access tokens.
- Cache repository metadata only after user attaches a repository to a project.

### Attach Repository To Project

```http
POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/repository
Content-Type: application/json
```

Request:

```json
{
  "github_installation_id": "uuid",
  "repository_id": 123456789,
  "owner": "octo-org",
  "name": "api",
  "full_name": "octo-org/api",
  "html_url": "https://github.com/octo-org/api",
  "clone_url": "https://github.com/octo-org/api.git",
  "default_branch": "main",
  "selected_branch": "main",
  "private": true,
  "permissions": {
    "contents": "read",
    "pull_requests": "read"
  }
}
```

Response:

```json
{
  "repository_connection": {
    "id": "uuid",
    "project_id": "uuid",
    "full_name": "octo-org/api",
    "selected_branch": "main",
    "status": "connected",
    "last_indexed_commit_sha": null,
    "last_indexed_at": null
  },
  "ingestion_job": {
    "id": "uuid",
    "status": "queued"
  }
}
```

Behavior:

1. Validate workspace membership.
2. Validate project belongs to workspace.
3. Validate repository belongs to the installation.
4. Upsert `repository_connections`.
5. Update `projects.repo_url`, `projects.repo_provider`, `projects.default_branch`, and `projects.indexing_status`.
6. Create an `ingestion_jobs` row with status `queued`.
7. Publish `repo.connected` to NATS once NATS publishing is implemented.
8. Write audit log events for repository connection and ingestion job creation.

Rules:

- One primary repository per project for MVP.
- Do not clone or index in the request path.
- Return quickly after queueing ingestion.

### Get Project Repository Connection

```http
GET /api/v1/workspaces/{workspaceId}/projects/{projectId}/repository
```

Response:

```json
{
  "repository_connection": {
    "id": "uuid",
    "full_name": "octo-org/api",
    "html_url": "https://github.com/octo-org/api",
    "selected_branch": "main",
    "status": "connected",
    "last_indexed_commit_sha": "abc123",
    "last_indexed_at": "2026-06-24T10:15:00Z"
  },
  "latest_ingestion_job": {
    "id": "uuid",
    "status": "succeeded",
    "source_commit_sha": "abc123",
    "started_at": "2026-06-24T10:12:00Z",
    "finished_at": "2026-06-24T10:15:00Z"
  }
}
```

### GitHub Webhook Receiver

```http
POST /api/v1/webhooks/github
```

Headers:

```text
X-GitHub-Event: installation
X-Hub-Signature-256: sha256=...
X-GitHub-Delivery: ...
```

Behavior:

1. Verify `X-Hub-Signature-256`.
2. Parse event type.
3. Persist only trusted event metadata.
4. Update installation/repository status where relevant.
5. Queue re-indexing for selected branch pushes later.

Initial events:

- `installation`
- `installation_repositories`
- `push`
- `pull_request`

Rules:

- Never execute instructions from webhook payload text.
- Treat webhook data as untrusted input.
- Do not trigger risky actions from webhooks in MVP.

## Error Shape

Use the existing API error style:

```json
{
  "error": "GITHUB_APP_NOT_CONFIGURED",
  "message": "GitHub App credentials are missing.",
  "request_id": "req_123"
}
```

Suggested error codes:

- `GITHUB_APP_NOT_CONFIGURED`
- `INVALID_INSTALLATION`
- `INSTALLATION_ACCESS_DENIED`
- `REPOSITORY_ACCESS_DENIED`
- `PROJECT_NOT_FOUND`
- `WORKSPACE_ACCESS_DENIED`
- `INGESTION_QUEUE_FAILED`
- `GITHUB_WEBHOOK_SIGNATURE_INVALID`

## Audit Events

Create audit log rows for:

- `github.installation.connected`
- `github.installation.updated`
- `github.installation.revoked`
- `project.repository.connected`
- `ingestion.job.created`
- `github.webhook.received`

Metadata must not include tokens, private keys, raw webhook secrets, or secret values.

## Service Boundary

The API Gateway owns HTTP routing and Clerk auth validation.

The Integration Service owns:

- GitHub App configuration,
- GitHub installation metadata,
- provider credential handling,
- GitHub webhook verification,
- repository listing through short-lived installation tokens,
- repository attachment orchestration with Project Service/database updates.
