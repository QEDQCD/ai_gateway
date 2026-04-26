create table document_chunks (
  chunk_id text primary key,
  tenant_id text not null references tenants(id),
  knowledge_base_id text not null references knowledge_bases(id),
  document_id text not null references documents(id) on delete cascade,
  document_name text not null,
  chunk_index integer not null,
  content text not null,
  embedding_json jsonb not null,
  created_at timestamptz not null default now()
);

create index idx_document_chunks_knowledge_base
  on document_chunks (tenant_id, knowledge_base_id, document_id, chunk_index);
