from __future__ import annotations

import json
import re
from dataclasses import asdict, dataclass
from pathlib import PurePosixPath

import yaml

from .domain import IngestionJob, RepositoryFile


ENV_PATTERNS = [
    re.compile(r"process\.env\.([A-Z][A-Z0-9_]+)"),
    re.compile(r"(?:os\.getenv|os\.environ\.get)\([\"']([A-Z][A-Z0-9_]+)[\"']"),
    re.compile(r"os\.environ\[[\"']([A-Z][A-Z0-9_]+)[\"']\]"),
    re.compile(r"System\.getenv\([\"']([A-Z][A-Z0-9_]+)[\"']\)"),
]


@dataclass(frozen=True)
class RepositoryTarget:
    workspace_id: str
    project_id: str
    owner: str
    repository: str
    branch: str
    installation_id: int
    indexed_commit_sha: str


@dataclass(frozen=True)
class Finding:
    code: str
    severity: str
    title: str
    detail: str
    remediation: str
    citations: list[str]


class DeploymentStore:
    def __init__(self, database_url: str):
        self.database_url = database_url

    def load_target(self, workspace_id: str, project_id: str) -> RepositoryTarget:
        import psycopg
        from psycopg.rows import dict_row

        with psycopg.connect(self.database_url, row_factory=dict_row) as connection:
            row = connection.execute(
                """
                SELECT repository.owner, repository.name, repository.selected_branch,
                       installation.installation_id,
                       COALESCE(repository.last_indexed_commit_sha, '') AS indexed_commit_sha
                FROM repository_connections AS repository
                JOIN github_installations AS installation ON installation.id = repository.github_installation_id
                WHERE repository.workspace_id = %s AND repository.project_id = %s
                  AND repository.status = 'connected' AND installation.status = 'active'
                """,
                (workspace_id, project_id),
            ).fetchone()
        if row is None:
            raise ValueError("an active repository connection is required")
        return RepositoryTarget(
            workspace_id, project_id, row["owner"], row["name"], row["selected_branch"],
            row["installation_id"], row["indexed_commit_sha"],
        )


def _line_citation(path: str, content: str, needle: str = "") -> str:
    line = 1
    if needle:
        for index, value in enumerate(content.splitlines(), start=1):
            if needle in value:
                line = index
                break
    return f"{path}:L{line}"


def _file_map(files: list[RepositoryFile]) -> dict[str, str]:
    return {item.path: item.content for item in files}


def _root_manifest(files: dict[str, str], name: str) -> tuple[str, str] | None:
    candidates = [(path, content) for path, content in files.items() if PurePosixPath(path).name == name]
    if not candidates:
        return None
    return sorted(candidates, key=lambda item: (len(PurePosixPath(item[0]).parts), item[0]))[0]


def detect_stack(files: dict[str, str]) -> dict:
    stack = {"kind": "unknown", "root": ".", "framework": "unknown", "build": "", "start": "", "port": 8080}
    package_manifest = _root_manifest(files, "package.json")
    if package_manifest:
        path, content = package_manifest
        try:
            package = json.loads(content)
        except ValueError:
            package = {}
        dependencies = {**package.get("dependencies", {}), **package.get("devDependencies", {})} if isinstance(package, dict) else {}
        scripts = package.get("scripts", {}) if isinstance(package, dict) else {}
        framework = "nextjs" if "next" in dependencies else "node"
        return {
            "kind": "node", "root": str(PurePosixPath(path).parent), "framework": framework,
            "build": "npm run build" if "build" in scripts else "",
            "start": "npm start" if "start" in scripts else "",
            "port": 3000 if framework == "nextjs" else 8080,
            "manifest": path,
        }
    go_manifest = _root_manifest(files, "go.mod")
    if go_manifest:
        return {**stack, "kind": "go", "framework": "go", "root": str(PurePosixPath(go_manifest[0]).parent), "build": "go build ./...", "start": "./app", "manifest": go_manifest[0]}
    requirements = _root_manifest(files, "requirements.txt")
    pyproject = _root_manifest(files, "pyproject.toml")
    if requirements or pyproject:
        manifest = requirements or pyproject
        joined = "\n".join(files.values()).lower()
        framework = "fastapi" if "fastapi" in joined else "django" if "django" in joined else "python"
        start = "uvicorn main:app --host 0.0.0.0 --port ${PORT:-8080}" if framework == "fastapi" else "python main.py"
        return {**stack, "kind": "python", "framework": framework, "root": str(PurePosixPath(manifest[0]).parent), "start": start, "manifest": manifest[0]}
    return stack


