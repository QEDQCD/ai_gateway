-- name: GetPlatformAPIKeyByHash :one
select
  id,
  tenant_id,
  name,
  key_hash,
  status,
  coalesce(expires_at, created_at + interval '30 days') as expires_at,
  coalesce(created_by_user_id, '') as created_by_user_id
from platform_api_keys
where key_hash = $1;
