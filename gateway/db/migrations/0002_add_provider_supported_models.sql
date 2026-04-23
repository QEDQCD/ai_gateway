alter table provider_credentials
add column supported_models text[] not null default '{}';
