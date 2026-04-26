from app.services import answerer
from app.services.repository import RetrievedChunk


def test_answer_question_uses_llm_completion(monkeypatch):
    monkeypatch.setattr(answerer, "complete_answer", lambda question, context_chunks: f"回答: {question} / {len(context_chunks)}")

    result = answerer.answer_question(
        "AI Gateway 是什么？",
        [
            RetrievedChunk(
                chunk_id="chunk_a",
                document_id="doc_a",
                document_name="文档A",
                content="AI Gateway 提供统一模型路由。",
                embedding=[0.1, 0.2],
                score=0.9,
            )
        ],
    )

    assert result == "回答: AI Gateway 是什么？ / 1"


def test_answer_question_returns_fallback_without_chunks():
    result = answerer.answer_question("AI Gateway 是什么？", [])

    assert "未在知识库中找到" in result
