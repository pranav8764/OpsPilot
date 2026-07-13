from __future__ import annotations

import io
import json
import os
import re
import shlex
import subprocess
import tarfile
import time
import uuid
from dataclasses import dataclass
from pathlib import PurePosixPath


MAX_FILES = 3000
MAX_FILE_BYTES = 512 * 1024
MAX_TOTAL_BYTES = 30 * 1024 * 1024
MAX_OUTPUT_CHARS = 12000
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


@dataclass(frozen=True)
class TestPlan:
    image: str
    workdir: str
    setup: str
    test: str


@dataclass(frozen=True)
class ExecutionResult:
    status: str
    commands: list[str]
    duration_ms: int
    output: str


class InvalidSandboxInput(ValueError):
    pass


def safe_path(path: str) -> bool:
    if not path or "\x00" in path or "\\" in path or path.startswith("/"):
        return False
    parsed = PurePosixPath(path)
    lower = path.lower()
    return (
        ".." not in parsed.parts
        and ".git" not in parsed.parts
        and "node_modules" not in parsed.parts
        and not lower.startswith(".env")
        and not lower.endswith((".pem", ".key", ".p12", ".pfx"))
    )


def validate_files(files: dict[str, str]) -> None:
    if not files or len(files) > MAX_FILES:
        raise InvalidSandboxInput("repository file count is outside sandbox limits")
    total = 0
    for path, content in files.items():
        if not safe_path(path) or not isinstance(content, str):
            raise InvalidSandboxInput("repository contains an invalid sandbox path")
        size = len(content.encode("utf-8"))
        if size > MAX_FILE_BYTES:
            raise InvalidSandboxInput(f"sandbox file exceeds size limit: {path}")
        total += size
        if total > MAX_TOTAL_BYTES:
            raise InvalidSandboxInput("repository exceeds sandbox byte limit")


def detect_test_plans(files: dict[str, str]) -> list[TestPlan]:
    plans: list[TestPlan] = []
    for path in sorted(files):
        if path.endswith("package.json") and len(plans) < 12:
            try:
                package = json.loads(files[path])
                scripts = package.get("scripts", {}) if isinstance(package, dict) else {}
            except (TypeError, ValueError):
                continue
            commands = []
            if isinstance(scripts, dict):
                if "lint" in scripts:
                    commands.append("npm run lint")
                if "build" in scripts:
                    commands.append("npm run build")
                test_script = str(scripts.get("test", ""))
                if test_script and "no test specified" not in test_script.lower():
                    commands.append("npm test")
            if not commands:
                continue
            directory = str(PurePosixPath(path).parent)
            lock_path = str(PurePosixPath(directory) / "package-lock.json") if directory != "." else "package-lock.json"
            setup = "npm ci --ignore-scripts --no-audit --no-fund" if lock_path in files else "npm install --ignore-scripts --no-audit --no-fund"
            plans.append(TestPlan("node:22-bookworm-slim", directory, setup, " && ".join(commands)))

    python_test_present = any(
        PurePosixPath(path).name.startswith("test_") and path.endswith(".py") or "/tests/" in f"/{path}"
        for path in files
    )
    if python_test_present:
        requirements = "requirements.txt" if "requirements.txt" in files else ""
        setup = "python -m venv /workspace/.opspilot-venv && /workspace/.opspilot-venv/bin/pip install --disable-pip-version-check pytest"
        if requirements:
            setup += " -r requirements.txt"
        plans.append(TestPlan("python:3.12-slim", ".", setup, "/workspace/.opspilot-venv/bin/python -m pytest -q"))

    for path in sorted(files):
        if PurePosixPath(path).name == "go.mod" and len(plans) < 16:
            directory = str(PurePosixPath(path).parent)
            plans.append(TestPlan("golang:1.23-bookworm", directory, "go mod download", "go test ./..."))
    return plans[:16]


def dependency_files(files: dict[str, str]) -> dict[str, str]:
    allowed_names = {
        "package.json", "package-lock.json", "npm-shrinkwrap.json",
        "requirements.txt", "requirements-dev.txt", "go.mod", "go.sum",
    }
    return {path: content for path, content in files.items() if PurePosixPath(path).name in allowed_names}


