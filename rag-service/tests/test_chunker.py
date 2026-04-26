from app.services.chunker import chunk_document


def test_chunk_document_splits_long_content():
    content = "\n".join(
        [
            "第一段介绍 AI Gateway 的统一接入能力。" * 8,
            "第二段介绍配额、审计和路由治理能力。" * 8,
            "第三段介绍知识库检索与回答生成。" * 8,
        ]
    )

    chunks = chunk_document(content, max_chars=120, overlap=20)

    assert len(chunks) >= 3
    assert all(len(chunk) <= 140 for chunk in chunks)
    assert "AI Gateway" in chunks[0]
