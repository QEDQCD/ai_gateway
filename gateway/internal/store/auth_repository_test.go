package store

import (
	"context"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/secret"
)

func TestSQLAuthRepositoryListActiveProviderCredentialsMapsSupportedModels(t *testing.T) {
	t.Parallel()

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}
	encryptedSecret, err := codec.Encrypt("provider-secret")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	queries := fakeAuthQueries{
		providerCredentials: []ListActiveProviderCredentialsRow{
			{
				ID:              "pc_123",
				Provider:        "dashscope",
				DisplayName:     "DashScope Primary",
				SupportedModels: []string{"qwen-flash"},
				BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
				EncryptedSecret: encryptedSecret,
				Status:          string(domain.StatusActive),
			},
		},
	}

	repo := NewAuthRepository(queries, codec)

	credentials, err := repo.ListActiveProviderCredentials(context.Background())
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials returned error: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}

	got := credentials[0]
	if got.ID != "pc_123" {
		t.Fatalf("expected credential id %q, got %q", "pc_123", got.ID)
	}
	if got.Provider != "dashscope" {
		t.Fatalf("expected provider %q, got %q", "dashscope", got.Provider)
	}
	if got.DisplayName != "DashScope Primary" {
		t.Fatalf("expected display name %q, got %q", "DashScope Primary", got.DisplayName)
	}
	if got.Status != domain.StatusActive {
		t.Fatalf("expected status %q, got %q", domain.StatusActive, got.Status)
	}
	if got.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("expected base URL %q, got %q", "https://dashscope.aliyuncs.com/compatible-mode/v1", got.BaseURL)
	}
	if len(got.SupportedModels) != 1 {
		t.Fatalf("expected 1 supported model, got %d", len(got.SupportedModels))
	}
	if got.SupportedModels[0] != "qwen-flash" {
		t.Fatalf("expected supported models to round-trip, got %#v", got.SupportedModels)
	}
	if got.APIKey != "provider-secret" {
		t.Fatalf("expected decrypted api key %q, got %q", "provider-secret", got.APIKey)
	}
}

func TestSQLAuthRepositoryListActiveProviderCredentialsDecryptsSecrets(t *testing.T) {
	t.Parallel()

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}
	encryptedSecret, err := codec.Encrypt("provider-secret-key")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	queries := fakeAuthQueries{
		providerCredentials: []ListActiveProviderCredentialsRow{
			{
				ID:              "pc_123",
				Provider:        "dashscope",
				DisplayName:     "DashScope Primary",
				EncryptedSecret: encryptedSecret,
				SupportedModels: []string{"qwen-flash"},
				Status:          string(domain.StatusActive),
			},
		},
	}

	repo := NewAuthRepository(queries, codec)

	credentials, err := repo.ListActiveProviderCredentials(context.Background())
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials returned error: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}
	if credentials[0].APIKey != "provider-secret-key" {
		t.Fatalf("expected decrypted api key, got %q", credentials[0].APIKey)
	}
}

type fakeAuthQueries struct {
	platformKey         GetPlatformAPIKeyByHashRow
	tenant              GetTenantByIDRow
	providerCredentials []ListActiveProviderCredentialsRow
}

func (f fakeAuthQueries) GetPlatformAPIKeyByHash(context.Context, string) (GetPlatformAPIKeyByHashRow, error) {
	return f.platformKey, nil
}

func (f fakeAuthQueries) GetTenantByID(context.Context, string) (GetTenantByIDRow, error) {
	return f.tenant, nil
}

func (f fakeAuthQueries) ListActiveProviderCredentials(context.Context) ([]ListActiveProviderCredentialsRow, error) {
	return f.providerCredentials, nil
}
