# GitHub App setup

OpsPilot uses a GitHub App, not a user PAT. Install it only on repositories that OpsPilot may read and for which an approved FIX draft PR may be opened.

## URLs

For local development:

| Setting | Value |
| --- | --- |
| Homepage | `http://localhost:3000` |
| Callback | `http://localhost:8080/api/v1/integrations/github/callback` |
| Webhook | Public HTTPS tunnel + `/api/v1/webhooks/github` |
| Setup | `http://localhost:3000/dashboard` |

GitHub cannot deliver webhooks to localhost. Disable webhook delivery temporarily or use a public HTTPS tunnel. Production callback/webhook URLs must be HTTPS and exactly match `.env`.

## Minimum repository permissions

| Permission | Access | Reason |
| --- | --- | --- |
| Metadata | Read | Repository/install identity (implicit/required) |
| Contents | Read and write | Read pinned source; create blobs/tree/commit/new fix branch |
| Pull requests | Read and write | Open a draft PR and expose its URL/state |

Do **not** grant Administration, Actions, Workflows, Deployments, Environments, Secrets, or Issues for this release. OpsPilot blocks `.github/workflows` changes and does not merge or update the base branch.

GitHub recommends choosing only the minimum GitHub App permissions: <https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app>.

Subscribe to:

- `installation`
- `installation_repositories`
- `push`
- `pull_request`

Only verified installation state and repository metadata affect the control plane. Commit messages, PR text, and repository files are treated as untrusted data.

## Environment

```bash
CREDENTIAL_ENCRYPTION_KEY=
GITHUB_APP_ID=
GITHUB_APP_SLUG=
GITHUB_APP_PRIVATE_KEY_PATH=
GITHUB_APP_PRIVATE_KEY_BASE64=
GITHUB_APP_WEBHOOK_SECRET=
GITHUB_APP_INSTALL_URL=https://github.com/apps/YOUR_APP/installations/new
GITHUB_APP_CALLBACK_URL=http://localhost:8080/api/v1/integrations/github/callback
GITHUB_WEBHOOK_URL=https://YOUR_TUNNEL/api/v1/webhooks/github
GITHUB_API_VERSION=2026-03-10
FRONTEND_URL=http://localhost:3000
```

Generate unrelated keys for credential encryption and webhook validation. Use either private-key path or base64, not both. For non-root containers, base64 from a secret manager is easiest:

```bash
openssl rand -base64 32
base64 < secrets/github-app.private-key.pem | tr -d '\n'
```

The first command is `CREDENTIAL_ENCRYPTION_KEY`; the second is `GITHUB_APP_PRIVATE_KEY_BASE64`.

## Secure installation flow

1. An authenticated owner/admin asks OpsPilot for an install session.
2. OpsPilot creates 32 random bytes, stores only SHA-256, binds the row to user/workspace, and sets a ten-minute expiry.
3. Browser redirects to the GitHub installation URL with opaque `state`.
4. Public callback consumes the state exactly once, derives user/workspace from the server row, validates the installation through GitHub, and stores metadata.
5. Callback redirects only to the configured frontend origin/workspace path.

Never construct workspace/user state in browser JavaScript and never log `state`, webhook signatures, private keys, installation tokens, or provider credentials.

## Verification

- Install on a disposable repository and confirm only selected repositories are listed.
- Replay the callback: it must fail.
- Use expired/tampered state: it must fail.
- Send a webhook with invalid/missing `X-Hub-Signature-256`: it must fail without logging the signature.
- Attach a repository and confirm a durable ingestion job succeeds at a pinned SHA.
- Generate a verified proposal, reject it, and confirm no ref/PR appears.
- Approve a fresh proposal and confirm one new `opspilot/fix-*` ref and one **draft** PR appear.
- Confirm the base branch SHA is unchanged.
- Change the base/file after proposal and confirm stale publication fails.
