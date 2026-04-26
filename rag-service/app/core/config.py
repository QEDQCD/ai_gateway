from dataclasses import dataclass
import os


@dataclass(frozen=True)
class Settings:
    app_name: str
    database_url: str
    basic_auth_username: str
    basic_auth_password: str
    llm_base_url: str
    llm_api_key: str
    llm_chat_model: str
    llm_embedding_model: str
    retrieval_top_k: int


def resolve_env_or_file(name: str) -> str:
    value = os.getenv(name, "")
    if value:
        return value

    file_path = os.getenv(f"{name}_FILE", "")
    if not file_path:
        return ""

    with open(file_path, "r", encoding="utf-8") as handle:
        return handle.read().strip()


def load_settings() -> Settings:
    return Settings(
        app_name=os.getenv("RAG_SERVICE_NAME", "AI Gateway RAG Service"),
        database_url=os.getenv("RAG_DATABASE_URL", ""),
        basic_auth_username=os.getenv("RAG_SERVICE_BASIC_AUTH_USERNAME", ""),
        basic_auth_password=os.getenv("RAG_SERVICE_BASIC_AUTH_PASSWORD", ""),
        llm_base_url=os.getenv("RAG_LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
        llm_api_key=resolve_env_or_file("RAG_LLM_API_KEY"),
        llm_chat_model=os.getenv("RAG_LLM_CHAT_MODEL", "qwen-flash"),
        llm_embedding_model=os.getenv("RAG_LLM_EMBEDDING_MODEL", "text-embedding-v4"),
        retrieval_top_k=int(os.getenv("RAG_RETRIEVAL_TOP_K", "3")),
    )


settings = load_settings()
