from pydantic import BaseModel


class SourceDocument(BaseModel):
    document_id: str
    chunk_id: str
    score: float


class RagQueryRequest(BaseModel):
    tenant_id: str
    knowledge_base_id: str
    question: str


class RagQueryResponse(BaseModel):
    answer: str
    sources: list[SourceDocument]


class IngestDocumentRequest(BaseModel):
    tenant_id: str
    knowledge_base_id: str
    document_name: str
    content: str


class IngestDocumentResponse(BaseModel):
    document_id: str
    status: str
