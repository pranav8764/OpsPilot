from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
import os

app = FastAPI(
    title="OpsPilot AI Worker",
    description="AI/RAG worker service — Phase 3 implementation",
    version="0.1.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "service": "ai-worker",
        "phase": "1 (skeleton)",
        "groq_configured": bool(os.getenv("GROQ_API_KEY")),
        "gemini_configured": bool(os.getenv("GEMINI_API_KEY")),
    }


@app.get("/")
async def root():
    return {
        "message": "OpsPilot AI Worker - Phase 3 features coming soon",
        "endpoints": {
            "Phase 3": ["/embed", "/rag/query", "/analyze/logs"],
            "Phase 4": ["/chat"],
            "Phase 5": ["/workflows/dockerfile", "/workflows/readiness-scan"],
        }
    }


# ── Phase 3 placeholder endpoints ────────────────────────────────

@app.post("/embed")
async def embed_placeholder():
    """Placeholder — will embed text using Gemini text-embedding-004 in Phase 3."""
    return {"message": "Embedding endpoint — implemented in Phase 3"}


@app.post("/rag/query")
async def rag_query_placeholder():
    """Placeholder — will query pgvector and generate answers with Groq in Phase 3."""
    return {"message": "RAG query endpoint — implemented in Phase 3"}


@app.post("/analyze/logs")
async def analyze_logs_placeholder():
    """Placeholder — will analyze logs and generate RCA in Phase 5."""
    return {"message": "Log analysis endpoint — implemented in Phase 5"}
