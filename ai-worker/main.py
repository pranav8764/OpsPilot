from __future__ import annotations

import asyncio
import contextvars
import hmac
import json
import logging
import re
import threading
import time
from contextlib import asynccontextmanager
from dataclasses import asdict
from datetime import datetime, timezone
from functools import lru_cache
from typing import Annotated, Optional
from uuid import UUID

import httpx
from dotenv import load_dotenv
from fastapi import Depends, FastAPI, Header, HTTPException, Query, Request, Response, status
from pydantic import BaseModel, Field

from opspilot.config import Settings
from opspilot.embeddings import build_embedding_provider
from opspilot.deployment import DeploymentService, DeploymentStore
from opspilot.github_client import GitHubRepositorySource
from opspilot.fixes import FixProposalService, GroqFixProvider, PostgresFixStore
from opspilot.ingestion import IngestionProcessor, JobNotAvailable
from opspilot.incidents import IncidentService, PostgresIncidentStore, build_rca_provider
from opspilot.rag import RAGService, build_answer_provider
from opspilot.retention import PostgresRetentionCleaner
from opspilot.sandbox import HTTPSandboxVerifier
from opspilot.storage import PostgresStore
from opspilot.usage import PostgresUsageLimiter, UsageLimitExceeded
from opspilot.worker import BackgroundIngestionWorker


load_dotenv()
request_id_context = contextvars.ContextVar("request_id", default="")


class JSONLogFormatter(logging.Formatter):
    _extra_fields = {
        "workspace_id",
        "project_id",
        "incident_id",
        "job_id",
        "reason",
        "deleted",
        "method",
        "path",
        "status",
        "duration_ms",
    }

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "level": record.levelname.lower(),
            "logger": record.name,
            "message": record.getMessage(),
        }
        request_id = request_id_context.get()
        if request_id:
            payload["request_id"] = request_id
        for field in self._extra_fields:
            if hasattr(record, field):
                payload[field] = getattr(record, field)
        if record.exc_info:
            payload["exception_type"] = record.exc_info[0].__name__
        return json.dumps(payload, default=str, separators=(",", ":"))


log_handler = logging.StreamHandler()
log_handler.setFormatter(JSONLogFormatter())
logging.getLogger().handlers = [log_handler]
logging.getLogger().setLevel(logging.INFO)
logger = logging.getLogger("opspilot.ai_worker")


class HTTPMetrics:
    def __init__(self):
        self._lock = threading.Lock()
        self._values: dict[tuple[str, int], tuple[int, float]] = {}

    def observe(self, method: str, status_code: int, duration: float) -> None:
        key = (method, status_code)
        with self._lock:
            count, total = self._values.get(key, (0, 0.0))
            self._values[key] = (count + 1, total + duration)

    def render(self) -> str:
        with self._lock:
            values = dict(self._values)
        lines = [
            "# HELP opspilot_ai_http_requests_total HTTP requests handled.",
            "# TYPE opspilot_ai_http_requests_total counter",
        ]
        for (method, status_code), (count, _) in sorted(values.items()):
            labels = f'method="{method}",status="{status_code}"'
            lines.append(f"opspilot_ai_http_requests_total{{{labels}}} {count}")
        lines.extend([
            "# HELP opspilot_ai_http_request_duration_seconds HTTP request duration.",
            "# TYPE opspilot_ai_http_request_duration_seconds summary",
        ])
        for (method, status_code), (count, total) in sorted(values.items()):
            labels = f'method="{method}",status="{status_code}"'
            lines.append(f"opspilot_ai_http_request_duration_seconds_sum{{{labels}}} {total:.6f}")
            lines.append(f"opspilot_ai_http_request_duration_seconds_count{{{labels}}} {count}")
        return "\n".join(lines) + "\n"


http_metrics = HTTPMetrics()
REQUEST_ID_PATTERN = re.compile(r"^[A-Za-z0-9._-]{1,128}$")


@lru_cache
def get_settings() -> Settings:
    return Settings.from_env()


