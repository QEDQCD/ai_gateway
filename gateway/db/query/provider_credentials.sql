-- name: ListActiveProviderCredentials :many
select id, provider, display_name, supported_models, base_url, encrypted_secret, status
from provider_credentials
where status = 'active'
order by created_at asc, id asc;
