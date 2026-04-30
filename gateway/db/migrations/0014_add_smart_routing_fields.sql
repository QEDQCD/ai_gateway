alter table llm_request_logs
  add column task_class text not null default '',
  add column routing_reason text not null default '',
  add column target_model_tier text not null default '',
  add column resolved_model text not null default '';
