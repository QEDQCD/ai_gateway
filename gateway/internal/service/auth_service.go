package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

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
	AuthenticateConsoleSession(ctx context.Context, subject string, password string) (ConsoleLoginResult, error)
	ResolveConsoleSession(ctx context.Context, token string) (ConsolePrincipal, error)
}

type ConsolePrincipal struct {
	UserID   string
	Email    string
	Name     string
	Role     string
	TenantID string
}

type ConsoleLoginResult struct {
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	TenantID  string `json:"tenant_id,omitempty"`
	ExpiresAt string `json:"expires_at"`
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
	sessionCodec *consoleSessionCodec
}

type consolePrincipalRepository interface {
	ResolveConsolePrincipal(ctx context.Context, subject string) (store.ConsolePrincipalRecord, error)
}

type consoleUserAuthenticator interface {
	AuthenticateConsoleUser(ctx context.Context, subject string, password string) (store.ConsolePrincipalRecord, error)
}

const redisQuotaExhaustedKeyPrefix = "tenant_quota_exhausted:"

func NewAuthService(repository store.AuthRepository, quotaGuard QuotaGuard, routeService RouteService) ConsoleAuthService {
	return NewAuthServiceWithConsoleSessions(repository, quotaGuard, routeService, "")
}

func NewAuthServiceWithConsoleSessions(
	repository store.AuthRepository,
	quotaGuard QuotaGuard,
	routeService RouteService,
	consoleSessionSecret string,
) ConsoleAuthService {
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
		sessionCodec: newConsoleSessionCodec(consoleSessionSecret),
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
		Name:     record.Name,
		Role:     record.Role,
		TenantID: record.TenantID,
	}, nil
}

func (s authService) AuthenticateConsoleSession(ctx context.Context, subject string, password string) (ConsoleLoginResult, error) {
	repository, ok := s.repository.(consoleUserAuthenticator)
	if !ok {
		return ConsoleLoginResult{}, fmt.Errorf("%w: console login unavailable", ErrUnauthorized)
	}
	if s.sessionCodec == nil || !s.sessionCodec.Enabled() {
		return ConsoleLoginResult{}, fmt.Errorf("%w: console session signing unavailable", ErrUnauthorized)
	}

	record, err := repository.AuthenticateConsoleUser(ctx, subject, password)
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) || errors.Is(err, store.ErrAuthScopeAmbiguous) {
			return ConsoleLoginResult{}, fmt.Errorf("%w: invalid console credentials", ErrUnauthorized)
		}
		return ConsoleLoginResult{}, err
	}

	principal := ConsolePrincipal{
		UserID:   record.UserID,
		Email:    record.Email,
		Name:     record.Name,
		Role:     record.Role,
		TenantID: record.TenantID,
	}

	token, expiresAt, err := s.sessionCodec.Sign(principal.Email, time.Now())
	if err != nil {
		return ConsoleLoginResult{}, err
	}

	return ConsoleLoginResult{
		Token:     token,
		UserID:    principal.UserID,
		Email:     principal.Email,
		Name:      principal.Name,
		Role:      principal.Role,
		TenantID:  principal.TenantID,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

func (s authService) ResolveConsoleSession(ctx context.Context, token string) (ConsolePrincipal, error) {
	if s.sessionCodec == nil || !s.sessionCodec.Enabled() {
		return ConsolePrincipal{}, fmt.Errorf("%w: console session unavailable", ErrUnauthorized)
	}

	subject, err := s.sessionCodec.Verify(token, time.Now())
	if err != nil {
		return ConsolePrincipal{}, fmt.Errorf("%w: invalid console session", ErrUnauthorized)
	}

	return s.ResolveConsolePrincipal(ctx, subject)
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

func (unauthorizedAuthService) AuthenticateConsoleSession(context.Context, string, string) (ConsoleLoginResult, error) {
	return ConsoleLoginResult{}, fmt.Errorf("%w: auth service not configured", ErrUnauthorized)
}

func (unauthorizedAuthService) ResolveConsoleSession(context.Context, string) (ConsolePrincipal, error) {
	return ConsolePrincipal{}, fmt.Errorf("%w: auth service not configured", ErrUnauthorized)
}

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}
