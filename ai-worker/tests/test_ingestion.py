from opspilot.domain import IngestionJob, RepositoryFile
from opspilot.embeddings import LocalHashEmbeddings
from opspilot.ingestion import IngestionProcessor

from conftest import make_settings


class FakeIngestionStore:
    def __init__(self):
        self.job = IngestionJob(
            id="job-1",
            workspace_id="workspace-1",
            project_id="project-1",
            repository_connection_id="connection-1",
            owner="acme",
            repository="api",
            branch="main",
            installation_id=42,
            attempt_count=1,
            max_attempts=3,
        )
        self.completed = None
        self.failed = None

    def claim_job(self, job_id=None):
        return self.job

    def complete_job(self, job, commit_sha, documents, stats):
        self.completed = (job, commit_sha, documents, stats)

    def fail_job(self, job, code, message):
        self.failed = (job, code, message)


def test_ingestion_filters_and_redacts_before_persistence():
    store = FakeIngestionStore()
    files = [
        RepositoryFile(".env", "sha-env", 20, "PASSWORD=do-not-store-me"),
        RepositoryFile(
            "src/config.py",
            "sha-code",
            100,
            "API_KEY=super-secret-value-123\nDATABASE_POOL_SIZE = 10\n",
        ),
    ]
    processor = IngestionProcessor(
        store,
        LocalHashEmbeddings(64),
        lambda job: ("commit-123", files),
        make_settings(embedding_dimensions=64, chunk_chars=200, chunk_overlap_chars=20),
    )

    result = processor.process("job-1")

    assert result.files_seen == 2
    assert result.files_indexed == 1
    assert result.files_skipped == 1
    assert result.redactions == 1
    assert store.failed is None
    documents = store.completed[2]
    assert [document.path for document in documents] == ["src/config.py"]
    assert "super-secret-value-123" not in documents[0].chunks[0].text
    assert "[REDACTED]" in documents[0].chunks[0].text


def test_ingestion_records_failure_without_completing_job():
    class BrokenEmbeddings(LocalHashEmbeddings):
        def embed_documents(self, texts):
            raise RuntimeError("provider unavailable")

    store = FakeIngestionStore()
    processor = IngestionProcessor(
        store,
        BrokenEmbeddings(64),
        lambda job: ("commit-123", [RepositoryFile("README.md", "sha", 10, "hello world")]),
        make_settings(chunk_chars=200, chunk_overlap_chars=20),
    )

    try:
        processor.process()
    except RuntimeError as exc:
        assert "provider unavailable" in str(exc)
    else:
        raise AssertionError("embedding failure should escape for retry handling")

    assert store.completed is None
    assert store.failed[1] == "RUNTIMEERROR"
