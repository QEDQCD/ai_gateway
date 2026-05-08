alter table tenant_quota_policies
  add column cost_limit_microyuan bigint not null default 0 check (cost_limit_microyuan >= 0);
