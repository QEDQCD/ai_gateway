-- name: GetTenantByID :one
select id, name, status
from tenants
where id = $1;

-- name: ListActiveTenants :many
select id, name, status
from tenants
where status = 'active';
