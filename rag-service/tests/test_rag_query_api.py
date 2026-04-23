from fastapi.testclient import TestClient

from app.main import app


def test_rag_query_returns_answer_and_sources():
    client = TestClient(app)
    response = client.post(
        "/internal/rag/query",
        json={
            "tenant_id": "tenant_demo",
            "knowledge_base_id": "kb_demo",
            "question": "What is AI Gateway?",
        },
    )

    assert response.status_code == 200
    data = response.json()
    assert "answer" in data
    assert "sources" in data


def test_ingest_document_returns_queued_status():
    client = TestClient(app)
    response = client.post(
        "/internal/rag/ingest",
        json={
            "tenant_id": "tenant_demo",
            "knowledge_base_id": "kb_demo",
            "document_name": "gateway.txt",
            "content": "AI Gateway overview",
        },
    )

    assert response.status_code == 200
    data = response.json()
    assert data["document_id"] == "doc_demo"
    assert data["status"] == "queued"
