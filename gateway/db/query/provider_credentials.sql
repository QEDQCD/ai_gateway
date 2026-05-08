-- name: ListActiveProviderCredentials :many
select id, provider, display_name, supported_models, base_url, encrypted_secret, secret_ref, credential_mode, status
from provider_credentials
where status = 'active'
order by created_at asc, id asc;
