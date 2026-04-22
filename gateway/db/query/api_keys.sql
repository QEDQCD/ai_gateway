-- name: GetPlatformAPIKeyByHash :one
select id, tenant_id, name, key_hash, status
from platform_api_keys
where key_hash = $1;
