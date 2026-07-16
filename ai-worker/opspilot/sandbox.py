from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

import httpx


@dataclass(frozen=True)
class SandboxResult:
    status: str
    commands: list[str]
    duration_ms: int
    output: str


class SandboxVerifier(Protocol):
    def run(self, files: dict[str, str]) -> SandboxResult: ...


class HTTPSandboxVerifier:
    def __init__(self, base_url: str, internal_token: str, timeout_seconds: float = 650.0):
        self.base_url = base_url.rstrip("/")
        self.internal_token = internal_token
        self.timeout_seconds = timeout_seconds

    def run(self, files: dict[str, str]) -> SandboxResult:
        headers = {"Content-Type": "application/json"}
        if self.internal_token:
            headers["Authorization"] = f"Bearer {self.internal_token}"
        response = httpx.post(
            f"{self.base_url}/verify",
            headers=headers,
            json={"files": [{"path": path, "content": content} for path, content in files.items()]},
            timeout=self.timeout_seconds,
        )
        if response.status_code >= 400:
            raise RuntimeError(f"sandbox runner failed with status {response.status_code}")
        try:
            payload = response.json()
            status = str(payload["status"])
            if status not in {"passed", "failed", "timed_out", "no_tests"}:
                raise ValueError("invalid status")
            return SandboxResult(
                status=status,
                commands=[str(command)[:500] for command in payload.get("commands", [])[:20]],
                duration_ms=max(0, int(payload.get("duration_ms", 0))),
                output=str(payload.get("output", ""))[-12000:],
            )
        except (KeyError, TypeError, ValueError) as exc:
            raise RuntimeError("sandbox runner returned an invalid result") from exc


def verification_without_sandbox() -> dict:
    return {
        "tests": {"status": "not_run", "reason": "isolated repository sandbox is not configured"},
        "verified_fix": False,
    }


def verify_in_sandbox(verifier: SandboxVerifier, baseline: dict[str, str], patched: dict[str, str]) -> dict:
    baseline_result = verifier.run(baseline)
    patched_result = verifier.run(patched)
    if patched_result.status in {"failed", "timed_out"}:
        raise ValueError(f"patched repository tests {patched_result.status}; refusing to create a fix proposal")
    verified = patched_result.status == "passed"
    reason = "isolated tests passed" if verified else "the repository has no recognized test command"
    return {
        "tests": {
            "status": patched_result.status,
            "reason": reason,
            "baseline_status": baseline_result.status,
            "commands": patched_result.commands,
            "duration_ms": patched_result.duration_ms,
            "output_tail": patched_result.output[-4000:],
        },
        "verified_fix": verified,
    }
