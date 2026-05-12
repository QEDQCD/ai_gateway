alter table llm_request_logs
  add column cache_hit boolean not null default false,
  add column cache_type text not null default '',
  add column cache_key text not null default '',
  add column cache_faq_key text not null default '',
  add column classifier_model text not null default '',
  add column classifier_status text not null default '',
  add column classifier_latency_ms integer not null default 0
  check (classifier_latency_ms >= 0);
