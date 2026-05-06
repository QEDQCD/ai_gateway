-- name: GetTenantByID :one
select
  t.id,
  t.name,
  t.status,
  coalesce(q.allowed_models, '{}')::text[] as allowed_models
from tenants t
left join tenant_quota_policies q on q.tenant_id = t.id
where t.id = $1;

-- name: ListActiveTenants :many
select id, name, status
from tenants
where status = 'active';