def build_processor(settings: Settings) -> IngestionProcessor:
    settings.validate_worker()
    store = PostgresStore(settings.database_url)
    embeddings = build_embedding_provider(settings)
    source = GitHubRepositorySource(settings)
    return IngestionProcessor(
        store=store,
        embeddings=embeddings,
        fetch_repository=lambda job: source.fetch_for_installation(
            job.installation_id, job.owner, job.repository, job.branch
        ),
        settings=settings,
    )


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    task: asyncio.Task | None = None
    worker: BackgroundIngestionWorker | None = None
    if settings.run_background_worker:
        try:
            processor = build_processor(settings)
            worker = BackgroundIngestionWorker(
                processor,
                nats_url=settings.nats_url,
                recovery_poll_seconds=settings.recovery_poll_seconds,
                retention_cleanup=PostgresRetentionCleaner(settings.database_url).cleanup,
                retention_cleanup_seconds=settings.retention_cleanup_seconds,
            )
            task = asyncio.create_task(worker.run())
            logger.info("background ingestion worker started")
        except Exception:
            logger.exception("background ingestion worker could not start")
    yield
    if worker is not None:
        worker.stop()
    if task is not None:
        try:
            await asyncio.wait_for(task, timeout=10)
        except TimeoutError:
            task.cancel()


app = FastAPI(
    title="OpsPilot AI Worker",
    description="Repository ingestion, retrieval, and grounded AI analysis service",
    version="0.2.0",
    lifespan=lifespan,
)


@app.middleware("http")
async def request_observability(request: Request, call_next):
    candidate = request.headers.get("X-Request-ID", "")
    request_id = candidate if REQUEST_ID_PATTERN.fullmatch(candidate) else f"ai-{time.time_ns():x}"
    token = request_id_context.set(request_id)
    started = time.monotonic()
    status_code = 500
    try:
        response = await call_next(request)
        status_code = response.status_code
        response.headers["X-Request-ID"] = request_id
        return response
    finally:
        duration = time.monotonic() - started
        http_metrics.observe(request.method, status_code, duration)
        logger.info(
            "http request",
            extra={
                "method": request.method,
                "path": request.url.path,
                "status": status_code,
                "duration_ms": round(duration * 1000, 3),
            },
        )
        request_id_context.reset(token)


def require_internal_token(
    authorization: Annotated[Optional[str], Header()] = None,
    settings: Settings = Depends(get_settings),
) -> None:
    if not settings.internal_token:
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="internal service token is not configured")
    expected = f"Bearer {settings.internal_token}"
    if authorization is None or not hmac.compare_digest(authorization, expected):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid internal service token")


class EmbedRequest(BaseModel):
    texts: list[str] = Field(min_length=1, max_length=100)
    task_type: str = Field(default="document", pattern="^(document|query)$")


class RAGQueryRequest(BaseModel):
    workspace_id: UUID
    project_id: UUID
    user_id: UUID
    question: str = Field(min_length=1, max_length=4000)
    top_k: int = Field(default=8, ge=1, le=20)


class LogAnalysisRequest(BaseModel):
    workspace_id: UUID
    project_id: UUID
    user_id: UUID
    source: str = Field(default="manual", min_length=1, max_length=64, pattern=r"^[a-z0-9_.-]+$")
    content: str = Field(min_length=1, max_length=1_048_576)


class FixProposalRequest(BaseModel):
    workspace_id: UUID
    project_id: UUID
    incident_id: UUID
    user_id: UUID


class DeploymentAnalysisRequest(BaseModel):
    workspace_id: UUID
    project_id: UUID
    user_id: UUID


@app.get("/health")
async def health() -> dict:
    settings = get_settings()
    return {
        "status": "ok",
        "service": "ai-worker",
        "version": "0.2.0",
        "background_worker": settings.run_background_worker,
        "embedding_provider": settings.embedding_provider,
        "answer_provider": "groq" if settings.groq_api_key else "extractive",
    }


@app.get("/metrics", response_class=Response)
async def metrics() -> Response:
    return Response(http_metrics.render(), media_type="text/plain; version=0.0.4")


