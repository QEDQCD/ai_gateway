from fastapi import FastAPI
from pydantic import BaseModel


class ChatMessage(BaseModel):
    role: str
    content: str


class ChatRequest(BaseModel):
    model: str
    messages: list[ChatMessage]


class EmbeddingsRequest(BaseModel):
    model: str
    input: str | list[str]


app = FastAPI(title="AI Gateway Model Service")


@app.post("/v1/chat/completions")
def chat_completions(payload: ChatRequest) -> dict:
    prompt = payload.messages[-1].content if payload.messages else ""
    return {
        "choices": [
            {
                "message": {
                    "content": f"模型服务已收到请求：{prompt}",
                }
            }
        ]
    }


@app.post("/v1/embeddings")
def embeddings(payload: EmbeddingsRequest) -> dict:
    values = payload.input if isinstance(payload.input, list) else [payload.input]
    vector = [round(0.1 + (index * 0.1), 2) for index, _ in enumerate(values[:3], start=0)]
    if not vector:
        vector = [0.1, 0.2, 0.3]
    return {"data": [{"embedding": vector}]}
