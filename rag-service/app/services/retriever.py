from __future__ import annotations

import math

from app.core.config import settings
from app.models.document import SourceDocument
from app.services.chunker import chunk_document
from app.services.llm_client import embed_texts
from app.services.repository import (
    RetrievedChunk,
    ensure_document_chunks,
    load_documents,
    to_sources,
)


def retrieve_ranked_chunks(tenant_id: str, knowledge_base_id: str, question: str) -> list[RetrievedChunk]:
    documents = load_documents(tenant_id, knowledge_base_id)
    rankable_chunks = ensure_document_chunks(
        tenant_id=tenant_id,
        knowledge_base_id=knowledge_base_id,
        documents=documents,
        build_chunks=chunk_document,
        build_embeddings=embed_texts,
    )
    if not rankable_chunks:
        return []

    query_embedding = embed_texts([question])[0]
    scored = [
        RetrievedChunk(
            chunk_id=chunk.chunk_id,
            document_id=chunk.document_id,
            document_name=chunk.document_name,
            content=chunk.content,
            embedding=chunk.embedding,
            score=cosine_similarity(query_embedding, chunk.embedding),
        )
        for chunk in rankable_chunks
    ]
    scored.sort(key=lambda item: item.score, reverse=True)
    return scored[: settings.retrieval_top_k]


def retrieve_sources(tenant_id: str, knowledge_base_id: str, question: str) -> list[SourceDocument]:
    return to_sources(retrieve_ranked_chunks(tenant_id, knowledge_base_id, question))


def cosine_similarity(left: list[float], right: list[float]) -> float:
    if not left or not right or len(left) != len(right):
        return 0.0

    numerator = sum(a * b for a, b in zip(left, right))
    left_norm = math.sqrt(sum(value * value for value in left))
    right_norm = math.sqrt(sum(value * value for value in right))
    if left_norm == 0 or right_norm == 0:
        return 0.0
    return numerator / (left_norm * right_norm)
