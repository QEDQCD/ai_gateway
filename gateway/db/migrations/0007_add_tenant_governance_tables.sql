create table users (
  id text primary key,
  email text not null unique,
  name text not null,
  role text not null check (role in ('admin', 'member')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table account_applications (
  id text primary key,
  email text not null,
  name text not null,
  company_name text not null default '',
  use_case text not null default '',
  status text not null check (status in ('pending', 'approved', 'rejected')),
  reviewer_id text references users(id),
  review_comment text not null default '',
  reviewed_at timestamptz,
  created_at timestamptz not null default now(),
  check (
    (
      status = 'pending'
      and reviewer_id is null
      and review_comment = ''
      and reviewed_at is null
    ) or (
      status in ('approved', 'rejected')
      and reviewer_id is not null
      and reviewer_id <> ''
      and reviewed_at is not null
    )
  )
);

create table tenant_memberships (
  id text primary key,
  tenant_id text not null references tenants(id),
  user_id text not null references users(id),
  role text not null check (role in ('member')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now(),
  unique (tenant_id, user_id)
);

create table audit_events (
  id text primary key,
  actor_type text not null check (actor_type in ('admin', 'member', 'system')),
  actor_user_id text references users(id),
  tenant_id text references tenants(id),
  event_type text not null,
  target_type text not null,
  target_id text not null default '',
  detail text not null default '',
  ip_digest text not null default '',
  created_at timestamptz not null default now(),
  check (
    (
      actor_type = 'system'
      and actor_user_id is null
    ) or (
      actor_type in ('admin', 'member')
      and actor_user_id is not null
      and actor_user_id <> ''
    )
  )
);
