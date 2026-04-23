from app.models.document import SourceDocument


def retrieve_sources(knowledge_base_id: str, question: str) -> list[SourceDocument]:
    del knowledge_base_id, question
    return [
        SourceDocument(
            document_id="doc_demo",
            chunk_id="chunk_1",
            score=0.91,
        )
    ]
