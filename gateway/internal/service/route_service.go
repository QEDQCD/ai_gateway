package service

import (
	"context"
	"errors"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
)

var ErrRouteNotFound = errors.New("no active provider credential available")

type RouteService interface {
	Resolve(requestedModel string) (domain.ProviderRoute, error)
}

type routeService struct {
	repository store.AuthRepository
}

func NewRouteService(repository store.AuthRepository) RouteService {
	return routeService{repository: repository}
}

func (s routeService) Resolve(requestedModel string) (domain.ProviderRoute, error) {
	_ = requestedModel

	credentials, err := s.repository.ListActiveProviderCredentials(context.Background())
	if err != nil {
		return domain.ProviderRoute{}, err
	}
	if len(credentials) == 0 {
		return domain.ProviderRoute{}, ErrRouteNotFound
	}

	return domain.ProviderRoute{
		ProviderID:   credentials[0].ID,
		ProviderName: credentials[0].DisplayName,
	}, nil
}
