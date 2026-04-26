alter table platform_api_keys
  add constraint platform_api_keys_id_tenant_key unique (id, tenant_id);

create table llm_request_logs (
  id text primary key,
  tenant_id text not null references tenants(id),
  platform_api_key_id text not null,
  platform_api_key_name text not null,
  provider_credential_id text not null references provider_credentials(id),
  route_id text not null,
  request_path text not null,
  request_model text not null,
  upstream_model text not null default '',
  usage_source text not null check (usage_source in ('upstream', 'estimated')),
  usage_status text not null check (usage_status in ('success', 'failed', 'timeout', 'rate_limited', 'auth_failed', 'upstream_error')),
  status_code integer not null,
  latency_ms integer not null default 0 check (latency_ms >= 0),
  prompt_tokens integer not null default 0 check (prompt_tokens >= 0),
  completion_tokens integer not null default 0 check (completion_tokens >= 0),
  total_tokens integer not null default 0 check (total_tokens >= 0),
  error_code text not null default '',
  error_message text not null default '',
  request_started_at timestamptz not null,
  request_completed_at timestamptz not null,
  created_at timestamptz not null default now(),
  constraint llm_request_logs_id_tenant_key unique (id, tenant_id),
  constraint llm_request_logs_platform_api_key_tenant_fkey
    foreign key (platform_api_key_id, tenant_id)
    references platform_api_keys(id, tenant_id),
  check (request_completed_at >= request_started_at)
);

create index idx_llm_request_logs_tenant_created_at
  on llm_request_logs (tenant_id, created_at desc);

create index idx_llm_request_logs_tenant_request_started_at
  on llm_request_logs (tenant_id, request_started_at desc);

create index idx_llm_request_logs_platform_api_key_created_at
  on llm_request_logs (platform_api_key_id, created_at desc);

create index idx_llm_request_logs_platform_api_key_request_started_at
  on llm_request_logs (platform_api_key_id, request_started_at desc);

create index idx_llm_request_logs_provider_credential_created_at
  on llm_request_logs (provider_credential_id, created_at desc);

create index idx_llm_request_logs_provider_credential_request_started_at
  on llm_request_logs (provider_credential_id, request_started_at desc);

create table llm_request_events (
  id text primary key,
  request_log_id text not null,
  tenant_id text not null references tenants(id),
  event_type text not null,
  usage_source text not null check (usage_source in ('upstream', 'estimated')),
  usage_status text not null check (usage_status in ('success', 'failed', 'timeout', 'rate_limited', 'auth_failed', 'upstream_error')),
  status_code integer not null default 0,
  detail text not null default '',
  constraint llm_request_events_request_log_tenant_fkey
    foreign key (request_log_id, tenant_id)
    references llm_request_logs(id, tenant_id)
    on delete cascade,
  created_at timestamptz not null default now()
);

create index idx_llm_request_events_request_log_created_at
  on llm_request_events (request_log_id, created_at asc);

create index idx_llm_request_events_tenant_created_at
  on llm_request_events (tenant_id, created_at desc);

create table llm_usage_agg_hourly (
  bucket_start timestamptz not null,
  tenant_id text not null references tenants(id),
  platform_api_key_id text not null,
  provider_credential_id text not null references provider_credentials(id),
  route_id text not null,
  request_path text not null,
  usage_source text not null check (usage_source in ('upstream', 'estimated')),
  usage_status text not null check (usage_status in ('success', 'failed', 'timeout', 'rate_limited', 'auth_failed', 'upstream_error')),
  request_count integer not null default 0 check (request_count >= 0),
  prompt_tokens integer not null default 0 check (prompt_tokens >= 0),
  completion_tokens integer not null default 0 check (completion_tokens >= 0),
  total_tokens integer not null default 0 check (total_tokens >= 0),
  constraint llm_usage_agg_hourly_platform_api_key_tenant_fkey
    foreign key (platform_api_key_id, tenant_id)
    references platform_api_keys(id, tenant_id),
  primary key (
    bucket_start,
    tenant_id,
    platform_api_key_id,
    provider_credential_id,
    route_id,
    request_path,
    usage_source,
    usage_status
  )
);

create index idx_llm_usage_agg_hourly_tenant_bucket
  on llm_usage_agg_hourly (tenant_id, bucket_start desc);

create index idx_llm_usage_agg_hourly_platform_key_bucket
  on llm_usage_agg_hourly (platform_api_key_id, bucket_start desc);
