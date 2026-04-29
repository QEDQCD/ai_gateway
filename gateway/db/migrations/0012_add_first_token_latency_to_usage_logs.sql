alter table llm_request_logs
  add column first_token_latency_ms integer not null default 0
  check (first_token_latency_ms >= 0);
