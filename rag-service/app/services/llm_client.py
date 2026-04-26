from __future__ import annotations

import hashlib
import json
import math
from typing import Iterable
from urllib import request

from app.core.config import settings


def embed_texts(texts: Iterable[str]) -> list[list[float]]:
    normalized = [text.strip() for text in texts]
    if not normalized:
        return []

    if not settings.llm_api_key:
        return [local_embed_text(text) for text in normalized]

    try:
        payload = {
            "model": settings.llm_embedding_model,
            "input": normalized,
        }
        response = post_json("/embeddings", payload)
        embeddings = [item["embedding"] for item in response.get("data", [])]
        if len(embeddings) == len(normalized):
            return embeddings
    except Exception:
        pass

    return [local_embed_text(text) for text in normalized]


def complete_answer(question: str, context_chunks: list[str]) -> str:
    if not context_chunks:
        return f"未在知识库中找到与“{question}”相关的内容。"

    if not settings.llm_api_key:
        summary = "；".join(chunk[:80] for chunk in context_chunks[:3])
        return f"根据知识库内容，关于“{question}”可参考：{summary}"

    try:
        context_block = "\n\n".join(
            f"[片段 {index}]\n{chunk}" for index, chunk in enumerate(context_chunks[:4], start=1)
        )
        payload = {
            "model": settings.llm_chat_model,
            "messages": [
                {
                    "role": "system",
                    "content": "你是企业知识库问答助手。只能根据提供的知识片段回答，回答必须使用中文，内容简洁准确。",
                },
                {
                    "role": "user",
                    "content": f"问题：{question}\n\n知识片段：\n{context_block}\n\n请基于片段直接作答。",
                },
            ],
            "temperature": 0.2,
        }
        response = post_json("/chat/completions", payload)
        choices = response.get("choices", [])
        if choices:
            return choices[0]["message"]["content"].strip()
    except Exception:
        pass

    summary = "；".join(chunk[:80] for chunk in context_chunks[:3])
    return f"根据知识库内容，关于“{question}”可参考：{summary}"


def post_json(path: str, payload: dict) -> dict:
    body = json.dumps(payload).encode("utf-8")
    req = request.Request(
        settings.llm_base_url.rstrip("/") + path,
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {settings.llm_api_key}",
            "Content-Type": "application/json",
        },
    )
    with request.urlopen(req, timeout=30) as response:
        return json.loads(response.read().decode("utf-8"))


def local_embed_text(text: str, dimensions: int = 32) -> list[float]:
    vector = [0.0] * dimensions
    tokens = tokenize(text)
    if not tokens:
        return vector

    for token in tokens:
        digest = hashlib.sha256(token.encode("utf-8")).digest()
        bucket = digest[0] % dimensions
        sign = 1.0 if digest[1] % 2 == 0 else -1.0
        weight = 1.0 + (digest[2] / 255.0)
        vector[bucket] += sign * weight

    norm = math.sqrt(sum(value * value for value in vector))
    if norm == 0:
        return vector
    return [value / norm for value in vector]


def tokenize(text: str) -> list[str]:
    cleaned = "".join(character.lower() if character.isalnum() else " " for character in text)
    return [token for token in cleaned.split() if token]
