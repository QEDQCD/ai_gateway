from dataclasses import dataclass
import os


@dataclass(frozen=True)
class Settings:
    app_name: str


def load_settings() -> Settings:
    return Settings(
        app_name=os.getenv("RAG_SERVICE_NAME", "AI Gateway RAG Service"),
    )


settings = load_settings()
