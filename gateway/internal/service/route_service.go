package service

import (
	"context"
	"errors"
	"strings"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
)

var ErrRouteNotFound = errors.New("no active provider credential available")

type RouteService interface {
	Resolve(ctx context.Context, requestedModel string) (domain.ProviderRoute, error)
}

type routeService struct {
	repository store.AuthRepository
}

func NewRouteService(repository store.AuthRepository) RouteService {
	return routeService{repository: repository}
}

func (s routeService) Resolve(ctx context.Context, requestedModel string) (domain.ProviderRoute, error) {
	credentials, err := s.repository.ListActiveProviderCredentials(ctx)
	if err != nil {
		return domain.ProviderRoute{}, err
	}
	if len(credentials) == 0 {
		return domain.ProviderRoute{}, ErrRouteNotFound
	}

	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return firstCredentialRoute(credentials), nil
	}

	for _, credential := range credentials {
		for _, supportedModel := range credential.SupportedModels {
			if strings.EqualFold(strings.TrimSpace(supportedModel), requestedModel) {
				return domain.ProviderRoute{
					ProviderID:   credential.ID,
					ProviderName: credential.DisplayName,
				}, nil
			}
		}
	}

	for _, credential := range credentials {
		if strings.EqualFold(credential.Provider, requestedModel) {
			return domain.ProviderRoute{
				ProviderID:   credential.ID,
				ProviderName: credential.DisplayName,
			}, nil
		}
	}

	for _, credential := range credentials {
		if strings.EqualFold(credential.DisplayName, requestedModel) {
			return domain.ProviderRoute{
				ProviderID:   credential.ID,
				ProviderName: credential.DisplayName,
			}, nil
		}
	}

	return domain.ProviderRoute{}, ErrRouteNotFound
}

func firstCredentialRoute(credentials []store.ProviderCredentialRecord) domain.ProviderRoute {
	return domain.ProviderRoute{
		ProviderID:   credentials[0].ID,
		ProviderName: credentials[0].DisplayName,
	}
}
