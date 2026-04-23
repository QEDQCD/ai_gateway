package store

import (
	"context"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
)

func TestSQLAuthRepositoryListActiveProviderCredentialsMapsSupportedModels(t *testing.T) {
	t.Parallel()

	queries := fakeAuthQueries{
		providerCredentials: []ListActiveProviderCredentialsRow{
			{
				ID:              "pc_123",
				Provider:        "openai",
				DisplayName:     "OpenAI Primary",
				SupportedModels: []string{"gpt-4o-mini", "text-embedding-3-small"},
				Status:          string(domain.StatusActive),
			},
		},
	}

	repo := NewAuthRepository(queries)

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
	if got.Provider != "openai" {
		t.Fatalf("expected provider %q, got %q", "openai", got.Provider)
	}
	if got.DisplayName != "OpenAI Primary" {
		t.Fatalf("expected display name %q, got %q", "OpenAI Primary", got.DisplayName)
	}
	if got.Status != domain.StatusActive {
		t.Fatalf("expected status %q, got %q", domain.StatusActive, got.Status)
	}
	if len(got.SupportedModels) != 2 {
		t.Fatalf("expected 2 supported models, got %d", len(got.SupportedModels))
	}
	if got.SupportedModels[0] != "gpt-4o-mini" || got.SupportedModels[1] != "text-embedding-3-small" {
		t.Fatalf("expected supported models to round-trip, got %#v", got.SupportedModels)
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
