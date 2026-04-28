alter table platform_api_keys
  add column key_ciphertext text not null default '',
  add column key_kek_version text not null default 'v1',
  add column created_by_user_id text references users(id),
  add column expires_at timestamptz,
  add column rotated_from_key_id text references platform_api_keys(id),
  add column disabled_at timestamptz,
  add column disabled_reason text not null default '',
  add column secret_recoverable boolean not null default false;

update platform_api_keys
set expires_at = created_at + interval '30 days'
where expires_at is null;

create table tenant_quota_policies (
  tenant_id text primary key references tenants(id),
  period_type text not null check (period_type in ('monthly')),
  request_limit bigint not null check (request_limit > 0),
  token_limit bigint not null check (token_limit > 0),
  effective_from timestamptz not null default now(),
  created_by text references users(id),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table tenant_quota_usage_periods (
  tenant_id text not null references tenants(id),
  period_start timestamptz not null,
  period_end timestamptz not null,
  requests_used bigint not null default 0 check (requests_used >= 0),
  tokens_used bigint not null default 0 check (tokens_used >= 0),
  last_aggregated_at timestamptz not null default now(),
  primary key (tenant_id, period_start),
  check (period_end > period_start)
);

create table api_key_secret_access_logs (
  id text primary key,
  api_key_id text not null references platform_api_keys(id) on delete cascade,
  tenant_id text not null references tenants(id),
  actor_user_id text references users(id),
  actor_role text not null check (actor_role in ('admin', 'member')),
  action text not null check (action in ('reveal', 'copy')),
  access_result text not null check (access_result in ('allowed', 'denied')),
  ip_address text not null default '',
  user_agent text not null default '',
  created_at timestamptz not null default now()
);
