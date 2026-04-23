-- name: ListActiveProviderCredentials :many
select id, provider, display_name, supported_models, encrypted_secret, status
from provider_credentials
where status = 'active';