class DockerExecutor:
    def __init__(self, docker_binary: str = "docker", timeout_seconds: int = 600):
        self.docker_binary = docker_binary
        self.timeout_seconds = timeout_seconds

    def verify(self, files: dict[str, str]) -> ExecutionResult:
        validate_files(files)
        plans = detect_test_plans(files)
        if not plans:
            return ExecutionResult("no_tests", [], 0, "No recognized test, lint, or build command was found.")
        started = time.monotonic()
        volume = f"opspilot-sandbox-{uuid.uuid4().hex}"
        outputs: list[str] = []
        commands: list[str] = []
        try:
            self._run([self.docker_binary, "volume", "create", volume], timeout=30)
            # Dependency resolution gets network access, so it sees manifests and
            # lockfiles only. Repository source is copied in only after egress is
            # disabled for every test process.
            self._populate_volume(volume, dependency_files(files))
            for plan in plans:
                workdir = "/workspace" if plan.workdir == "." else "/workspace/" + plan.workdir
                setup_command = self._container_command(volume, plan.image, workdir, plan.setup, network="bridge")
                commands.append(f"[{plan.workdir}] {plan.setup}")
                setup = self._run(setup_command, timeout=self.timeout_seconds)
                outputs.append(setup.stdout)
                if setup.returncode != 0:
                    return self._result("failed", commands, started, outputs)
            self._populate_volume(volume, files)
            for plan in plans:
                workdir = "/workspace" if plan.workdir == "." else "/workspace/" + plan.workdir
                test_command = self._container_command(volume, plan.image, workdir, plan.test, network="none")
                commands.append(f"[{plan.workdir}] {plan.test}")
                tested = self._run(test_command, timeout=self.timeout_seconds)
                outputs.append(tested.stdout)
                if tested.returncode != 0:
                    return self._result("failed", commands, started, outputs)
            return self._result("passed", commands, started, outputs)
        except subprocess.TimeoutExpired as exc:
            outputs.append((exc.stdout or "") if isinstance(exc.stdout, str) else "")
            return self._result("timed_out", commands, started, outputs)
        finally:
            try:
                self._run([self.docker_binary, "volume", "rm", "-f", volume], timeout=30, check=False)
            except (OSError, subprocess.SubprocessError):
                pass

    def _populate_volume(self, volume: str, files: dict[str, str]) -> None:
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as tar:
            # A completely empty named volume is populated from the next
            # image's mountpoint by Docker, which resets the ownership to root.
            # Keep an inert marker when a repository has no dependency files.
            archive_files = files or {".opspilot-volume-ready": ""}
            for path, content in archive_files.items():
                data = content.encode("utf-8")
                info = tarfile.TarInfo(path)
                info.size = len(data)
                info.mode = 0o644
                tar.addfile(info, io.BytesIO(data))
        archive.seek(0)
        command = [
            self.docker_binary, "run", "--rm", "-i", "--network", "none",
            "--read-only", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE",
            "--security-opt", "no-new-privileges",
            "-v", f"{volume}:/workspace", "alpine:3.20",
            # Docker creates the volume mount root as root. Chown it explicitly
            # (not only its current children) so the non-root setup/test UID can
            # create node_modules, virtualenvs, and build output.
            "sh", "-lc", "chown 65534:65534 /workspace && tar -xf - -C /workspace && chown -R 65534:65534 /workspace",
        ]
        completed = subprocess.run(
            command, input=archive.getvalue(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            timeout=120, check=False,
        )
        if completed.returncode != 0:
            raise RuntimeError("failed to populate isolated repository volume")

    def _container_command(self, volume: str, image: str, workdir: str, command: str, network: str) -> list[str]:
        return [
            self.docker_binary, "run", "--rm",
            "--network", network,
            "--cpus", "2", "--memory", "2g", "--memory-swap", "2g", "--pids-limit", "256",
            "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
            "--user", "65534:65534",
            "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=256m",
            "-e", "HOME=/tmp", "-e", "CI=1", "-e", "NO_COLOR=1",
            "-v", f"{volume}:/workspace:rw", "-w", workdir,
            image, "sh", "-lc", command,
        ]

    def _run(self, command: list[str], timeout: int, check: bool = False):
        return subprocess.run(
            command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            timeout=timeout, check=check,
        )

    @staticmethod
    def _result(status: str, commands: list[str], started: float, outputs: list[str]) -> ExecutionResult:
        output = ANSI_RE.sub("", "\n".join(outputs))[-MAX_OUTPUT_CHARS:]
        return ExecutionResult(status, commands, int((time.monotonic() - started) * 1000), output)
