package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
)

var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrQuotaExceeded = errors.New("quota exceeded")
)

type AuthService interface {
	Resolve(rawKey string, requestedModel string) (domain.RequestContext, error)
}

type QuotaGuard interface {
	CheckTenantQuota(tenantID string) error
}

type authService struct {
	repository   store.AuthRepository
	quotaGuard   QuotaGuard
	routeService RouteService
}

func NewAuthService(repository store.AuthRepository, quotaGuard QuotaGuard, routeService RouteService) AuthService {
	if quotaGuard == nil {
		quotaGuard = noopQuotaGuard{}
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

func (s authService) Resolve(rawKey string, requestedModel string) (domain.RequestContext, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return domain.RequestContext{}, fmt.Errorf("%w: platform API key is required", ErrUnauthorized)
	}

	platformKey, err := s.repository.FindPlatformAPIKeyByHash(context.Background(), hashPlatformAPIKey(rawKey))
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) {
			return domain.RequestContext{}, fmt.Errorf("%w: platform API key not found", ErrUnauthorized)
		}
		return domain.RequestContext{}, err
	}
	if platformKey.Status != domain.StatusActive {
		return domain.RequestContext{}, fmt.Errorf("%w: platform API key is %s", ErrUnauthorized, platformKey.Status)
	}

	tenant, err := s.repository.FindTenantByID(context.Background(), platformKey.TenantID)
	if err != nil {
		if errors.Is(err, store.ErrAuthRecordNotFound) {
			return domain.RequestContext{}, fmt.Errorf("%w: tenant not found", ErrUnauthorized)
		}
		return domain.RequestContext{}, err
	}
	if tenant.Status != domain.StatusActive {
		return domain.RequestContext{}, fmt.Errorf("%w: tenant is %s", ErrUnauthorized, tenant.Status)
	}

	if err := s.quotaGuard.CheckTenantQuota(tenant.ID); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			return domain.RequestContext{}, err
		}
		return domain.RequestContext{}, fmt.Errorf("%w: %v", ErrQuotaExceeded, err)
	}

	route, err := s.routeService.Resolve(requestedModel)
	if err != nil {
		return domain.RequestContext{}, err
	}

	return domain.RequestContext{
		TenantID:             tenant.ID,
		PlatformAPIKeyID:     platformKey.ID,
		SelectedProviderID:   route.ProviderID,
		SelectedProviderName: route.ProviderName,
	}, nil
}

type noopQuotaGuard struct{}

func (noopQuotaGuard) CheckTenantQuota(string) error {
	return nil
}

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}