@app.get("/ready")
async def ready() -> dict:
    import psycopg

    settings = get_settings()
    try:
        def check_database() -> None:
            with psycopg.connect(settings.database_url) as connection:
                connection.execute("SELECT 1")

        await asyncio.to_thread(check_database)
        if settings.sandbox_runner_url:
            response = await asyncio.to_thread(
                httpx.get, settings.sandbox_runner_url.rstrip("/") + "/health", timeout=3.0
            )
            if response.status_code >= 400 or response.json().get("status") != "ok":
                raise RuntimeError("sandbox runner is not ready")
        return {"status": "ready", "service": "ai-worker"}
    except Exception as exc:
        logger.warning("readiness check failed", extra={"reason": type(exc).__name__})
        raise HTTPException(status_code=503, detail="ai-worker dependencies are not ready") from exc


@app.get("/")
async def root() -> dict:
    return {
        "service": "OpsPilot AI Worker",
        "version": "0.2.0",
        "capabilities": ["repository_ingestion", "secret_redaction", "embeddings", "cited_rag"],
    }


@app.post("/embed", dependencies=[Depends(require_internal_token)])
async def embed(request: EmbedRequest) -> dict:
    if sum(len(text) for text in request.texts) > 200_000:
        raise HTTPException(status_code=413, detail="embedding request is too large")
    provider = build_embedding_provider(get_settings())
    if request.task_type == "query":
        embeddings = [provider.embed_query(text) for text in request.texts]
    else:
        embeddings = provider.embed_documents(request.texts)
    return {"embeddings": embeddings, "dimensions": len(embeddings[0]) if embeddings else 0}


@app.post("/ingestion/jobs/{job_id}/run", dependencies=[Depends(require_internal_token)])
async def run_ingestion_job(job_id: UUID) -> dict:
    try:
        result = await asyncio.to_thread(build_processor(get_settings()).process, str(job_id))
        return asdict(result)
    except JobNotAvailable as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("manual ingestion job failed", extra={"job_id": str(job_id)})
        raise HTTPException(status_code=502, detail="repository ingestion failed") from exc


