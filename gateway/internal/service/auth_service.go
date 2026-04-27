package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/store"
)

var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrQuotaExceeded = errors.New("quota exceeded")
)

type AuthService interface {
	Resolve(ctx context.Context, rawKey string, requestedModel string) (domain.RequestContext, error)
}

type ConsoleAuthService interface {
	AuthService
	ResolvePlatformAPIKey(ctx context.Context, rawKey string) (domain.RequestContext, error)
	ResolveConsolePrincipal(ctx context.Context, subject string) (ConsolePrincipal, error)
}

type ConsolePrincipal struct {
	UserID   string
	Email    string
	Role     string
	TenantID string
}

type QuotaGuard interface {
	CheckTenantQuota(ctx context.Context, tenantID string) error
}

type RedisQuotaClient interface {
	Exists(ctx context.Context, key string) (bool, error)
}

type RedisQuotaGuard struct {
	client    RedisQuotaClient
	keyPrefix string
}

type unauthorizedAuthService struct{}

type authService struct {
	repository   store.AuthRepository
	quotaGuard   QuotaGuard
	routeService RouteService
}

type consolePrincipalRepository interface {
	ResolveConsolePrincipal(ctx context.Context, subject string) (store.ConsolePrincipalRecord, error)
}

const redisQuotaExhaustedKeyPrefix = "tenant_quota_exhausted:"

func NewAuthService(repository store.AuthRepository, quotaGuard QuotaGuard, routeService RouteService) ConsoleAuthService {
	if quotaGuard == nil {
		panic("service: quota guard is required")
	}
	if routeService == nil {
		routeService = NewRouteService(repository)
	}

	return authService{
		repository:   repository,
		quotaGuard:   quotaGuard,
		routeService: routeService,
	}
}

func NewRedisQuotaGuard(client RedisQuotaClient) RedisQuotaGuard {
	if client == nil {
		panic("service: redis quota client is required")
	}

	return RedisQuotaGuard{
		client:    client,
		keyPrefix: redisQuotaExhaustedKeyPrefix,
	}
}

func NewUnauthorizedAuthService() ConsoleAuthService {
	return unauthorizedAuthService{}
}

func (s authService) Resolve(ctx context.Context, rawKey string, requestedModel string) (domain.RequestContext, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return domain.RequestContext{}, fmt.Errorf("%w: platform API key is required", ErrUnauthorized)
	}

	platformKey, err := s.repository.FindPlatformAPIKeyByHash(ctx, hashPlatformAPIKey(rawKey))
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) {
			return domain.RequestContext{}, fmt.Errorf("%w: platform API key not found", ErrUnauthorized)
		}
		return domain.RequestContext{}, err
	}
	if platformKey.Status != domain.StatusActive {
		return domain.RequestContext{}, fmt.Errorf("%w: platform API key is %s", ErrUnauthorized, platformKey.Status)
	}

	tenant, err := s.repository.FindTenantByID(ctx, platformKey.TenantID)
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) {
			return domain.RequestContext{}, fmt.Errorf("%w: tenant not found", ErrUnauthorized)
		}
		return domain.RequestContext{}, err
	}
	if tenant.Status != domain.StatusActive {
		return domain.RequestContext{}, fmt.Errorf("%w: tenant is %s", ErrUnauthorized, tenant.Status)
	}

	if err := s.quotaGuard.CheckTenantQuota(ctx, tenant.ID); err != nil {
		return domain.RequestContext{}, err
	}

	route, err := s.routeService.Resolve(ctx, requestedModel)
	if err != nil {
		return domain.RequestContext{}, err
	}

	return domain.RequestContext{
		TenantID:             tenant.ID,
		UserID:               platformKey.UserID,
		PlatformAPIKeyID:     platformKey.ID,
		PlatformAPIKeyName:   platformKey.Name,
		SelectedProviderID:   route.ProviderID,
		SelectedProviderName: route.ProviderName,
		RouteID:              route.RouteID,
		ProviderTarget:       route.Target,
	}, nil
}

func (s authService) ResolvePlatformAPIKey(ctx context.Context, rawKey string) (domain.RequestContext, error) {
	return s.Resolve(ctx, rawKey, "")
}

func (s authService) ResolveConsolePrincipal(ctx context.Context, subject string) (ConsolePrincipal, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ConsolePrincipal{}, fmt.Errorf("%w: console subject is required", ErrUnauthorized)
	}

	repository, ok := s.repository.(consolePrincipalRepository)
	if !ok {
		return ConsolePrincipal{}, fmt.Errorf("%w: console principal resolution unavailable", ErrUnauthorized)
	}

	record, err := repository.ResolveConsolePrincipal(ctx, subject)
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) {
			return ConsolePrincipal{}, fmt.Errorf("%w: console principal not found", ErrUnauthorized)
		}
		if errors.Is(err, store.ErrAuthScopeAmbiguous) {
			return ConsolePrincipal{}, fmt.Errorf("%w: console principal scope is ambiguous", ErrUnauthorized)
		}
		return ConsolePrincipal{}, err
	}

	switch record.Role {
	case domain.ConsoleRoleAdmin:
	case domain.ConsoleRoleMember:
		if record.TenantID == "" {
			return ConsolePrincipal{}, fmt.Errorf("%w: tenant-scoped membership is required", ErrUnauthorized)
		}
	default:
		return ConsolePrincipal{}, fmt.Errorf("%w: console role %q is invalid", ErrUnauthorized, record.Role)
	}

	return ConsolePrincipal{
		UserID:   record.UserID,
		Email:    record.Email,
		Role:     record.Role,
		TenantID: record.TenantID,
	}, nil
}

func (g RedisQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	exhausted, err := g.client.Exists(ctx, g.keyPrefix+tenantID)
	if err != nil {
		return err
	}
	if exhausted {
		return ErrQuotaExceeded
	}
	return nil
}

func (unauthorizedAuthService) Resolve(context.Context, string, string) (domain.RequestContext, error) {
	return domain.RequestContext{}, fmt.Errorf("%w: auth service not configured", ErrUnauthorized)
}

func (unauthorizedAuthService) ResolvePlatformAPIKey(context.Context, string) (domain.RequestContext, error) {
	return domain.RequestContext{}, fmt.Errorf("%w: auth service not configured", ErrUnauthorized)
}

func (unauthorizedAuthService) ResolveConsolePrincipal(context.Context, string) (ConsolePrincipal, error) {
	return ConsolePrincipal{}, fmt.Errorf("%w: auth service not configured", ErrUnauthorized)
}

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}
