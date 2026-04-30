alter table llm_request_logs
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_price_microyuan_per_million bigint not null default 0 check (input_price_microyuan_per_million >= 0),
  add column output_price_microyuan_per_million bigint not null default 0 check (output_price_microyuan_per_million >= 0),
  add column cached_price_microyuan_per_million bigint not null default 0 check (cached_price_microyuan_per_million >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);

alter table llm_usage_agg_hourly
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);

alter table tenant_usage_ledger
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);
