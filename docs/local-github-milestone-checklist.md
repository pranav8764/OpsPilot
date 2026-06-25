# Local GitHub Milestone Checklist

Use this checklist for the next milestone:

```text
GitHub-connected project -> repository ingestion -> source-cited project Q&A
```

## [x] 1. Environment

Create a local `.env` from the example:

```bash
cp .env.example .env
```

Fill the required values:

- `CLERK_SECRET_KEY`
- `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY`
- `CREDENTIAL_ENCRYPTION_KEY`
- `GITHUB_APP_ID`
- `GITHUB_APP_SLUG`
- `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY_BASE64`
- `GITHUB_APP_WEBHOOK_SECRET`
- `GITHUB_APP_INSTALL_URL`
- `NEXT_PUBLIC_GITHUB_APP_INSTALL_URL`

Generate the encryption key:

```bash
openssl rand -base64 32
```

## [x] 2. GitHub App

Create the GitHub App using [github-app-setup.md](./github-app-setup.md).

For local development:

- use path-based private key loading if possible;
- store the key under `secrets/`;
- keep `secrets/` and `.env` uncommitted;
- use a tunnel URL only when testing live webhooks from GitHub.

## [x] 3. Infrastructure

Start Docker Desktop, then run:

```bash
make up
make migrate
make health
```

Current known local caveat: if Docker Desktop is not running, `make migrate` cannot connect to the Docker daemon or fallback Postgres.

## [x] 4. Backend Contract

Implement the routes defined in [github-integration-api-contract.md](./github-integration-api-contract.md).

The implementation must:

- keep GitHub installation tokens short-lived and unpersisted;
- verify GitHub webhook signatures;
- store installation and repository metadata in the new migration tables;
- create `ingestion_jobs` when a repo is attached;
- avoid cloning/indexing inside the HTTP request path.

## [x] 5. Frontend Contract

Replace the current project creation card's "Connect Repository" placeholder with:

1. Connect GitHub button.
2. Installation/account selector.
3. Repository selector.
4. Attach-to-project action.
5. Repository status panel showing:
   - full repo name,
   - selected branch,
   - connection status,
   - latest ingestion job status,
   - last indexed commit when available.

## [x] 6. Verification

Minimum checks for this milestone slice:

```bash
GOCACHE=/private/tmp/opspilot-gocache make test
docker compose config
make migrate
```

After endpoints exist:

- install GitHub App on a test repository;
- confirm installation is stored in `github_installations`;
- list repositories through the API;
- attach one repo to a project;
- confirm `repository_connections` row exists;
- confirm `ingestion_jobs` row is queued;
- confirm project UI shows connected repo state.

Verified locally:

```text
repository_connections.status = connected
repository_connections.full_name = pranav8764/ParkIntel
repository_connections.selected_branch = main

ingestion_jobs.status = queued
ingestion_jobs.source_branch = main
```

The GitHub connection slice is complete through queued ingestion job creation.

## [ ] 7. Repository Ingestion Worker

Next milestone:

```text
queued ingestion job -> fetch repository contents -> index chunks -> source-cited Q&A
```

The worker should:

1. Poll or subscribe for `ingestion_jobs` with `status = 'queued'`.
2. Load the linked `repository_connections` and `github_installations` records.
3. Generate a short-lived GitHub installation token.
4. Fetch repository files for the selected branch.
5. Store indexed file/chunk metadata.
6. Mark the job `succeeded` or `failed`.
7. Update project indexing status for the UI.

## Service Boundary

GitHub integration lives in the dedicated Integration Service.

The API Gateway should expose frontend-facing HTTP routes and call Integration Service over gRPC.
