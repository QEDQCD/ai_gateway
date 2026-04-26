from app.services.llm_client import complete_answer
from app.services.repository import RetrievedChunk


def answer_question(question: str, chunks: list[RetrievedChunk]) -> str:
    if not chunks:
        return f"未在知识库中找到与“{question}”相关的内容。"

    context_chunks = [chunk.content for chunk in chunks]
    return complete_answer(question, context_chunks)
