package service

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/store"
)

var ErrRouteNotFound = errors.New("no active provider credential available")

const defaultRouteKey = "default"

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
				return routeFromCredential(credential, supportedModel), nil
			}
		}
	}

	for _, credential := range credentials {
		if strings.EqualFold(credential.Provider, requestedModel) {
			return routeFromCredential(credential, ""), nil
		}
	}

	for _, credential := range credentials {
		if strings.EqualFold(credential.DisplayName, requestedModel) {
			return routeFromCredential(credential, ""), nil
		}
	}

	return domain.ProviderRoute{}, ErrRouteNotFound
}

func firstCredentialRoute(credentials []store.ProviderCredentialRecord) domain.ProviderRoute {
	return routeFromCredential(credentials[0], "")
}

func routeFromCredential(credential store.ProviderCredentialRecord, requestedModel string) domain.ProviderRoute {
	return domain.ProviderRoute{
		RouteID:      RouteIDForCredential(credential.ID, credential.SupportedModels, requestedModel),
		ProviderID:   credential.ID,
		ProviderName: credential.DisplayName,
		Model:        routeModelForCredential(credential.SupportedModels, requestedModel),
		Target:       providerTargetFromCredential(credential),
	}
}

func routeModelForCredential(supportedModels []string, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	for _, supportedModel := range supportedModels {
		supportedModel = strings.TrimSpace(supportedModel)
		if supportedModel == "" {
			continue
		}
		if requestedModel == "" || strings.EqualFold(supportedModel, requestedModel) {
			return supportedModel
		}
	}
	return requestedModel
}

func RouteIDForCredential(providerCredentialID string, supportedModels []string, requestedModel string) string {
	return "route:" + providerCredentialID + ":" + routeKeyForCredential(supportedModels, requestedModel)
}

func routeKeyForCredential(supportedModels []string, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return defaultRouteKey
	}

	firstSupportedModel := ""
	for index, supportedModel := range supportedModels {
		supportedModel = strings.TrimSpace(supportedModel)
		if supportedModel == "" {
			continue
		}
		if firstSupportedModel == "" {
			firstSupportedModel = supportedModel
		}
		if strings.EqualFold(supportedModel, requestedModel) {
			if index == 0 {
				return defaultRouteKey
			}
			if routeKey := slugRouteKey(supportedModel); routeKey != "" {
				return routeKey
			}
			return defaultRouteKey
		}
	}

	if firstSupportedModel != "" {
		return defaultRouteKey
	}

	if routeKey := slugRouteKey(requestedModel); routeKey != "" {
		return routeKey
	}
	return defaultRouteKey
}

func slugRouteKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return ""
	}
	return slug
}

func providerTargetFromCredential(credential store.ProviderCredentialRecord) domain.ProviderTarget {
	if credential.BaseURL == "" && credential.APIKey == "" {
		return domain.ProviderTarget{}
	}

	return domain.ProviderTarget{
		CredentialID: credential.ID,
		Provider:     credential.Provider,
		BaseURL:      credential.BaseURL,
		APIKey:       credential.APIKey,
	}
}
