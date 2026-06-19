alter table llm_request_logs
  add column user_id text references users(id);

create index idx_llm_request_logs_user_request_started_at
  on llm_request_logs (user_id, request_started_at desc)
  where user_id is not null;

create index idx_llm_request_logs_tenant_user_request_started_at
  on llm_request_logs (tenant_id, user_id, request_started_at desc)
  where user_id is not null;

update llm_request_logs l
set user_id = p.created_by_user_id
from platform_api_keys p
where l.platform_api_key_id = p.id
  and l.tenant_id = p.tenant_id
  and p.created_by_user_id is not null
  and l.user_id is null;

create table user_usage_ledger (
  bucket_start timestamptz not null,
  tenant_id text not null references tenants(id),
  user_id text not null references users(id),
  input_tokens integer not null default 0 check (input_tokens >= 0),
  output_tokens integer not null default 0 check (output_tokens >= 0),
  total_tokens integer not null default 0 check (total_tokens >= 0),
  cached_tokens integer not null default 0 check (cached_tokens >= 0),
  input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0),
  request_count integer not null default 0 check (request_count >= 0),
  success_count integer not null default 0 check (success_count >= 0),
  failure_count integer not null default 0 check (failure_count >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (bucket_start, tenant_id, user_id)
);

create index idx_user_usage_ledger_tenant_bucket
  on user_usage_ledger (tenant_id, bucket_start desc);

create index idx_user_usage_ledger_user_bucket
  on user_usage_ledger (user_id, bucket_start desc);
