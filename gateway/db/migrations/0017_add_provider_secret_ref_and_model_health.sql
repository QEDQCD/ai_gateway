alter table provider_credentials
add column secret_ref text not null default '',
add column credential_mode text not null default 'encrypted' check (credential_mode in ('encrypted', 'secret_ref'));

alter table route_catalog
add column status text not null default 'active' check (status in ('active', 'disabled')),
add column healthcheck_enabled boolean not null default false,
add column healthcheck_assertion_type text not null default 'non_empty' check (healthcheck_assertion_type in ('non_empty')),
add column last_health_checked_at timestamptz,
add column last_health_error text not null default '',
add column first_token_latency_ms integer not null default 0;