@app.post("/rag/query", dependencies=[Depends(require_internal_token)])
async def rag_query(request: RAGQueryRequest) -> dict:
    settings = get_settings()
    try:
        PostgresUsageLimiter(settings.database_url).consume(
            str(request.workspace_id), str(request.project_id), str(request.user_id), "ai_query"
        )
        service = RAGService(
            store=PostgresStore(settings.database_url),
            embeddings=build_embedding_provider(settings),
            answer_provider=build_answer_provider(settings),
            min_score=settings.retrieval_min_score,
        )
        return await asyncio.to_thread(
            service.query,
            str(request.workspace_id),
            str(request.project_id),
            request.question,
            request.top_k,
        )
    except UsageLimitExceeded as exc:
        raise HTTPException(status_code=429, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("RAG query failed", extra={"project_id": str(request.project_id)})
        raise HTTPException(status_code=502, detail="project query failed") from exc


@app.post("/analyze/logs", dependencies=[Depends(require_internal_token)])
async def analyze_logs(request: LogAnalysisRequest) -> dict:
    settings = get_settings()
    try:
        PostgresUsageLimiter(settings.database_url).consume(
            str(request.workspace_id), str(request.project_id), str(request.user_id), "incident_analysis"
        )
        postgres = PostgresStore(settings.database_url)
        service = IncidentService(
            retrieval_store=postgres,
            incident_store=PostgresIncidentStore(settings.database_url),
            embeddings=build_embedding_provider(settings),
            rca_provider=build_rca_provider(settings.groq_api_key, settings.groq_model),
        )
        return await asyncio.to_thread(
            service.analyze,
            str(request.workspace_id),
            str(request.project_id),
            str(request.user_id),
            request.source,
            request.content,
        )
    except UsageLimitExceeded as exc:
        raise HTTPException(status_code=429, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("log analysis failed", extra={"project_id": str(request.project_id)})
        raise HTTPException(status_code=502, detail="log analysis failed") from exc


@app.get("/incidents", dependencies=[Depends(require_internal_token)])
async def list_incidents(
    workspace_id: UUID,
    project_id: UUID,
    limit: int = Query(default=25, ge=1, le=100),
) -> dict:
    try:
        incidents = await asyncio.to_thread(
            PostgresIncidentStore(get_settings().database_url).list_incidents,
            str(workspace_id),
            str(project_id),
            limit,
        )
        return {"incidents": incidents}
    except Exception as exc:
        logger.exception("incident list failed", extra={"project_id": str(project_id)})
        raise HTTPException(status_code=502, detail="incident history could not be loaded") from exc


@app.post("/fixes/propose", dependencies=[Depends(require_internal_token)])
async def propose_fix(request: FixProposalRequest) -> dict:
    settings = get_settings()
    try:
        PostgresUsageLimiter(settings.database_url).consume(
            str(request.workspace_id), str(request.project_id), str(request.user_id), "fix_proposal"
        )
        source = GitHubRepositorySource(settings)
        service = FixProposalService(
            store=PostgresFixStore(settings.database_url),
            fetch_repository=lambda job: source.fetch_for_installation(
                job.installation_id, job.owner, job.repository, job.branch
            ),
            provider=GroqFixProvider(settings.groq_api_key, settings.groq_model),
            max_file_bytes=settings.max_file_bytes,
            sandbox=HTTPSandboxVerifier(settings.sandbox_runner_url, settings.sandbox_runner_token)
            if settings.sandbox_runner_url else None,
        )
        return await asyncio.to_thread(
            service.propose,
            str(request.workspace_id),
            str(request.project_id),
            str(request.incident_id),
            str(request.user_id),
        )
    except UsageLimitExceeded as exc:
        raise HTTPException(status_code=429, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("fix proposal failed", extra={"incident_id": str(request.incident_id)})
        raise HTTPException(status_code=502, detail="fix proposal generation failed") from exc


def build_deployment_service(settings: Settings) -> DeploymentService:
    source = GitHubRepositorySource(settings)
    return DeploymentService(
        DeploymentStore(settings.database_url),
        lambda job: source.fetch_for_installation(job.installation_id, job.owner, job.repository, job.branch),
    )


@app.post("/deployment/readiness", dependencies=[Depends(require_internal_token)])
async def deployment_readiness(request: DeploymentAnalysisRequest) -> dict:
    try:
        PostgresUsageLimiter(get_settings().database_url).consume(
            str(request.workspace_id), str(request.project_id), str(request.user_id), "deployment_analysis"
        )
        return await asyncio.to_thread(
            build_deployment_service(get_settings()).scan,
            str(request.workspace_id), str(request.project_id),
        )
    except UsageLimitExceeded as exc:
        raise HTTPException(status_code=429, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("deployment readiness scan failed", extra={"project_id": str(request.project_id)})
        raise HTTPException(status_code=502, detail="deployment readiness scan failed") from exc


@app.post("/deployment/generate", dependencies=[Depends(require_internal_token)])
async def generate_deployment_config(request: DeploymentAnalysisRequest) -> dict:
    try:
        PostgresUsageLimiter(get_settings().database_url).consume(
            str(request.workspace_id), str(request.project_id), str(request.user_id), "deployment_analysis"
        )
        return await asyncio.to_thread(
            build_deployment_service(get_settings()).generate,
            str(request.workspace_id), str(request.project_id),
        )
    except UsageLimitExceeded as exc:
        raise HTTPException(status_code=429, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("deployment config generation failed", extra={"project_id": str(request.project_id)})
        raise HTTPException(status_code=502, detail="deployment configuration generation failed") from exc


@app.get("/usage", dependencies=[Depends(require_internal_token)])
async def usage_summary(workspace_id: UUID) -> dict:
    try:
        return await asyncio.to_thread(PostgresUsageLimiter(get_settings().database_url).summary, str(workspace_id))
    except ValueError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("usage summary failed", extra={"workspace_id": str(workspace_id)})
        raise HTTPException(status_code=502, detail="usage summary could not be loaded") from exc


@app.get("/eval/report", dependencies=[Depends(require_internal_token)])
async def get_eval_report() -> dict:
    try:
        report_path = Path(__file__).resolve().parent / "eval" / "report.json"
        if not report_path.exists():
            raise HTTPException(status_code=404, detail="Evaluation report not found. Run runner.py first.")
        
        with open(report_path, "r") as f:
            return json.load(f)
    except HTTPException:
        raise
    except Exception as exc:
        logger.exception("failed to load evaluation report")
        raise HTTPException(status_code=502, detail="failed to load evaluation report") from exc

