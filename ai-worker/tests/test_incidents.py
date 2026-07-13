from opspilot.domain import RetrievedChunk
from opspilot.embeddings import LocalHashEmbeddings
from datetime import datetime, timezone

from opspilot.incidents import ExtractiveRCAProvider, IncidentService, _incident_row_to_dict, parse_logs


class FakeRetrievalStore:
    def __init__(self, chunks):
        self.chunks = chunks

    def search(self, workspace_id, project_id, query, embedding, limit):
        return self.chunks[:limit]

    def record_query(self, *args):
        raise AssertionError("incident analysis should not record a chat query")


class FakeIncidentStore:
    def __init__(self):
        self.created = None

    def create_incident(self, workspace_id, project_id, user_id, source, parsed, rca, evidence):
        self.created = (workspace_id, project_id, user_id, source, parsed, rca, evidence)
        return {"incident_id": "incident-1", "workflow_id": "workflow-1", "log_id": "log-1"}


def test_parse_logs_redacts_and_produces_stable_fingerprint():
    first = parse_logs("2026-07-13T10:00:01Z ERROR request 1234 database timeout API_KEY=super-secret-value-123")
    second = parse_logs("2026-07-13T10:04:09Z ERROR request 9876 database timeout API_KEY=another-secret-value-456")

    assert "super-secret-value-123" not in first.sanitized_content
    assert sum(first.redactions.values()) == 1
    assert first.fingerprint == second.fingerprint
    assert first.severity == "error"


def test_incident_analysis_is_grounded_and_persisted():
    chunks = [RetrievedChunk("chunk-1", "db/pool.go", "database pool timeout handling", 0.62, 40, 55)]
    incident_store = FakeIncidentStore()
    service = IncidentService(
        FakeRetrievalStore(chunks),
        incident_store,
        LocalHashEmbeddings(64),
        ExtractiveRCAProvider(),
    )

    result = service.analyze(
        "workspace-1",
        "project-1",
        "user-1",
        "manual",
        "ERROR database connection pool timeout at db/pool.go:44",
    )

    assert result["incident_id"] == "incident-1"
    assert result["analysis"]["confidence"] == "medium"
    assert result["evidence"][0]["citation"] == "db/pool.go:L40-L55"
    assert incident_store.created is not None


def test_incident_analysis_enforces_size_limit():
    service = IncidentService(FakeRetrievalStore([]), FakeIncidentStore(), LocalHashEmbeddings(64), ExtractiveRCAProvider())
    try:
        service.analyze("w", "p", "u", "manual", "x" * 1_048_577)
    except ValueError as exc:
        assert "1 MiB" in str(exc)
    else:
        raise AssertionError("oversized logs should be rejected")


def test_incident_history_hides_new_file_content_and_preserves_approval():
    row = {
        "id": "incident-1", "title": "Crash", "status": "fix_proposed", "severity": "error",
        "summary": "summary", "root_cause": "cause", "confidence": "high", "evidence": [],
        "fix_recommendation": "fix", "error_fingerprint": "abc", "source": "vercel.runtime",
        "created_at": datetime(2026, 7, 13, tzinfo=timezone.utc),
        "analysis_workflow_id": "workflow-a", "analysis_workflow_status": "succeeded", "analysis_steps": [],
        "proposal_id": "proposal-1", "fix_workflow_id": "workflow-f", "fix_workflow_status": "waiting_approval",
        "proposal_status": "pending_approval", "base_commit_sha": "deadbeef", "unified_diff": "diff",
        "verification": {"verified_fix": False},
        "proposed_changes": [{"path": "app.py", "reason": "guard null", "additions": 1, "deletions": 1, "new_content": "must-not-leak"}],
        "fix_steps": [], "approval_id": "approval-1", "approval_status": "pending",
        "approval_expires_at": datetime(2026, 7, 14, tzinfo=timezone.utc), "publish_result": {},
    }
    result = _incident_row_to_dict(row)
    assert result["fix_proposal"]["approval"]["status"] == "pending"
    assert result["fix_proposal"]["changes"] == [{"path": "app.py", "reason": "guard null", "additions": 1, "deletions": 1}]
    assert "new_content" not in str(result)
