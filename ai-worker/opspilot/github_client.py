from __future__ import annotations

import base64
import time
from pathlib import Path
from urllib.parse import quote

import httpx

from .config import Settings
from .domain import RepositoryFile
from .security import path_policy


class GitHubAPIError(RuntimeError):
    pass


class GitHubRepositorySource:
    def __init__(self, settings: Settings, client: httpx.Client | None = None):
        self.settings = settings
        self._provided_client = client

    def _private_key(self) -> bytes:
        if self.settings.github_private_key_base64:
            try:
                return base64.b64decode(self.settings.github_private_key_base64, validate=True)
            except ValueError as exc:
                raise GitHubAPIError("invalid base64 GitHub private key") from exc
        if self.settings.github_private_key_path:
            try:
                return Path(self.settings.github_private_key_path).read_bytes()
            except OSError as exc:
                raise GitHubAPIError("unable to read GitHub private key") from exc
        raise GitHubAPIError("GitHub private key is not configured")

    def _app_jwt(self) -> str:
        import jwt

        now = int(time.time())
        return jwt.encode(
            {"iat": now - 60, "exp": now + 540, "iss": self.settings.github_app_id},
            self._private_key(),
            algorithm="RS256",
        )

    def _headers(self, token: str) -> dict[str, str]:
        return {
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "User-Agent": "opspilot-ingestion-worker",
            "X-GitHub-Api-Version": self.settings.github_api_version,
        }

    def _client(self, headers: dict[str, str]) -> tuple[httpx.Client, bool]:
        if self._provided_client is not None:
            self._provided_client.headers.update(headers)
            return self._provided_client, False
        return httpx.Client(base_url="https://api.github.com", headers=headers, timeout=30.0), True

    @staticmethod
    def _response_json(response: httpx.Response, operation: str) -> dict:
        if response.status_code >= 400:
            request_id = response.headers.get("x-github-request-id", "unknown")
            raise GitHubAPIError(f"GitHub {operation} failed with status {response.status_code} (request {request_id})")
        try:
            return response.json()
        except ValueError as exc:
            raise GitHubAPIError(f"GitHub {operation} returned invalid JSON") from exc

    def _installation_token(self, installation_id: int) -> str:
        client, owned = self._client(self._headers(self._app_jwt()))
        try:
            response = client.post(f"/app/installations/{installation_id}/access_tokens")
            data = self._response_json(response, "installation token request")
            token = data.get("token")
            if not token:
                raise GitHubAPIError("GitHub installation token response did not include a token")
            return str(token)
        finally:
            if owned:
                client.close()

    def fetch_for_installation(
        self,
        installation_id: int,
        owner: str,
        repository: str,
        branch: str,
    ) -> tuple[str, list[RepositoryFile]]:
        token = self._installation_token(installation_id)
        client, owned = self._client(self._headers(token))
        try:
            encoded_owner = quote(owner, safe="")
            encoded_repo = quote(repository, safe="")
            encoded_branch = quote(branch, safe="")
            commit_response = client.get(
                f"/repos/{encoded_owner}/{encoded_repo}/commits/{encoded_branch}"
            )
            commit = self._response_json(commit_response, "branch commit request")
            commit_sha = str(commit.get("sha") or "")
            tree_sha = str(commit.get("commit", {}).get("tree", {}).get("sha") or "")
            if not commit_sha or not tree_sha:
                raise GitHubAPIError("GitHub branch response did not include commit and tree SHAs")

            response = client.get(
                f"/repos/{encoded_owner}/{encoded_repo}/git/trees/{tree_sha}",
                params={"recursive": "1"},
            )
            tree = self._response_json(response, "repository tree request")
            if tree.get("truncated"):
                raise GitHubAPIError("repository tree is too large for safe recursive ingestion")

            files: list[RepositoryFile] = []
            total_bytes = 0
            for entry in tree.get("tree", []):
                if entry.get("type") != "blob":
                    continue
                path = str(entry.get("path") or "")
                size = int(entry.get("size") or 0)
                allowed, _ = path_policy(path, size, self.settings.max_file_bytes)
                if not allowed:
                    continue
                if len(files) >= self.settings.max_repository_files:
                    raise GitHubAPIError("repository exceeds the configured indexable file limit")
                if total_bytes + size > self.settings.max_repository_bytes:
                    raise GitHubAPIError("repository exceeds the configured indexable byte limit")

                blob_response = client.get(
                    f"/repos/{encoded_owner}/{encoded_repo}/git/blobs/{entry['sha']}"
                )
                blob = self._response_json(blob_response, f"blob request for {path}")
                if blob.get("encoding") != "base64":
                    continue
                try:
                    raw = base64.b64decode(str(blob.get("content") or ""))
                    content = raw.decode("utf-8")
                except (ValueError, UnicodeDecodeError):
                    continue
                files.append(RepositoryFile(path=path, sha=str(entry["sha"]), size=size, content=content))
                total_bytes += size
            return commit_sha, files
        finally:
            if owned:
                client.close()

    def fetch(self, owner: str, repository: str, branch: str) -> tuple[str, list[RepositoryFile]]:
        raise RuntimeError("use fetch_for_installation so GitHub access remains installation-scoped")
