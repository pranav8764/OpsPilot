import io
import tarfile
from types import SimpleNamespace

from sandbox_runner.executor import DockerExecutor, dependency_files, detect_test_plans, safe_path, validate_files


def test_detects_node_python_and_go_without_using_manifest_commands_directly():
    files = {
        "frontend/package.json": '{"scripts":{"lint":"evil text is not copied","build":"next build","test":"vitest"}}',
        "frontend/package-lock.json": "{}",
        "tests/test_app.py": "def test_ok(): assert True",
        "requirements.txt": "fastapi\n",
        "service/go.mod": "module example.test/service\n",
    }
    plans = detect_test_plans(files)
    assert [plan.image for plan in plans] == ["node:22-bookworm-slim", "python:3.12-slim", "golang:1.23-bookworm"]
    assert "evil text" not in plans[0].test
    assert plans[0].test == "npm run lint && npm run build && npm test"


def test_path_and_size_guardrails():
    assert safe_path("src/app.py")
    for path in ("../escape", "/absolute", ".env", "secret.pem", "node_modules/pkg/index.js", ".git/config"):
        assert not safe_path(path)
    try:
        validate_files({"big.txt": "x" * (512 * 1024 + 1)})
    except ValueError as exc:
        assert "size" in str(exc)
    else:
        raise AssertionError("oversized sandbox files must be rejected")


def test_container_command_enforces_runtime_isolation():
    command = DockerExecutor()._container_command("volume", "python:3.12-slim", "/workspace", "pytest", "none")
    joined = " ".join(command)
    assert "--network none" in joined
    assert "--read-only" in command
    assert "--cap-drop ALL" in joined
    assert "no-new-privileges" in joined
    assert "--pids-limit 256" in joined
    assert "--memory 2g" in joined
    assert "--user 65534:65534" in joined


def test_networked_dependency_phase_receives_manifests_only():
    files = {
        "package.json": "{}", "package-lock.json": "{}", "requirements.txt": "pytest",
        "go.mod": "module example", "go.sum": "", "src/private_code.py": "valuable source",
    }
    visible = dependency_files(files)
    assert "src/private_code.py" not in visible
    assert set(visible) == {"package.json", "package-lock.json", "requirements.txt", "go.mod", "go.sum"}


def test_volume_root_is_owned_by_non_root_test_user(monkeypatch):
    commands = []

    def capture(command, **_kwargs):
        commands.append(command)
        return SimpleNamespace(returncode=0)

    monkeypatch.setattr("sandbox_runner.executor.subprocess.run", capture)
    DockerExecutor()._populate_volume("verification-volume", {"test_ok.py": "def test_ok(): assert True"})

    shell = commands[0][-1]
    assert "chown 65534:65534 /workspace" in shell
    assert shell.index("chown 65534:65534 /workspace") < shell.index("tar -xf")


def test_empty_dependency_phase_writes_inert_volume_marker(monkeypatch):
    captured = {}

    def capture(command, **kwargs):
        captured["command"] = command
        captured["input"] = kwargs["input"]
        return SimpleNamespace(returncode=0)

    monkeypatch.setattr("sandbox_runner.executor.subprocess.run", capture)
    DockerExecutor()._populate_volume("verification-volume", {})

    with tarfile.open(fileobj=io.BytesIO(captured["input"]), mode="r:") as archive:
        assert archive.getnames() == [".opspilot-volume-ready"]
