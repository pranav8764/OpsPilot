# Local end-to-end acceptance checklist

Use a disposable GitHub repository and non-production Vercel/Railway deployment.

## Automated gates

```bash
make test
(cd frontend && npm run lint && npm run build)
buf lint
buf generate
git diff --exit-code -- proto gen/go

CLERK_SECRET_KEY=test-only \
AI_WORKER_INTERNAL_TOKEN=test-only \
SANDBOX_RUNNER_INTERNAL_TOKEN=test-only \
docker compose config --quiet
```

CI additionally applies every migration to PostgreSQL/pgvector and builds every image.

## Live environment

- [ ] Docker daemon is running; sandbox `/health` reports `docker_ready: true`.
- [ ] All Compose services become healthy and AI `/ready` reports ready.
- [ ] Migrations 001–007 apply with `ON_ERROR_STOP=1`.
- [ ] Frontend sign-in creates/synchronizes the user and workspace.
- [ ] Gateway rejects invalid JWTs, origins, oversized bodies, and rate overflow.

## GitHub and ingestion

- [ ] Owner/admin creates install session; state is opaque, expires, and is single-use.
- [ ] Callback/replay/tamper tests behave as documented.
- [ ] Selected repo is listed and attached; browser-supplied metadata cannot spoof another repo.
- [ ] Ingestion transitions queued → running → succeeded at an exact SHA.
- [ ] `.env`, keys, binaries, dependencies, build outputs, and large files are excluded/redacted.
- [ ] Project Q&A cites real file/line ranges; weak evidence returns insufficient context.

## Incident and FIX PR

- [ ] Vercel and/or Railway provider token saves encrypted and is absent from all responses/logs.
- [ ] Runtime/build collection returns analysis but no raw provider logs to browser.
- [ ] Secret canaries in logs are redacted before persistence/model input.
- [ ] Incident history survives restart and shows ordered steps/evidence.
- [ ] Fix proposal is based on indexed SHA and contains exact diff/file reasons/verification.
- [ ] Forbidden paths, secret values, oversized changes, missing files, and no-test repos fail closed.
- [ ] Baseline and patched test runs are isolated; patched failure/timeout blocks proposal.
- [ ] Viewer/developer cannot approve; owner/admin can reject with zero GitHub writes.
- [ ] Expired/replayed approval cannot publish.
- [ ] Changing base SHA/file after review prevents publication.
- [ ] Valid approval creates a new `opspilot/fix-*` branch and draft PR only.
- [ ] Default branch SHA is unchanged and PR cannot be merged by OpsPilot.

## Lifecycle and operations

- [ ] Retention can be changed 1–365 by owner/admin and not by developer/viewer.
- [ ] Expired log rows are removed; incident summary/audit remains.
- [ ] Typed project deletion cascades project data and leaves sanitized audit metadata.
- [ ] `/metrics` contains method/status only, never token/workspace/project/log text.
- [ ] Request ID correlates gateway and AI JSON logs.
- [ ] Backup/restore drill succeeds in an isolated database.
