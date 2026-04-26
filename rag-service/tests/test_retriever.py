from app.services import retriever
from app.services.repository import RetrievedChunk


def test_retrieve_ranked_chunks_orders_by_similarity(monkeypatch):
    monkeypatch.setattr(
        retriever,
        "load_documents",
        lambda tenant_id, knowledge_base_id: [],
    )
    monkeypatch.setattr(
        retriever,
        "ensure_document_chunks",
        lambda **kwargs: [
            RetrievedChunk(
                chunk_id="chunk_b",
                document_id="doc_b",
                document_name="文档B",
                content="embedding b",
                embedding=[0.0, 1.0],
                score=0.0,
            ),
            RetrievedChunk(
                chunk_id="chunk_a",
                document_id="doc_a",
                document_name="文档A",
                content="embedding a",
                embedding=[1.0, 0.0],
                score=0.0,
            ),
        ],
    )
    monkeypatch.setattr(retriever, "embed_texts", lambda texts: [[1.0, 0.0]])

    ranked = retriever.retrieve_ranked_chunks("tenant_alpha", "kb_product_docs", "网关")

    assert [chunk.chunk_id for chunk in ranked] == ["chunk_a", "chunk_b"]
    assert ranked[0].score > ranked[1].score
