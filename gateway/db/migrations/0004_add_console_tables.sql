alter table tenants
add column request_quota_per_day integer not null default 500000;

alter table platform_api_keys
add column scopes text[] not null default '{}';

alter table platform_api_keys
add column last_used_at timestamptz;

create table knowledge_bases (
  id text primary key,
  tenant_id text not null references tenants(id),
  name text not null,
  status text not null check (status in ('ready', 'indexing', 'failed')),
  document_count integer not null default 0,
  chunk_count integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table documents (
  id text primary key,
  tenant_id text not null references tenants(id),
  knowledge_base_id text not null references knowledge_bases(id),
  name text not null,
  content text not null,
  status text not null check (status in ('ready', 'indexing', 'failed')),
  chunk_count integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table route_catalog (
  id text primary key,
  requested_model text not null unique,
  resolved_provider text not null,
  provider_credential_id text not null references provider_credentials(id),
  endpoint text not null,
  latency_ms integer not null default 0,
  health_status text not null check (health_status in ('healthy', 'warning', 'degraded')),
  request_mode text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table audit_logs (
  id bigserial primary key,
  tenant_id text not null references tenants(id),
  platform_api_key_id text not null references platform_api_keys(id),
  requested_model text not null default '',
  endpoint text not null,
  status_code integer not null,
  provider_display_name text not null,
  latency_ms integer not null default 0,
  created_at timestamptz not null default now()
);

create table operational_alerts (
  id bigserial primary key,
  alert_type text not null,
  scope text not null,
  severity text not null,
  created_at timestamptz not null default now()
);

create table playground_runs (
  id bigserial primary key,
  tenant_id text not null references tenants(id),
  platform_api_key_id text not null references platform_api_keys(id),
  requested_model text not null,
  prompt text not null,
  response_excerpt text not null,
  endpoint text not null,
  resolved_provider text not null,
  status_code integer not null,
  latency_ms integer not null,
  created_at timestamptz not null default now()
);

create table system_settings (
  key text primary key,
  value text not null,
  updated_at timestamptz not null default now()
);
