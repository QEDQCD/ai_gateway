-- name: ListActiveProviderCredentials :many
select id, provider, display_name, encrypted_secret, status
from provider_credentials
where status = 'active';