def discover_env_names(files: dict[str, str]) -> list[str]:
    names = set()
    for path, content in files.items():
        if path.lower().startswith(".env"):
            continue
        for pattern in ENV_PATTERNS:
            names.update(pattern.findall(content))
    return sorted(names)[:100]


def readiness_report(commit_sha: str, files: list[RepositoryFile]) -> dict:
    file_map = _file_map(files)
    stack = detect_stack(file_map)
    findings: list[Finding] = []
    dockerfiles = [path for path in file_map if PurePosixPath(path).name.lower() == "dockerfile"]
    if not dockerfiles:
        findings.append(Finding("missing_dockerfile", "blocker", "No Dockerfile", "No container build definition was found.", "Generate and review a least-privilege multi-stage Dockerfile.", [stack.get("manifest", "repository:L1")]))
    else:
        docker_path = dockerfiles[0]
        docker = file_map[docker_path]
        if re.search(r"(?im)^\s*user\s+", docker) is None:
            findings.append(Finding("container_runs_as_root", "warning", "Container may run as root", "The Dockerfile has no USER instruction.", "Add a non-root runtime user.", [_line_citation(docker_path, docker)]))
        if re.search(r"(?im)^\s*healthcheck\s+", docker) is None:
            findings.append(Finding("missing_container_healthcheck", "warning", "No container health check", "The Dockerfile does not declare HEALTHCHECK.", "Add a lightweight application health check.", [_line_citation(docker_path, docker)]))

    manifest = stack.get("manifest")
    if stack["kind"] == "unknown":
        findings.append(Finding("unknown_runtime", "blocker", "Runtime not detected", "No supported root package.json, go.mod, requirements.txt, or pyproject.toml was found.", "Add an explicit runtime manifest or configure the project root.", []))
    elif not stack.get("start"):
        findings.append(Finding("missing_start_command", "blocker", "Start command not detected", "The runtime manifest has no supported production start command.", "Add an explicit production start script.", [_line_citation(manifest, file_map.get(manifest, ""))]))
    if stack["kind"] == "node" and "package-lock.json" not in {PurePosixPath(path).name for path in file_map}:
        findings.append(Finding("missing_lockfile", "warning", "No npm lockfile", "Reproducible dependency resolution cannot be guaranteed.", "Commit package-lock.json.", [_line_citation(manifest, file_map.get(manifest, ""))]))

    env_names = discover_env_names(file_map)
    env_examples = [path for path in file_map if PurePosixPath(path).name in {".env.example", ".env.sample"}]
    if env_names and not env_examples:
        findings.append(Finding("missing_env_example", "warning", "No environment-variable template", f"Code references {len(env_names)} environment variables but no .env.example/.env.sample is indexed.", "Add names-only placeholders without secret values.", [_line_citation(manifest, file_map.get(manifest, ""))]))

    health_citations = []
    for path, content in file_map.items():
        match = re.search(r"[\"']/(?:health|healthz|ready|readiness)[\"']|/actuator/health", content, re.IGNORECASE)
        if match:
            health_citations.append(_line_citation(path, content, match.group(0)))
            break
    if not health_citations:
        findings.append(Finding("missing_health_endpoint", "warning", "Health endpoint not detected", "No common health/readiness route was found in indexed source.", "Add an unauthenticated dependency-aware health endpoint.", [manifest + ":L1"] if manifest else []))

    port_detected = any(re.search(r"\bPORT\b|listen\s*\(|EXPOSE\s+\d+", content, re.IGNORECASE) for content in file_map.values())
    if not port_detected:
        findings.append(Finding("port_not_configurable", "warning", "Configurable port not detected", "The application may not bind the provider-supplied PORT.", "Read PORT from the environment and bind to 0.0.0.0.", [manifest + ":L1"] if manifest else []))

    penalty = sum(25 if item.severity == "blocker" else 8 if item.severity == "warning" else 2 for item in findings)
    score = max(0, 100 - penalty)
    if stack["framework"] == "nextjs":
        recommendations = [
            {"provider": "vercel", "fit": "high", "reason": "Native fit for a detected Next.js application."},
            {"provider": "railway", "fit": "medium", "reason": "Suitable when a persistent container runtime is preferred."},
        ]
    else:
        recommendations = [
            {"provider": "railway", "fit": "high", "reason": "Persistent container runtime fits the detected backend stack."},
            {"provider": "vercel", "fit": "low", "reason": "Use only if the application can run within serverless constraints."},
        ]
    return {
        "commit_sha": commit_sha,
        "score": score,
        "ready": not any(item.severity == "blocker" for item in findings),
        "stack": stack,
        "required_env_names": env_names,
        "findings": [asdict(item) for item in findings],
        "provider_recommendations": recommendations,
    }


