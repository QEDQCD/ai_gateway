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
	requestContext := context.WithValue(context.Background(), testContextKey{}, "request-123")

	testCases := []struct {
		name                  string
		requestedModel        string
		platformKey           store.PlatformAPIKeyRecord
		tenant                store.TenantRecord
		providerCredentials   []store.ProviderCredentialRecord
		quotaErr              error
		wantContext           domain.RequestContext
		wantErr               error
		wantCredentialLookups int
	}{
		{
			name:           "active platform key and active provider credential resolves request context",
			requestedModel: "openai",
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
					ID:          "pc_123",
					Provider:    "anthropic",
					DisplayName: "Anthropic Primary",
					Status:      domain.StatusActive,
				},
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
			name:           "requested model without matching active provider returns route not found",
			requestedModel: "openai",
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
					ID:          "pc_123",
					Provider:    "anthropic",
					DisplayName: "Anthropic Primary",
					Status:      domain.StatusActive,
				},
			},
			wantErr:               service.ErrRouteNotFound,
			wantCredentialLookups: 1,
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
			quotaGuard := &fakeQuotaGuard{err: tc.quotaErr}
			routeService := &capturingRouteService{
				delegate: service.NewRouteService(repo),
			}

			authService := service.NewAuthService(repo, quotaGuard, routeService)
			gotContext, err := authService.Resolve(requestContext, rawKey, tc.requestedModel)

			if repo.gotKeyHash != expectedKeyHash {
				t.Fatalf("expected hashed key %q, got %q", expectedKeyHash, repo.gotKeyHash)
			}
			if repo.platformKeyCtx != requestContext {
				t.Fatal("expected platform key lookup to use the request context")
			}
			if tc.platformKey.Status == domain.StatusActive && repo.tenantCtx != requestContext {
				t.Fatal("expected tenant lookup to use the request context")
			}
			if repo.listProviderCredentialCalls != tc.wantCredentialLookups {
				t.Fatalf("expected %d provider credential lookups, got %d", tc.wantCredentialLookups, repo.listProviderCredentialCalls)
			}
			if tc.platformKey.Status == domain.StatusActive && tc.tenant.Status == domain.StatusActive {
				if quotaGuard.ctx != requestContext {
					t.Fatal("expected quota check to use the request context")
				}
				if quotaGuard.tenantID != tc.tenant.ID {
					t.Fatalf("expected quota check tenant %q, got %q", tc.tenant.ID, quotaGuard.tenantID)
				}
			}
			if tc.wantCredentialLookups > 0 && routeService.ctx != requestContext {
				t.Fatal("expected route resolution to use the request context")
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

func TestNewAuthServiceRequiresQuotaGuard(t *testing.T) {
	t.Parallel()

	repo := &fakeAuthRepository{}

	defer func() {
		if recover() == nil {
			t.Fatal("expected NewAuthService to panic when quota guard is nil")
		}
	}()

	service.NewAuthService(repo, nil, service.NewRouteService(repo))
}

func TestRedisQuotaGuardCheckTenantQuota(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		exhausted bool
		wantErr   error
	}{
		{
			name: "available quota passes",
		},
		{
			name:      "exhausted quota returns quota exceeded",
			exhausted: true,
			wantErr:   service.ErrQuotaExceeded,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeRedisQuotaClient{exhausted: tc.exhausted}
			guard := service.NewRedisQuotaGuard(client)
			requestContext := context.WithValue(context.Background(), testContextKey{}, tc.name)

			err := guard.CheckTenantQuota(requestContext, "tenant_123")

			if client.gotKey != "tenant_quota_exhausted:tenant_123" {
				t.Fatalf("expected redis key %q, got %q", "tenant_quota_exhausted:tenant_123", client.gotKey)
			}
			if client.ctx != requestContext {
				t.Fatal("expected redis quota client to use the request context")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestUnauthorizedAuthServiceResolveReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	authService := service.NewUnauthorizedAuthService()

	_, err := authService.Resolve(context.Background(), "platform-live-key", "openai")

	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("expected error %v, got %v", service.ErrUnauthorized, err)
	}
}

type fakeAuthRepository struct {
	platformKey                 store.PlatformAPIKeyRecord
	tenant                      store.TenantRecord
	providerCredentials         []store.ProviderCredentialRecord
	gotKeyHash                  string
	platformKeyCtx              context.Context
	tenantCtx                   context.Context
	listProviderCredentialCalls int
	providerCredentialsCtx      context.Context
}

func (f *fakeAuthRepository) FindPlatformAPIKeyByHash(ctx context.Context, keyHash string) (store.PlatformAPIKeyRecord, error) {
	f.platformKeyCtx = ctx
	f.gotKeyHash = keyHash
	return f.platformKey, nil
}

func (f *fakeAuthRepository) FindTenantByID(ctx context.Context, _ string) (store.TenantRecord, error) {
	f.tenantCtx = ctx
	return f.tenant, nil
}

func (f *fakeAuthRepository) ListActiveProviderCredentials(ctx context.Context) ([]store.ProviderCredentialRecord, error) {
	f.listProviderCredentialCalls++
	f.providerCredentialsCtx = ctx
	return f.providerCredentials, nil
}

type fakeQuotaGuard struct {
	err      error
	ctx      context.Context
	tenantID string
}

func (f *fakeQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	f.ctx = ctx
	f.tenantID = tenantID
	return f.err
}

type fakeRedisQuotaClient struct {
	exhausted bool
	gotKey    string
	ctx       context.Context
}

func (f *fakeRedisQuotaClient) Exists(ctx context.Context, key string) (bool, error) {
	f.ctx = ctx
	f.gotKey = key
	return f.exhausted, nil
}

type capturingRouteService struct {
	delegate service.RouteService
	ctx      context.Context
}

func (s *capturingRouteService) Resolve(ctx context.Context, requestedModel string) (domain.ProviderRoute, error) {
	s.ctx = ctx
	return s.delegate.Resolve(ctx, requestedModel)
}

type testContextKey struct{}

func hashPlatformAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
