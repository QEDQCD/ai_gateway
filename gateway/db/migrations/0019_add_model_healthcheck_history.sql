create table if not exists model_healthcheck_history (
  id text primary key,
  route_id text not null references route_catalog(id) on delete cascade,
  requested_model text not null,
  provider_credential_id text not null,
  route_label text not null default '',
  health_status text not null check (health_status in ('healthy', 'warning', 'degraded')),
  last_health_error text not null default '',
  request_mode text not null default '',
  latency_ms integer not null default 0,
  first_token_latency_ms integer not null default 0,
  checked_at timestamptz not null default now(),
  created_at timestamptz not null default now()
);

create index if not exists idx_model_healthcheck_history_route_checked_at
  on model_healthcheck_history(route_id, checked_at desc);

create index if not exists idx_model_healthcheck_history_checked_at
  on model_healthcheck_history(checked_at desc);
