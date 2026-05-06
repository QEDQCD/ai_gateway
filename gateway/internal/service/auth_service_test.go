package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/example/ai_gateway/gateway/internal/store"
)

func TestResolveConsolePrincipalAllowsAdminAndMemberScopes(t *testing.T) {
	t.Parallel()

	authService := newSeededAuthService(t)

	admin, err := authService.ResolveConsolePrincipal(context.Background(), "admin@example.com")
	if err != nil || admin.Role != "admin" {
		t.Fatalf("expected admin principal, got %#v err=%v", admin, err)
	}

	member, err := authService.ResolveConsolePrincipal(context.Background(), "member-a@example.com")
	if err != nil || member.Role != "member" || member.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant-scoped member, got %#v err=%v", member, err)
	}
}

func TestResolveConsolePrincipalRejectsAmbiguousMemberTenantScope(t *testing.T) {
	t.Parallel()

	repo := &fakeAuthRepository{consolePrincipalErr: store.ErrAuthScopeAmbiguous}
	authService := service.NewAuthService(repo, &fakeQuotaGuard{}, service.NewRouteService(repo))

	_, err := authService.ResolveConsolePrincipal(context.Background(), "member@example.com")
	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("expected error %v, got %v", service.ErrUnauthorized, err)
	}
}

func TestAuthenticateConsoleSessionReturnsSignedLoginResult(t *testing.T) {
	t.Parallel()

	repo := &fakeAuthRepository{
		authenticatedPrincipal: store.ConsolePrincipalRecord{
			UserID:   "user_member_a",
			Email:    "member-a@example.com",
			Name:     "租户用户 A",
			Role:     "member",
			TenantID: "tenant_demo",
		},
		consolePrincipal: store.ConsolePrincipalRecord{
			UserID:   "user_member_a",
			Email:    "member-a@example.com",
			Name:     "租户用户 A",
			Role:     "member",
			TenantID: "tenant_demo",
		},
	}
	authService := service.NewAuthServiceWithConsoleSessions(
		repo,
		&fakeQuotaGuard{},
		service.NewRouteService(repo),
		"console-session-secret",
	)

	result, err := authService.AuthenticateConsoleSession(context.Background(), "member-a@example.com", "secret")
	if err != nil {
		t.Fatalf("AuthenticateConsoleSession failed: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected signed console session token")
	}
	if result.Email != "member-a@example.com" || result.Role != "member" || result.TenantID != "tenant_demo" {
		t.Fatalf("unexpected login result: %#v", result)
	}

	principal, err := authService.ResolveConsoleSession(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("ResolveConsoleSession failed: %v", err)
	}
	if principal.UserID != "user_member_a" || principal.Role != "member" {
		t.Fatalf("unexpected resolved principal: %#v", principal)
	}
}

func TestResolveConsoleSessionRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	repo := &fakeAuthRepository{}
	authService := service.NewAuthServiceWithConsoleSessions(
		repo,
		&fakeQuotaGuard{},
		service.NewRouteService(repo),
		"console-session-secret",
	)

	_, err := authService.ResolveConsoleSession(context.Background(), "invalid.token.payload")
	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("expected error %v, got %v", service.ErrUnauthorized, err)
	}
}

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
			requestedModel: "gpt-4o-mini",
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
					SupportedModels: []string{
						"claude-3-5-sonnet",
					},
				},
				{
					ID:          "pc_456",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"gpt-4o-mini",
					},
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_123",
				PlatformAPIKeyName:   "demo key",
				SelectedProviderID:   "pc_456",
				SelectedProviderName: "OpenAI Primary",
				RouteID:              "route:pc_456:default",
			},
			wantCredentialLookups: 1,
		},
		{
			name:           "provider alias still resolves when supported models are unavailable",
			requestedModel: "openai",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_compat",
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
				PlatformAPIKeyID:     "pak_compat",
				PlatformAPIKeyName:   "demo key",
				SelectedProviderID:   "pc_456",
				SelectedProviderName: "OpenAI Primary",
				RouteID:              "route:pc_456:default",
			},
			wantCredentialLookups: 1,
		},
		{
			name:           "requested model prefers supported model match over provider alias compatibility",
			requestedModel: "gpt-4o-mini",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_prefer_model",
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
					ID:          "pc_provider_alias",
					Provider:    "gpt-4o-mini",
					DisplayName: "Compatibility Alias",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"claude-3-5-sonnet",
					},
				},
				{
					ID:          "pc_model_match",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"gpt-4o-mini",
					},
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_prefer_model",
				PlatformAPIKeyName:   "demo key",
				SelectedProviderID:   "pc_model_match",
				SelectedProviderName: "OpenAI Primary",
				RouteID:              "route:pc_model_match:default",
			},
			wantCredentialLookups: 1,
		},
		{
			name:           "display name compatibility resolves when model match is unavailable",
			requestedModel: "OpenAI Primary",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_display_name",
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
					ID:          "pc_display_name",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_display_name",
				PlatformAPIKeyName:   "demo key",
				SelectedProviderID:   "pc_display_name",
				SelectedProviderName: "OpenAI Primary",
				RouteID:              "route:pc_display_name:default",
			},
			wantCredentialLookups: 1,
		},
		{
			name:           "missing requested model falls back to first active credential",
			requestedModel: "",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_first_active",
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
					ID:          "pc_first",
					Provider:    "anthropic",
					DisplayName: "Anthropic Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"claude-3-5-sonnet",
					},
				},
				{
					ID:          "pc_second",
					Provider:    "openai",
					DisplayName: "OpenAI Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"gpt-4o-mini",
					},
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_first_active",
				PlatformAPIKeyName:   "demo key",
				SelectedProviderID:   "pc_first",
				SelectedProviderName: "Anthropic Primary",
				RouteID:              "route:pc_first:default",
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
			name: "expired platform key returns unauthorized",
			platformKey: store.PlatformAPIKeyRecord{
				ID:        "pak_expired",
				TenantID:  "tenant_123",
				Name:      "expired key",
				Status:    domain.StatusActive,
				ExpiresAt: time.Now().Add(-time.Hour),
			},
			tenant: store.TenantRecord{
				ID:     "tenant_123",
				Name:   "demo tenant",
				Status: domain.StatusActive,
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_expired",
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
			requestedModel: "gpt-4o-mini",
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
					SupportedModels: []string{
						"claude-3-5-sonnet",
					},
				},
			},
			wantErr:               service.ErrRouteNotFound,
			wantCredentialLookups: 1,
		},
		{
			name:           "tenant model scope rejects disallowed model",
			requestedModel: "mimo-v2.5-pro",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_model_scope",
				TenantID: "tenant_123",
				Name:     "scoped key",
				Status:   domain.StatusActive,
			},
			tenant: store.TenantRecord{
				ID:            "tenant_123",
				Name:          "demo tenant",
				Status:        domain.StatusActive,
				AllowedModels: []string{"qwen-flash"},
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_qwen",
					Provider:    "dashscope",
					DisplayName: "Qwen Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"qwen-flash",
					},
				},
				{
					ID:          "pc_mimo",
					Provider:    "mimo",
					DisplayName: "MIMO Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"mimo-v2.5-pro",
					},
				},
			},
			wantErr:               service.ErrModelNotAllowed,
			wantCredentialLookups: 1,
		},
		{
			name:           "tenant model scope allows configured model",
			requestedModel: "qwen-flash",
			platformKey: store.PlatformAPIKeyRecord{
				ID:       "pak_model_scope_allowed",
				TenantID: "tenant_123",
				Name:     "scoped key",
				Status:   domain.StatusActive,
			},
			tenant: store.TenantRecord{
				ID:            "tenant_123",
				Name:          "demo tenant",
				Status:        domain.StatusActive,
				AllowedModels: []string{"qwen-flash"},
			},
			providerCredentials: []store.ProviderCredentialRecord{
				{
					ID:          "pc_qwen",
					Provider:    "dashscope",
					DisplayName: "Qwen Primary",
					Status:      domain.StatusActive,
					SupportedModels: []string{
						"qwen-flash",
					},
				},
			},
			wantContext: domain.RequestContext{
				TenantID:             "tenant_123",
				PlatformAPIKeyID:     "pak_model_scope_allowed",
				PlatformAPIKeyName:   "scoped key",
				SelectedProviderID:   "pc_qwen",
				SelectedProviderName: "Qwen Primary",
				RouteID:              "route:pc_qwen:default",
			},
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
			platformKeyUsable := tc.platformKey.Status == domain.StatusActive &&
				(tc.platformKey.ExpiresAt.IsZero() || tc.platformKey.ExpiresAt.After(time.Now()))
			if platformKeyUsable && repo.tenantCtx != requestContext {
				t.Fatal("expected tenant lookup to use the request context")
			}
			if repo.listProviderCredentialCalls != tc.wantCredentialLookups {
				t.Fatalf("expected %d provider credential lookups, got %d", tc.wantCredentialLookups, repo.listProviderCredentialCalls)
			}
			if platformKeyUsable && tc.tenant.Status == domain.StatusActive {
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
			if tc.wantCredentialLookups > 0 && routeService.model != tc.requestedModel {
				t.Fatalf("expected requested model %q to be forwarded to route service, got %q", tc.requestedModel, routeService.model)
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
			if gotContext.PlatformAPIKeyName == "" {
				t.Fatal("expected request context PlatformAPIKeyName to be populated")
			}
			if gotContext.RouteID == "" {
				t.Fatal("expected request context RouteID to be populated")
			}
			if gotContext.RouteID == gotContext.SelectedProviderID {
				t.Fatalf("expected route id to differ from provider credential id, got %q", gotContext.RouteID)
			}
			if gotContext != tc.wantContext {
				t.Fatalf("expected context %+v, got %+v", tc.wantContext, gotContext)
			}
		})
	}
}

