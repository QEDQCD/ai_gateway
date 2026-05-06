create table account_deletion_applications (
  id text primary key,
  user_id text not null references users(id),
  tenant_id text not null references tenants(id),
  reason text not null default '',
  status text not null check (status in ('pending', 'approved', 'rejected')),
  reviewer_id text references users(id),
  review_comment text not null default '',
  reviewed_at timestamptz,
  disabled_api_keys integer not null default 0 check (disabled_api_keys >= 0),
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

create unique index account_deletion_applications_one_pending_per_user
  on account_deletion_applications (user_id)
  where status = 'pending';

create index account_deletion_applications_status_created_at_idx
  on account_deletion_applications (status, created_at desc);
