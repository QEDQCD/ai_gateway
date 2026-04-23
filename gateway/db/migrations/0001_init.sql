create table tenants (
  id text primary key,
  name text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table platform_api_keys (
  id text primary key,
  tenant_id text not null references tenants(id),
  name text not null,
  key_hash text not null unique,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table provider_credentials (
  id text primary key,
  provider text not null,
  display_name text not null,
  supported_models text[] not null default '{}',
  encrypted_secret text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table byok_credentials (
  id text primary key,
  tenant_id text not null references tenants(id),
  provider text not null,
  encrypted_secret text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);
