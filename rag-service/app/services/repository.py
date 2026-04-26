from __future__ import annotations

from dataclasses import dataclass
import json
from typing import Iterable
from urllib.parse import urlparse
from uuid import uuid4

from app.core.config import settings
from app.models.document import SourceDocument


@dataclass(frozen=True)
class StoredDocument:
    document_id: str
    tenant_id: str
    knowledge_base_id: str
    name: str
    content: str
    chunk_count: int


@dataclass(frozen=True)
class RetrievedChunk:
    chunk_id: str
    document_id: str
    document_name: str
    content: str
    score: float
    embedding: list[float]


def load_documents(tenant_id: str, knowledge_base_id: str, limit: int | None = None) -> list[StoredDocument]:
    if not settings.database_url:
        return [
            StoredDocument(
                document_id="doc_demo",
                tenant_id=tenant_id,
                knowledge_base_id=knowledge_base_id,
                name="演示文档",
                content="AI Gateway 提供统一模型路由、审计和知识库检索能力。",
                chunk_count=1,
            )
        ]

    connection = open_database_connection(settings.database_url)
    with connection as conn:
        cur = conn.cursor()
        try:
            sql = """
                select id, tenant_id, knowledge_base_id, name, content, chunk_count
                from documents
                where tenant_id = %s and knowledge_base_id = %s
                order by updated_at desc, id asc
            """
            params: tuple = (tenant_id, knowledge_base_id)
            if limit is not None:
                sql += " limit %s"
                params = (tenant_id, knowledge_base_id, limit)
            cur.execute(sql, params)
            rows = cur.fetchall()
        finally:
            cur.close()

    return [
        StoredDocument(
            document_id=row[0],
            tenant_id=row[1],
            knowledge_base_id=row[2],
            name=row[3],
            content=row[4],
            chunk_count=row[5],
        )
        for row in rows
    ]


def load_document_chunks(tenant_id: str, knowledge_base_id: str) -> list[RetrievedChunk]:
    if not settings.database_url:
        return []

    connection = open_database_connection(settings.database_url)
    with connection as conn:
        cur = conn.cursor()
        try:
            cur.execute(
                """
                select chunk_id, document_id, document_name, content, embedding_json
                from document_chunks
                where tenant_id = %s and knowledge_base_id = %s
                order by document_id asc, chunk_index asc
                """,
                (tenant_id, knowledge_base_id),
            )
            rows = cur.fetchall()
        finally:
            cur.close()

    return [
        RetrievedChunk(
            chunk_id=row[0],
            document_id=row[1],
            document_name=row[2],
            content=row[3],
            score=0.0,
            embedding=json.loads(row[4]) if isinstance(row[4], str) else row[4],
        )
        for row in rows
    ]


def replace_document_chunks(
    tenant_id: str,
    knowledge_base_id: str,
    document_id: str,
    document_name: str,
    chunks: list[str],
    embeddings: list[list[float]],
) -> None:
    if not settings.database_url:
        return
    if len(chunks) != len(embeddings):
        raise ValueError("chunks and embeddings length mismatch")

    connection = open_database_connection(settings.database_url)
    with connection as conn:
        cur = conn.cursor()
        try:
            cur.execute("delete from document_chunks where document_id = %s", (document_id,))
            for index, (chunk, embedding) in enumerate(zip(chunks, embeddings), start=1):
                cur.execute(
                    """
                    insert into document_chunks (
                        chunk_id, tenant_id, knowledge_base_id, document_id, document_name, chunk_index, content, embedding_json
                    ) values (%s, %s, %s, %s, %s, %s, %s, %s)
                    on conflict (chunk_id) do update set
                        tenant_id = excluded.tenant_id,
                        knowledge_base_id = excluded.knowledge_base_id,
                        document_id = excluded.document_id,
                        document_name = excluded.document_name,
                        chunk_index = excluded.chunk_index,
                        content = excluded.content,
                        embedding_json = excluded.embedding_json
                    """,
                    (
                        f"{document_id}_chunk_{index}",
                        tenant_id,
                        knowledge_base_id,
                        document_id,
                        document_name,
                        index,
                        chunk,
                        json.dumps(embedding),
                    ),
                )
        finally:
            cur.close()
        conn.commit()


def upsert_document(
    tenant_id: str,
    knowledge_base_id: str,
    document_name: str,
    content: str,
    chunk_count: int,
) -> str:
    if not settings.database_url:
        return "doc_demo"

    document_id = f"doc_{uuid4().hex[:12]}"
    connection = open_database_connection(settings.database_url)
    with connection as conn:
        cur = conn.cursor()
        try:
            cur.execute(
                """
                insert into documents (id, tenant_id, knowledge_base_id, name, content, status, chunk_count)
                values (%s, %s, %s, %s, %s, 'ready', %s)
                """,
                (document_id, tenant_id, knowledge_base_id, document_name, content, chunk_count),
            )
            cur.execute(
                """
                update knowledge_bases
                set
                    document_count = (
                        select count(*)
                        from documents
                        where knowledge_base_id = %s
                    ),
                    chunk_count = (
                        select coalesce(sum(chunk_count), 0)
                        from documents
                        where knowledge_base_id = %s
                    ),
                    status = 'ready',
                    updated_at = now()
                where id = %s
                """,
                (knowledge_base_id, knowledge_base_id, knowledge_base_id),
            )
        finally:
            cur.close()
        conn.commit()

    return document_id


def ensure_document_chunks(
    tenant_id: str,
    knowledge_base_id: str,
    documents: list[StoredDocument],
    build_chunks: callable,
    build_embeddings: callable,
) -> list[RetrievedChunk]:
    existing = load_document_chunks(tenant_id, knowledge_base_id)
    existing_document_ids = {chunk.document_id for chunk in existing}

    for document in documents:
        if document.document_id in existing_document_ids:
            continue
        chunks = build_chunks(document.content)
        embeddings = build_embeddings(chunks)
        replace_document_chunks(
            tenant_id=tenant_id,
            knowledge_base_id=knowledge_base_id,
            document_id=document.document_id,
            document_name=document.name,
            chunks=chunks,
            embeddings=embeddings,
        )

    return load_document_chunks(tenant_id, knowledge_base_id)


def open_database_connection(database_url: str):
    from pg8000 import dbapi

    parsed = urlparse(database_url)
    return dbapi.connect(
        database=parsed.path.lstrip("/"),
        user=parsed.username or "",
        password=parsed.password or "",
        host=parsed.hostname or "localhost",
        port=parsed.port or 5432,
    )


def to_sources(chunks: Iterable[RetrievedChunk]) -> list[SourceDocument]:
    return [
        SourceDocument(
            document_id=chunk.document_id,
            chunk_id=chunk.chunk_id,
            score=round(chunk.score, 4),
        )
        for chunk in chunks
    ]
