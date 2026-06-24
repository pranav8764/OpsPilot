# GitHub App Setup

This guide prepares the external GitHub App values required by OpsPilot's next implementation milestone:

```text
GitHub-connected project -> repository ingestion -> source-cited project Q&A
```

Do not commit generated private keys, webhook secrets, or filled `.env` files.

## Required GitHub App Settings

Create a GitHub App named `OpsPilot` or an environment-specific variant such as `OpsPilot Local`.

Use these development URLs for local setup:

| Setting | Value |
| --- | --- |
| Homepage URL | `http://localhost:3000` |
| Callback URL | `http://localhost:8080/api/v1/integrations/github/callback` |
| Webhook URL | `http://localhost:8080/api/v1/webhooks/github` |
| Setup URL | `http://localhost:3000/dashboard` |

For local webhook delivery, GitHub will need a public tunnel URL later. When using a tunnel, replace the webhook URL with:

```text
https://YOUR_TUNNEL_DOMAIN/api/v1/webhooks/github
```

## Repository Permissions

Start with the minimum permissions needed for repository selection, ingestion, and future PR-based fixes.

| Permission | Access | Why OpsPilot needs it |
| --- | --- | --- |
| Metadata | Read-only | Required for repository identity and installation metadata. |
| Contents | Read-only for first milestone | Read repository files for indexing. Increase to read/write only when PR-based config fixes are implemented. |
| Pull requests | Read-only for first milestone | Prepare for PR visibility. Increase to read/write only when OpsPilot creates draft PRs. |
| Issues | Read-only or none for first milestone | Not needed until GitHub issue creation is implemented. |
| Checks | Read-only or none for first milestone | Not needed until CI/check annotations are implemented. |
| Actions | Read-only or none for first milestone | Not needed until workflow/debug integration is implemented. |

## Webhook Events

Enable these events first:

| Event | Why |
| --- | --- |
| Installation | Track new, suspended, unsuspended, and deleted app installations. |
| Installation repositories | Track repositories added to or removed from an installation. |
| Push | Later re-index when the selected branch changes. |
| Pull request | Later update workflow/PR context. |

## Values Needed In `.env`

After creating the GitHub App, fill these values in `.env` using `.env.example` as the template:

```bash
CREDENTIAL_ENCRYPTION_KEY=
GITHUB_APP_ID=
GITHUB_APP_SLUG=
GITHUB_APP_PRIVATE_KEY_PATH=
GITHUB_APP_PRIVATE_KEY_BASE64=
GITHUB_APP_WEBHOOK_SECRET=
GITHUB_APP_INSTALL_URL=
GITHUB_APP_CALLBACK_URL=http://localhost:8080/api/v1/integrations/github/callback
GITHUB_WEBHOOK_URL=http://localhost:8080/api/v1/webhooks/github
NEXT_PUBLIC_GITHUB_APP_INSTALL_URL=
```

Generate the credential encryption key locally:

```bash
openssl rand -base64 32
```

Use either `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY_BASE64`.

For local development, the path-based option is easier:

```text
GITHUB_APP_PRIVATE_KEY_PATH=./secrets/github-app.private-key.pem
```

Create the `secrets/` directory locally and keep it uncommitted.

## First Backend Milestone

The dedicated Integration Service should implement:

1. GitHub App install callback.
2. Webhook receiver with signature verification.
3. Installation token exchange.
4. List repositories accessible to an installation.
5. Attach a selected repository to an OpsPilot project.
6. Create an initial `ingestion_jobs` row for that project.