func TestResolveRejectsExpiredPlatformAPIKey(t *testing.T) {
	t.Parallel()

	repo := &fakeAuthRepository{
		platformKey: store.PlatformAPIKeyRecord{
			ID:        "pak_expired",
			TenantID:  "tenant_123",
			Name:      "expired key",
			Status:    domain.StatusActive,
			ExpiresAt: time.Now().Add(-time.Hour),
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
	}
	quotaGuard := &fakeQuotaGuard{}
	authService := service.NewAuthService(repo, quotaGuard, service.NewRouteService(repo))

	_, err := authService.Resolve(context.Background(), "platform-live-key", "gpt-4o-mini")
	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("expected error %v, got %v", service.ErrUnauthorized, err)
	}
	if quotaGuard.tenantID != "" {
		t.Fatalf("expected quota guard to be skipped for expired key, got tenant %q", quotaGuard.tenantID)
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
	consolePrincipal            store.ConsolePrincipalRecord
	authenticatedPrincipal      store.ConsolePrincipalRecord
	consolePrincipalErr         error
	gotKeyHash                  string
	platformKeyCtx              context.Context
	tenantCtx                   context.Context
	listProviderCredentialCalls int
	providerCredentialsCtx      context.Context
	consolePrincipalCtx         context.Context
	consolePrincipalSubject     string
	authenticatedPrincipalCtx   context.Context
	authenticatedPrincipalEmail string
	authenticatedPassword       string
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

func (f *fakeAuthRepository) ResolveConsolePrincipal(ctx context.Context, subject string) (store.ConsolePrincipalRecord, error) {
	f.consolePrincipalCtx = ctx
	f.consolePrincipalSubject = subject
	if f.consolePrincipalErr != nil {
		return store.ConsolePrincipalRecord{}, f.consolePrincipalErr
	}
	return f.consolePrincipal, nil
}

func (f *fakeAuthRepository) AuthenticateConsoleUser(ctx context.Context, subject string, password string) (store.ConsolePrincipalRecord, error) {
	f.authenticatedPrincipalCtx = ctx
	f.authenticatedPrincipalEmail = subject
	f.authenticatedPassword = password
	if f.consolePrincipalErr != nil {
		return store.ConsolePrincipalRecord{}, f.consolePrincipalErr
	}
	return f.authenticatedPrincipal, nil
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
	model    string
}

func (s *capturingRouteService) Resolve(ctx context.Context, requestedModel string) (domain.ProviderRoute, error) {
	s.ctx = ctx
	s.model = requestedModel
	return s.delegate.Resolve(ctx, requestedModel)
}

type testContextKey struct{}

func hashPlatformAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newSeededAuthService(t *testing.T) service.ConsoleAuthService {
	t.Helper()

	repo := store.NewBootstrapAuthRepository(store.BootstrapAuthConfig{
		RawPlatformAPIKey:    "agw_demo_key",
		PlatformAPIKeyID:     "pak_demo",
		PlatformAPIKeyUserID: "user_demo",
		PlatformAPIKeyName:   "demo",
		TenantID:             "tenant_demo",
		TenantName:           "Demo Tenant",
		ConsolePrincipals: []store.ConsolePrincipalRecord{
			{
				UserID: "user_admin",
				Email:  "admin@example.com",
				Role:   "admin",
				Name:   "平台管理员",
			},
			{
				UserID:   "user_member_a",
				Email:    "member-a@example.com",
				Role:     "member",
				TenantID: "tenant_demo",
				Name:     "租户用户 A",
			},
		},
	})

	return service.NewAuthService(repo, &fakeQuotaGuard{}, service.NewRouteService(repo))
}
