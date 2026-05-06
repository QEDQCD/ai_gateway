alter table tenant_quota_policies
  add column allowed_models text[] not null default '{}';
