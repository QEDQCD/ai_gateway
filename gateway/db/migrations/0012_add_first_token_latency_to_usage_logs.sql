alter table llm_request_logs
  add column first_token_latency_ms integer
  check (first_token_latency_ms is null or first_token_latency_ms >= 0);
