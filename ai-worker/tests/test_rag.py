from opspilot.domain import RetrievedChunk
from opspilot.embeddings import LocalHashEmbeddings
from opspilot.rag import ExtractiveAnswerProvider, RAGService


class FakeRetrievalStore:
    def __init__(self, chunks):
        self.chunks = chunks
        self.recorded = []

    def search(self, workspace_id, project_id, query, embedding, limit):
        assert workspace_id == "workspace-1"
        assert project_id == "project-1"
        return self.chunks[:limit]

    def record_query(self, workspace_id, project_id, query, enough_context, source_count):
        self.recorded.append((query, enough_context, source_count))


def test_rag_returns_cited_answer_when_context_is_strong():
    chunks = [RetrievedChunk("1", "README.md", "Run make up to start services.", 0.72, 10, 12)]
    store = FakeRetrievalStore(chunks)
    service = RAGService(store, LocalHashEmbeddings(64), ExtractiveAnswerProvider(), min_score=0.2)

    result = service.query("workspace-1", "project-1", "How do I start the services?")

    assert result["enough_context"] is True
    assert result["confidence"] == "high"
    assert result["sources"][0]["citation"] == "README.md:L10-L12"
    assert "[S1]" in result["answer"]
    assert store.recorded == [("How do I start the services?", True, 1)]


def test_rag_refuses_when_context_is_weak():
    chunks = [RetrievedChunk("1", "README.md", "Unrelated", 0.05, 1, 1)]
    store = FakeRetrievalStore(chunks)
    service = RAGService(store, LocalHashEmbeddings(64), ExtractiveAnswerProvider(), min_score=0.2)

    result = service.query("workspace-1", "project-1", "What is the production password?")

    assert result["enough_context"] is False
    assert result["confidence"] == "insufficient"
    assert "Not enough" in result["answer"]
