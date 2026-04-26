from secrets import compare_digest

from fastapi import APIRouter, Depends, HTTPException, status
from fastapi.security import HTTPBasic, HTTPBasicCredentials

from app.models.document import (
    IngestDocumentRequest,
    IngestDocumentResponse,
    RagQueryRequest,
    RagQueryResponse,
)
from app.core.config import settings
from app.services.answerer import answer_question
from app.services.chunker import chunk_document
from app.services.llm_client import embed_texts
from app.services.repository import replace_document_chunks, to_sources, upsert_document
from app.services.retriever import retrieve_ranked_chunks


router = APIRouter()
security = HTTPBasic(auto_error=False)


def health_status() -> dict[str, str]:
    return {"status": "ok"}


def require_basic_auth(credentials: HTTPBasicCredentials | None = Depends(security)) -> None:
    if not settings.basic_auth_username and not settings.basic_auth_password:
        return

    if credentials is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="unauthorized")

    username_matches = compare_digest(credentials.username, settings.basic_auth_username)
    password_matches = compare_digest(credentials.password, settings.basic_auth_password)
    if not username_matches or not password_matches:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="unauthorized")


@router.get("/")
def root() -> dict[str, str]:
    return health_status()


@router.get("/healthz")
def healthz() -> dict[str, str]:
    return health_status()


@router.post("/internal/rag/query", response_model=RagQueryResponse)
def rag_query(payload: RagQueryRequest, _: None = Depends(require_basic_auth)) -> RagQueryResponse:
    chunks = retrieve_ranked_chunks(payload.tenant_id, payload.knowledge_base_id, payload.question)
    return RagQueryResponse(
        answer=answer_question(payload.question, chunks),
        sources=to_sources(chunks),
    )


@router.post("/internal/rag/ingest", response_model=IngestDocumentResponse)
def ingest_document(payload: IngestDocumentRequest, _: None = Depends(require_basic_auth)) -> IngestDocumentResponse:
    chunks = chunk_document(payload.content)
    document_id = upsert_document(
        tenant_id=payload.tenant_id,
        knowledge_base_id=payload.knowledge_base_id,
        document_name=payload.document_name,
        content=payload.content,
        chunk_count=len(chunks),
    )
    replace_document_chunks(
        tenant_id=payload.tenant_id,
        knowledge_base_id=payload.knowledge_base_id,
        document_id=document_id,
        document_name=payload.document_name,
        chunks=chunks,
        embeddings=embed_texts(chunks),
    )
    return IngestDocumentResponse(document_id=document_id, status="queued")