def generate_deployment_files(commit_sha: str, files: list[RepositoryFile]) -> dict:
    file_map = _file_map(files)
    stack = detect_stack(file_map)
    if stack["kind"] == "unknown" or not stack.get("start"):
        raise ValueError("a supported runtime and production start command are required")
    root = stack["root"]
    root_copy = "" if root == "." else root.rstrip("/") + "/"
    if stack["kind"] == "node":
        build = "RUN npm run build\n" if stack.get("build") else ""
        dockerfile = f"""# Generated from commit {commit_sha}; review before use.
FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY {root_copy}package*.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY {root_copy} .
{build}RUN chown -R node:node /app
USER node
ENV NODE_ENV=production PORT={stack['port']}
EXPOSE {stack['port']}
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s CMD node -e "fetch('http://127.0.0.1:' + process.env.PORT + '/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["npm", "start"]
"""
    elif stack["kind"] == "python":
        requirement = (root_copy + "requirements.txt") if any(PurePosixPath(path).name == "requirements.txt" for path in file_map) else "requirements.txt"
        command = stack["start"].replace("${PORT:-8080}", "8080").split()
        dockerfile = f"""# Generated from commit {commit_sha}; review before use.
FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PORT=8080
WORKDIR /app
RUN groupadd --system app && useradd --system --gid app app
COPY {requirement} ./requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
COPY {root_copy} .
RUN chown -R app:app /app
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"
CMD {json.dumps(command)}
"""
    else:
        go_sum_copy = f" {root_copy}go.sum" if any(PurePosixPath(path).name == "go.sum" for path in file_map) else ""
        dockerfile = f"""# Generated from commit {commit_sha}; review before use.
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY {root_copy}go.mod{go_sum_copy} ./
RUN go mod download
COPY {root_copy} .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app .
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
"""

    env_names = discover_env_names(file_map)
    env_example = "\n".join(f"{name}=" for name in env_names if name not in {"NODE_ENV"})
    if "PORT" not in env_names:
        env_example = (env_example + "\n" if env_example else "") + f"PORT={stack['port']}"
    compose = {
        "services": {
            "app": {
                "build": {"context": ".", "dockerfile": "Dockerfile"},
                "ports": [f"{stack['port']}:{stack['port']}"],
                "env_file": [".env"],
                "restart": "unless-stopped",
                "read_only": True,
                "tmpfs": ["/tmp"],
                "security_opt": ["no-new-privileges:true"],
            }
        }
    }
    compose_yaml = yaml.safe_dump(compose, sort_keys=False)
    yaml.safe_load(compose_yaml)
    return {
        "commit_sha": commit_sha,
        "stack": stack,
        "dockerfile": dockerfile,
        "compose_yaml": compose_yaml,
        "env_example": env_example + "\n",
        "assumptions": [
            f"The application root is {root}.",
            f"The service listens on port {stack['port']} and exposes GET /health.",
            "Secret values are intentionally omitted; only variable names are generated.",
        ],
        "citations": [stack["manifest"] + ":L1"],
    }


class DeploymentService:
    def __init__(self, store: DeploymentStore, fetch_repository):
        self.store = store
        self.fetch_repository = fetch_repository

    def _load(self, workspace_id: str, project_id: str):
        target = self.store.load_target(workspace_id, project_id)
        job = IngestionJob(
            "deployment-analysis", workspace_id, project_id, "", target.owner, target.repository,
            target.branch, target.installation_id, 1, 1,
        )
        commit_sha, files = self.fetch_repository(job)
        if target.indexed_commit_sha and commit_sha != target.indexed_commit_sha:
            raise ValueError("repository changed after indexing; re-index before deployment analysis")
        return commit_sha, files

    def scan(self, workspace_id: str, project_id: str) -> dict:
        return readiness_report(*self._load(workspace_id, project_id))

    def generate(self, workspace_id: str, project_id: str) -> dict:
        return generate_deployment_files(*self._load(workspace_id, project_id))
