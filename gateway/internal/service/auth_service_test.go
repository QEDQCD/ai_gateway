package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
)

func TestResolveRequestContextUsesPlatformKeyAndProviderCredential(t *testing.T) {
	t.Parallel()

	rawKey := "platform-live-key"
	expectedKeyHash := hashPlatformAPIKey(rawKey)

	testCases := []struct {
		name                  string
		platformKey           store.PlatformAPIKeyRecord
		tenant                store.TenantRecord
		providerCredentials   []store.ProviderCredentialRecord
		quotaErr              error
		wantContext           domain.RequestContext
		wantErr               error
		wantCredentialLookups int
	}{
		{
			name: "active platform key and active provider credential resolves request context",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_123",
				TenantID: "tenant_123",
				Name:     "demo key",
				Status:   domain.StatusActive,
			},
			tenant: store.TenantRecord{
				ID:     "tenant_123",
				Name:   "demo tenant",
				Status: domain.StatusActive,
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_456",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_123",
				SelectedProviderID:   "pc_456",
				SelectedProviderName: "OpenAI Primary",
			},
			wantCredentialLookups: 1,
		},
		{
			name: "disabled platform key returns unauthorized",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_disabled",
				TenantID: "tenant_123",
				Name:     "disabled key",
				Status:   domain.StatusDisabled,
			},
			tenant: store.TenantRecord{
				ID:     "tenant_123",
				Name:   "demo tenant",
				Status: domain.StatusActive,
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_456",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
				},
			},
			wantErr:               service.ErrUnauthorized,
			wantCredentialLookups: 0,
		},
		{
			name: "quota exhausted returns quota exceeded",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_123",
				TenantID: "tenant_123",
				Name:     "demo key",
				Status:   domain.StatusActive,
			},
			tenant: store.TenantRecord{
				ID:     "tenant_123",
				Name:   "demo tenant",
				Status: domain.StatusActive,
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_456",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
				},
			},
			quotaErr:              service.ErrQuotaExceeded,
			wantErr:               service.ErrQuotaExceeded,
			wantCredentialLookups: 0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeAuthRepository{
				platformKey:         tc.platformKey,
				tenant:              tc.tenant,
				providerCredentials: tc.providerCredentials,
			}
			quotaGuard := fakeQuotaGuard{err: tc.quotaErr}

			authService := service.NewAuthService(repo, quotaGuard, service.NewRouteService(repo))
			gotContext, err := authService.Resolve(rawKey, "gpt-4o-mini")

			if repo.gotKeyHash != expectedKeyHash {
				t.Fatalf("expected hashed key %q, got %q", expectedKeyHash, repo.gotKeyHash)
			}
			if repo.listProviderCredentialCalls != tc.wantCredentialLookups {
				t.Fatalf("expected %d provider credential lookups, got %d", tc.wantCredentialLookups, repo.listProviderCredentialCalls)
			}

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve returned unexpected error: %v", err)
			}
			if gotContext != tc.wantContext {
				t.Fatalf("expected context %+v, got %+v", tc.wantContext, gotContext)
			}
		})
	}
}

type fakeAuthRepository struct {
	platformKey                 store.PlatformAPIKeyRecord
	tenant                      store.TenantRecord
	providerCredentials         []store.ProviderCredentialRecord
	gotKeyHash                  string
	listProviderCredentialCalls int
}

func (f *fakeAuthRepository) FindPlatformAPIKeyByHash(_ context.Context, keyHash string) (store.PlatformAPIKeyRecord, error) {
	f.gotKeyHash = keyHash
	return f.platformKey, nil
}

func (f *fakeAuthRepository) FindTenantByID(_ context.Context, _ string) (store.TenantRecord, error) {
	return f.tenant, nil
}

func (f *fakeAuthRepository) ListActiveProviderCredentials(_ context.Context) ([]store.ProviderCredentialRecord, error) {
	f.listProviderCredentialCalls++
	return f.providerCredentials, nil
}

type fakeQuotaGuard struct {
	err error
}

func (f fakeQuotaGuard) CheckTenantQuota(_ string) error {
	return f.err
}

func hashPlatformAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
