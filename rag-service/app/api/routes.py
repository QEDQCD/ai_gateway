from fastapi import APIRouter

from app.models.document import (
    IngestDocumentRequest,
    IngestDocumentResponse,
    RagQueryRequest,
    RagQueryResponse,
)
from app.services.answerer import answer_question
from app.services.chunker import chunk_document
from app.services.retriever import retrieve_sources


router = APIRouter()


@router.post("/internal/rag/query", response_model=RagQueryResponse)
def rag_query(payload: RagQueryRequest) -> RagQueryResponse:
    return RagQueryResponse(
        answer=answer_question(payload.question),
        sources=retrieve_sources(payload.knowledge_base_id, payload.question),
    )


@router.post("/internal/rag/ingest", response_model=IngestDocumentResponse)
def ingest_document(payload: IngestDocumentRequest) -> IngestDocumentResponse:
    chunk_document(payload.content)
    return IngestDocumentResponse(document_id="doc_demo", status="queued")
