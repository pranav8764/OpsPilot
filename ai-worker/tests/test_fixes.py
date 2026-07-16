from opspilot.domain import RepositoryFile
from opspilot.fixes import EditInstruction, FixContext, FixProposalService
from opspilot.sandbox import SandboxResult


class FakeFixStore:
    def __init__(self):
        self.context = FixContext(
            incident_id="incident-1",
            title="Feature flag is disabled",
            root_cause="The runtime reads the wrong default.",
            fix_recommendation="Change the default and add a regression test.",
            confidence="high",
            sanitized_logs="ERROR feature is disabled",
            owner="acme",
            repository="api",
            branch="main",
            installation_id=42,
            indexed_commit_sha="commit-1",
            evidence_paths=["config.json"],
        )
        self.created = None

    def load_context(self, workspace_id, project_id, incident_id):
        assert incident_id == "incident-1"
        return self.context

    def create_proposal(self, *args):
        self.created = args
        return {
            "proposal_id": "proposal-1",
            "approval_id": "approval-1",
            "workflow_id": "workflow-1",
            "approval_status": "pending",
            "approval_expires_at": "2026-07-14T00:00:00Z",
        }


class FakeFixProvider:
    def __init__(self, edits):
        self.edits = edits

    def propose(self, context, candidates):
        return self.edits


def repository(commit="commit-1"):
    return commit, [RepositoryFile("config.json", "blob-1", 25, '{"feature_enabled": false}\n')]


def test_fix_proposal_is_pinned_validated_and_waits_for_approval():
    store = FakeFixStore()
    service = FixProposalService(
        store,
        lambda job: repository(),
        FakeFixProvider([
            EditInstruction("config.json", '"feature_enabled": false', '"feature_enabled": true', "Use the intended default")
        ]),
        max_file_bytes=1024,
    )

    result = service.propose("workspace-1", "project-1", "incident-1", "user-1")

    assert result["approval_status"] == "pending"
    assert result["verification"]["verified_fix"] is False
    assert result["verification"]["tests"]["status"] == "not_run"
    assert '-{"feature_enabled": false}' in result["unified_diff"]
    assert '+{"feature_enabled": true}' in result["unified_diff"]
    assert store.created is not None


def test_fix_proposal_refuses_stale_repository_context():
    service = FixProposalService(
        FakeFixStore(),
        lambda job: repository("newer-commit"),
        FakeFixProvider([]),
        max_file_bytes=1024,
    )
    try:
        service.propose("workspace-1", "project-1", "incident-1", "user-1")
    except ValueError as exc:
        assert "re-index" in str(exc)
    else:
        raise AssertionError("stale repository context should block a proposal")


def test_fix_proposal_refuses_unapproved_paths():
    service = FixProposalService(
        FakeFixStore(),
        lambda job: repository(),
        FakeFixProvider([EditInstruction(".github/workflows/deploy.yml", "x", "y", "unsafe")]),
        max_file_bytes=1024,
    )
    try:
        service.propose("workspace-1", "project-1", "incident-1", "user-1")
    except ValueError as exc:
        assert "unapproved path" in str(exc)
    else:
        raise AssertionError("unapproved file edits should be blocked")


class PassingSandbox:
    def __init__(self):
        self.calls = []

    def run(self, files):
        self.calls.append(files)
        return SandboxResult("passed", ["python -m pytest -q"], 120, "1 passed")


class FailingPatchedSandbox:
    def __init__(self):
        self.calls = 0

    def run(self, files):
        self.calls += 1
        status = "passed" if self.calls == 1 else "failed"
        return SandboxResult(status, ["test"], 10, "failed")


def test_fix_proposal_runs_baseline_and_patched_in_sandbox():
    sandbox = PassingSandbox()
    service = FixProposalService(
        FakeFixStore(), lambda job: repository(),
        FakeFixProvider([EditInstruction("config.json", "false", "true", "fix")]),
        max_file_bytes=1024, sandbox=sandbox,
    )
    result = service.propose("workspace-1", "project-1", "incident-1", "user-1")
    assert result["verification"]["verified_fix"] is True
    assert len(sandbox.calls) == 2
    assert '"feature_enabled": false' in sandbox.calls[0]["config.json"]
    assert '"feature_enabled": true' in sandbox.calls[1]["config.json"]


def test_fix_proposal_is_refused_when_patched_tests_fail():
    service = FixProposalService(
        FakeFixStore(), lambda job: repository(),
        FakeFixProvider([EditInstruction("config.json", "false", "true", "fix")]),
        max_file_bytes=1024, sandbox=FailingPatchedSandbox(),
    )
    try:
        service.propose("workspace-1", "project-1", "incident-1", "user-1")
    except ValueError as exc:
        assert "refusing" in str(exc)
    else:
        raise AssertionError("failing patched tests must block a proposal")
