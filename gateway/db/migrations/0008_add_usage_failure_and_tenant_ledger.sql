create table llm_request_failures (
  id text primary key,
  request_log_id text not null,
  tenant_id text not null references tenants(id),
  user_id text references users(id),
  platform_api_key_id text not null,
  failure_stage text not null check (failure_stage in ('request', 'upstream', 'response', 'publish', 'internal')),
  error_category text not null check (error_category in ('failed', 'rate_limited', 'auth_failed', 'timeout', 'upstream_error', 'publish_failure', 'internal_error')),
  status_code integer not null default 0,
  retryable boolean not null default false,
  user_message text not null default '',
  internal_message_digest text not null default '',
  created_at timestamptz not null default now(),
  constraint llm_request_failures_request_log_tenant_fkey
    foreign key (request_log_id, tenant_id)
    references llm_request_logs(id, tenant_id)
    on delete cascade,
  constraint llm_request_failures_platform_api_key_tenant_fkey
    foreign key (platform_api_key_id, tenant_id)
    references platform_api_keys(id, tenant_id)
);

create index idx_llm_request_failures_tenant_created_at
  on llm_request_failures (tenant_id, created_at desc);

create index idx_llm_request_failures_user_created_at
  on llm_request_failures (user_id, created_at desc);

create table tenant_usage_ledger (
  bucket_start timestamptz not null,
  tenant_id text not null references tenants(id),
  input_tokens integer not null default 0 check (input_tokens >= 0),
  output_tokens integer not null default 0 check (output_tokens >= 0),
  total_tokens integer not null default 0 check (total_tokens >= 0),
  request_count integer not null default 0 check (request_count >= 0),
  success_count integer not null default 0 check (success_count >= 0),
  failure_count integer not null default 0 check (failure_count >= 0),
  estimated_count integer not null default 0 check (estimated_count >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (bucket_start, tenant_id)
);

create index idx_tenant_usage_ledger_tenant_bucket
  on tenant_usage_ledger (tenant_id, bucket_start desc);
