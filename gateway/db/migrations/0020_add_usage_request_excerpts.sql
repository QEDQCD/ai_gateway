alter table llm_request_logs
add column prompt_excerpt text not null default '',
add column response_excerpt text not null default '';
