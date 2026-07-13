import base64

import httpx

from opspilot.github_client import GitHubRepositorySource

from conftest import make_settings


def test_github_source_fetches_only_safe_text_blobs():
    requested_paths = []

    def handler(request: httpx.Request) -> httpx.Response:
        requested_paths.append(request.url.path)
        if request.url.path.endswith("/commits/main"):
            return httpx.Response(
                200,
                json={"sha": "commit-sha", "commit": {"tree": {"sha": "tree-sha"}}},
            )
        if request.url.path.endswith("/git/trees/tree-sha"):
            return httpx.Response(
                200,
                json={
                    "sha": "commit-sha",
                    "truncated": False,
                    "tree": [
                        {"path": "README.md", "type": "blob", "size": 12, "sha": "safe-sha"},
                        {"path": ".env", "type": "blob", "size": 12, "sha": "secret-sha"},
                    ],
                },
            )
        if request.url.path.endswith("/git/blobs/safe-sha"):
            return httpx.Response(200, json={"encoding": "base64", "content": base64.b64encode(b"hello readme").decode()})
        raise AssertionError(f"unexpected GitHub request: {request.url}")

    client = httpx.Client(base_url="https://api.github.test", transport=httpx.MockTransport(handler))
    source = GitHubRepositorySource(make_settings(), client=client)
    source._installation_token = lambda installation_id: "installation-token"

    commit, files = source.fetch_for_installation(42, "acme", "api", "main")

    assert commit == "commit-sha"
    assert [file.path for file in files] == ["README.md"]
    assert all("secret-sha" not in path for path in requested_paths)
    client.close()
