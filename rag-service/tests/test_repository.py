import json
import sys
import types

from app.core.config import Settings
from app.services import repository


class FakeCursor:
    def __init__(self, rows=None):
        self.rows = rows or []
        self.executions = []

    def execute(self, sql, params):
        self.executions.append((" ".join(sql.split()), params))

    def fetchall(self):
        return self.rows

    def close(self):
        return None


class FakeConnection:
    def __init__(self, rows=None):
        self.cursor_instance = FakeCursor(rows=rows)
        self.committed = False

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False

    def cursor(self):
        return self.cursor_instance

    def commit(self):
        self.committed = True


def install_fake_pg8000(monkeypatch, connection):
    fake_package = types.ModuleType("pg8000")
    fake_dbapi = types.ModuleType("pg8000.dbapi")

    def connect(*, database, user, password, host, port):
        assert database == "ai_gateway"
        assert user == "example-db-user"
        assert password == "change-me-db-password"
        assert host == "postgres"
        assert port == 5432
        return connection

    fake_dbapi.connect = connect
    fake_package.dbapi = fake_dbapi

    monkeypatch.setitem(sys.modules, "pg8000", fake_package)
    monkeypatch.setitem(sys.modules, "pg8000.dbapi", fake_dbapi)


def build_settings():
    return Settings(
        app_name="AI Gateway RAG Service",
        database_url="postgres://example-db-user:change-me-db-password@postgres:5432/ai_gateway",
        basic_auth_username="",
        basic_auth_password="",
        llm_base_url="https://dashscope.aliyuncs.com/compatible-mode/v1",
        llm_api_key="",
        llm_chat_model="qwen-flash",
        llm_embedding_model="text-embedding-v4",
        retrieval_top_k=3,
    )


def test_load_documents_uses_pg8000_database_driver(monkeypatch):
    connection = FakeConnection(
        rows=[
            ("doc_a", "tenant_alpha", "kb_product_docs", "产品文档", "内容A", 8),
            ("doc_b", "tenant_alpha", "kb_product_docs", "部署说明", "内容B", 4),
        ]
    )
    install_fake_pg8000(monkeypatch, connection)
    monkeypatch.setattr(repository, "settings", build_settings())

    documents = repository.load_documents("tenant_alpha", "kb_product_docs", limit=2)

    assert [document.document_id for document in documents] == ["doc_a", "doc_b"]
    assert connection.cursor_instance.executions[0][1] == ("tenant_alpha", "kb_product_docs", 2)


def test_replace_document_chunks_persists_embeddings(monkeypatch):
    connection = FakeConnection()
    install_fake_pg8000(monkeypatch, connection)
    monkeypatch.setattr(repository, "settings", build_settings())

    repository.replace_document_chunks(
        tenant_id="tenant_alpha",
        knowledge_base_id="kb_product_docs",
        document_id="doc_alpha",
        document_name="产品文档",
        chunks=["第一段", "第二段"],
        embeddings=[[0.1, 0.2], [0.3, 0.4]],
    )

    assert len(connection.cursor_instance.executions) == 3
    assert connection.cursor_instance.executions[0][1] == ("doc_alpha",)
    assert json.loads(connection.cursor_instance.executions[1][1][-1]) == [0.1, 0.2]
    assert connection.committed is True


def test_upsert_document_uses_pg8000_database_driver(monkeypatch):
    connection = FakeConnection()
    install_fake_pg8000(monkeypatch, connection)
    monkeypatch.setattr(repository, "settings", build_settings())
    monkeypatch.setattr(repository, "uuid4", lambda: types.SimpleNamespace(hex="1234567890abcdef"))

    document_id = repository.upsert_document(
        tenant_id="tenant_alpha",
        knowledge_base_id="kb_product_docs",
        document_name="新增文档",
        content="AI Gateway 支持统一路由。",
        chunk_count=6,
    )

    assert document_id == "doc_1234567890ab"
    assert len(connection.cursor_instance.executions) == 2
    assert connection.committed is True
