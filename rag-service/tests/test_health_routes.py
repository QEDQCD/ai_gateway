from fastapi.testclient import TestClient

from app.api import routes
from app.core.config import Settings
from app.main import app


def test_root_route_returns_ok():
    client = TestClient(app)

    response = client.get("/")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_healthz_route_returns_ok():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_basic_auth_does_not_protect_root_or_healthz(monkeypatch):
    monkeypatch.setattr(
        routes,
        "settings",
        Settings(
            app_name="AI Gateway RAG Service",
            database_url="postgres://example-db-user:change-me-db-password@postgres:5432/ai_gateway",
            basic_auth_username="example-rag-user",
            basic_auth_password="change-me-rag-password",
            llm_base_url="https://dashscope.aliyuncs.com/compatible-mode/v1",
            llm_api_key="",
            llm_chat_model="qwen-flash",
            llm_embedding_model="text-embedding-v4",
            retrieval_top_k=3,
        ),
    )
    client = TestClient(app)

    root_response = client.get("/")
    health_response = client.get("/healthz")

    assert root_response.status_code == 200
    assert root_response.json() == {"status": "ok"}
    assert health_response.status_code == 200
    assert health_response.json() == {"status": "ok"}


def test_basic_auth_still_protects_internal_rag_routes(monkeypatch):
    monkeypatch.setattr(
        routes,
        "settings",
        Settings(
            app_name="AI Gateway RAG Service",
            database_url="postgres://example-db-user:change-me-db-password@postgres:5432/ai_gateway",
            basic_auth_username="example-rag-user",
            basic_auth_password="change-me-rag-password",
            llm_base_url="https://dashscope.aliyuncs.com/compatible-mode/v1",
            llm_api_key="",
            llm_chat_model="qwen-flash",
            llm_embedding_model="text-embedding-v4",
            retrieval_top_k=3,
        ),
    )
    client = TestClient(app)

    unauthorized_response = client.post(
        "/internal/rag/query",
        json={
            "tenant_id": "tenant_demo",
            "knowledge_base_id": "kb_demo",
            "question": "What is AI Gateway?",
        },
    )
    authorized_response = client.post(
        "/internal/rag/query",
        auth=("example-rag-user", "change-me-rag-password"),
        json={
            "tenant_id": "tenant_demo",
            "knowledge_base_id": "kb_demo",
            "question": "What is AI Gateway?",
        },
    )

    assert unauthorized_response.status_code == 401
    assert authorized_response.status_code == 200
