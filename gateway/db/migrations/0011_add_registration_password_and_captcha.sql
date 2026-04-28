alter table account_applications
  add column if not exists password_hash text,
  add column if not exists email_normalized text not null default '';

update account_applications
set email_normalized = lower(trim(email))
where email_normalized = '';

alter table account_applications
  alter column email_normalized drop default;

create unique index if not exists account_applications_pending_email_normalized_idx
  on account_applications (email_normalized)
  where status = 'pending';

create table if not exists captcha_challenges (
  id text primary key,
  answer_hash text not null,
  status text not null check (status in ('issued', 'verified', 'consumed', 'expired', 'failed')),
  verify_attempts integer not null default 0 check (verify_attempts >= 0),
  max_attempts integer not null default 5 check (max_attempts > 0),
  pass_token_hash text not null default '',
  issued_ip text not null default '',
  issued_user_agent text not null default '',
  verified_at timestamptz,
  consumed_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
