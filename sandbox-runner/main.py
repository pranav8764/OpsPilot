from __future__ import annotations

import hmac
import os
import subprocess
from typing import Optional

from fastapi import FastAPI, Header, HTTPException, status
from pydantic import BaseModel, Field

from sandbox_runner.executor import DockerExecutor, InvalidSandboxInput


class RepositoryFile(BaseModel):
    path: str = Field(min_length=1, max_length=1024)
    content: str = Field(max_length=524288)


class VerifyRequest(BaseModel):
    files: list[RepositoryFile] = Field(min_length=1, max_length=3000)


app = FastAPI(title="OpsPilot Sandbox Runner", version="0.1.0")


def require_token(authorization: Optional[str] = Header(default=None)) -> None:
    token = os.getenv("SANDBOX_RUNNER_INTERNAL_TOKEN", "")
    if not token:
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="sandbox runner token is not configured")
    if authorization is None or not hmac.compare_digest(authorization, f"Bearer {token}"):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid internal token")


@app.get("/health")
async def health() -> dict:
    configured = bool(os.getenv("SANDBOX_RUNNER_INTERNAL_TOKEN"))
    docker_ready = False
    try:
        result = subprocess.run(["docker", "info"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=3, check=False)
        docker_ready = result.returncode == 0
    except (OSError, subprocess.SubprocessError):
        pass
    return {
        "status": "ok" if configured and docker_ready else "degraded",
        "service": "sandbox-runner",
        "isolation": "docker",
        "docker_ready": docker_ready,
    }


@app.post("/verify", dependencies=[])
async def verify(request: VerifyRequest, authorization: Optional[str] = Header(default=None)) -> dict:
    require_token(authorization)
    files = {item.path: item.content for item in request.files}
    if len(files) != len(request.files):
        raise HTTPException(status_code=400, detail="duplicate repository path")
    try:
        result = DockerExecutor().verify(files)
        return {
            "status": result.status,
            "commands": result.commands,
            "duration_ms": result.duration_ms,
            "output": result.output,
        }
    except InvalidSandboxInput as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=503, detail="sandbox execution unavailable") from exc
