package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/config"
	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/google/uuid"
)

const modelHealthcheckChatEndpoint = "/v1/chat/completions"

var errModelHealthcheckSatisfied = errors.New("model healthcheck satisfied")

type ModelHealthcheckRoute struct {
	RouteID              string
	RequestedModel       string
	ProviderCredentialID string
	Provider             string
	BaseURL              string
	APIKey               string
	RequestMode          string
}

type ModelHealthcheckUpdate struct {
	LastHealthCheckedAt time.Time
	HealthStatus        string
	LastHealthError     string
	FirstTokenLatencyMS int64
	LatencyMS           int64
}

type ModelHealthcheckRunSummary struct {
	Checked   int
	Healthy   int
	Unhealthy int
}

type ModelHealthcheckCatalog interface {
	ListRunnableRoutes(ctx context.Context) ([]ModelHealthcheckRoute, error)
	UpdateRouteHealth(ctx context.Context, routeID string, update ModelHealthcheckUpdate) error
}

type ModelHealthcheckRunner struct {
	catalog ModelHealthcheckCatalog
	client  UpstreamChatClient
	cfg     config.Config
}

type postgresModelHealthcheckCatalog struct {
	db         store.DBTX
	repository store.AuthRepository
}

type providerCredentialResolver interface {
	ResolveProviderCredential(ctx context.Context, id string) (store.ProviderCredentialRecord, error)
}

func NewModelHealthcheckRunner(catalog ModelHealthcheckCatalog, client UpstreamChatClient, cfg config.Config) *ModelHealthcheckRunner {
	return &ModelHealthcheckRunner{
		catalog: catalog,
		client:  client,
		cfg:     cfg,
	}
}

func NewPostgresModelHealthcheckCatalog(db store.DBTX, repository store.AuthRepository) ModelHealthcheckCatalog {
	if db == nil || repository == nil {
		return nil
	}
	return postgresModelHealthcheckCatalog{
		db:         db,
		repository: repository,
	}
}

func (r *ModelHealthcheckRunner) Start(ctx context.Context) {
	if r == nil || !r.cfg.ModelHealthcheckEnabled {
		return
	}

	interval := r.cfg.ModelHealthcheckInterval
	if interval <= 0 {
		interval = time.Hour
	}

	run := func() {
		summary, err := r.RunOnce(ctx)
		if err != nil {
			log.Printf("model healthcheck runner: %v", err)
			return
		}
		log.Printf("model healthcheck runner: checked=%d healthy=%d unhealthy=%d", summary.Checked, summary.Healthy, summary.Unhealthy)
	}

	run()
	if ctx.Err() != nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (r *ModelHealthcheckRunner) RunOnce(ctx context.Context) (ModelHealthcheckRunSummary, error) {
	if r == nil || r.catalog == nil || r.client == nil {
		return ModelHealthcheckRunSummary{}, nil
	}

	routes, err := r.catalog.ListRunnableRoutes(ctx)
	if err != nil {
		return ModelHealthcheckRunSummary{}, err
	}

	summary := ModelHealthcheckRunSummary{}
	var firstErr error
	for _, route := range routes {
		summary.Checked++
		healthy, err := r.checkRoute(ctx, route)
		if healthy {
			summary.Healthy++
		} else {
			summary.Unhealthy++
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return summary, firstErr
}

func (r *ModelHealthcheckRunner) checkRoute(ctx context.Context, route ModelHealthcheckRoute) (bool, error) {
	startedAt := time.Now().UTC()
	update := ModelHealthcheckUpdate{
		LastHealthCheckedAt: startedAt,
		HealthStatus:        "degraded",
	}

	timeout := r.cfg.ModelHealthcheckTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	routeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stream, _, err := r.client.StreamComplete(routeCtx, domain.ProviderTarget{
		CredentialID: route.ProviderCredentialID,
		Provider:     strings.TrimSpace(route.Provider),
		BaseURL:      strings.TrimSpace(route.BaseURL),
		APIKey:       route.APIKey,
	}, ChatRequest{
		Model:     strings.TrimSpace(route.RequestedModel),
		Messages:  []ChatMessage{{Role: "user", Content: r.prompt()}},
		Stream:    true,
		MaxTokens: r.maxTokens(),
	})
	if err != nil {
		completedAt := time.Now().UTC()
		update.LastHealthCheckedAt = completedAt
		update.LatencyMS = durationMilliseconds(completedAt.Sub(startedAt))
		update.LastHealthError = strings.TrimSpace(err.Error())
		return false, r.catalog.UpdateRouteHealth(ctx, route.RouteID, update)
	}

	var firstTokenAt time.Time
	stopStream := false
	result, runErr := stream.Run(func([]byte) error {
		if stopStream {
			return errModelHealthcheckSatisfied
		}
		return nil
	}, func() {
		if firstTokenAt.IsZero() {
			firstTokenAt = time.Now().UTC()
		}
		stopStream = true
	})
	if errors.Is(runErr, errModelHealthcheckSatisfied) {
		runErr = nil
	}

	completedAt := time.Now().UTC()
	update.LastHealthCheckedAt = completedAt
	update.LatencyMS = durationMilliseconds(completedAt.Sub(startedAt))
	if (result.Response.Usage != nil && result.Response.Usage.CompletionTokens > 0 && firstTokenAt.IsZero()) {
		firstTokenAt = completedAt
	}
	if result.SawContentToken || !firstTokenAt.IsZero() {
		update.HealthStatus = "healthy"
		update.LastHealthError = ""
		if !firstTokenAt.IsZero() {
			update.FirstTokenLatencyMS = durationMilliseconds(firstTokenAt.Sub(startedAt))
		}
	} else {
		update.LastHealthError = modelHealthcheckError(runErr, routeCtx.Err())
	}

	return update.HealthStatus == "healthy", r.catalog.UpdateRouteHealth(ctx, route.RouteID, update)
}

func (r *ModelHealthcheckRunner) prompt() string {
	prompt := strings.TrimSpace(r.cfg.ModelHealthcheckPrompt)
	if prompt == "" {
		return "你好"
	}
	return prompt
}

func (r *ModelHealthcheckRunner) maxTokens() int {
	if r.cfg.ModelHealthcheckMaxTokens <= 0 {
		return 1
	}
	return r.cfg.ModelHealthcheckMaxTokens
}

func modelHealthcheckError(runErr error, ctxErr error) string {
	switch {
	case runErr != nil:
		return strings.TrimSpace(runErr.Error())
	case ctxErr != nil:
		return strings.TrimSpace(ctxErr.Error())
	default:
		return "no non-empty content token received"
	}
}

func (c postgresModelHealthcheckCatalog) ListRunnableRoutes(ctx context.Context) ([]ModelHealthcheckRoute, error) {
	rows, err := c.db.Query(ctx, `
		select
			rc.id,
			rc.requested_model,
			rc.provider_credential_id,
			coalesce(pc.provider, ''),
			coalesce(pc.base_url, ''),
			coalesce(rc.request_mode, '')
		from route_catalog rc
		left join provider_credentials pc on pc.id = rc.provider_credential_id
		where rc.status = 'active'
		  and rc.healthcheck_enabled = true
		  and rc.endpoint = $1
		order by rc.requested_model asc;
	`, modelHealthcheckChatEndpoint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := make([]ModelHealthcheckRoute, 0)
	for rows.Next() {
		var route ModelHealthcheckRoute
		if err := rows.Scan(
			&route.RouteID,
			&route.RequestedModel,
			&route.ProviderCredentialID,
			&route.Provider,
			&route.BaseURL,
			&route.RequestMode,
		); err != nil {
			return nil, err
		}

		route.RouteID = strings.TrimSpace(route.RouteID)
		route.RequestedModel = strings.TrimSpace(route.RequestedModel)
		route.ProviderCredentialID = strings.TrimSpace(route.ProviderCredentialID)
		route.Provider = strings.TrimSpace(route.Provider)
		route.BaseURL = strings.TrimSpace(route.BaseURL)
		route.RequestMode = strings.TrimSpace(route.RequestMode)

		if !isModelHealthcheckChatRequestMode(route.RequestMode) {
			continue
		}
		if route.RouteID == "" || route.RequestedModel == "" || route.ProviderCredentialID == "" {
			continue
		}

		credential, ok, err := c.resolveProviderCredential(ctx, route.ProviderCredentialID)
		if err != nil || !ok {
			continue
		}
		route.Provider = strings.TrimSpace(credential.Provider)
		route.BaseURL = strings.TrimSpace(credential.BaseURL)
		route.APIKey = strings.TrimSpace(credential.APIKey)
		if route.APIKey == "" {
			continue
		}

		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return routes, nil
}

func (c postgresModelHealthcheckCatalog) resolveProviderCredential(ctx context.Context, id string) (store.ProviderCredentialRecord, bool, error) {
	if c.repository == nil {
		return store.ProviderCredentialRecord{}, false, nil
	}

	if resolver, ok := c.repository.(providerCredentialResolver); ok {
		credential, err := resolver.ResolveProviderCredential(ctx, id)
		if err != nil {
			return store.ProviderCredentialRecord{}, false, nil
		}
		return credential, true, nil
	}

	credentials, err := c.repository.ListActiveProviderCredentials(ctx)
	if err != nil {
		return store.ProviderCredentialRecord{}, false, err
	}
	for _, credential := range credentials {
		if strings.TrimSpace(credential.ID) == strings.TrimSpace(id) {
			return credential, true, nil
		}
	}
	return store.ProviderCredentialRecord{}, false, nil
}

func (c postgresModelHealthcheckCatalog) UpdateRouteHealth(ctx context.Context, routeID string, update ModelHealthcheckUpdate) error {
	if strings.TrimSpace(routeID) == "" {
		return fmt.Errorf("route id is required")
	}
	checkedAt := update.LastHealthCheckedAt.UTC()
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}

	_, err := c.db.Exec(ctx, `
		update route_catalog
		set last_health_checked_at = $2,
			health_status = $3,
			last_health_error = $4,
			first_token_latency_ms = greatest($5, 0),
			latency_ms = greatest($6, 0),
			updated_at = now()
		where id = $1;
	`,
		routeID,
		checkedAt,
		strings.TrimSpace(update.HealthStatus),
		strings.TrimSpace(update.LastHealthError),
		update.FirstTokenLatencyMS,
		update.LatencyMS,
	)
	if err != nil {
		return err
	}

	var requestedModel string
	var providerCredentialID string
	var routeLabel string
	var requestMode string
	if err := c.db.QueryRow(ctx, `
		select
			rc.requested_model,
			rc.provider_credential_id,
			coalesce(nullif(pc.display_name, ''), nullif(rc.resolved_provider, ''), rc.provider_credential_id),
			coalesce(rc.request_mode, '')
		from route_catalog rc
		left join provider_credentials pc on pc.id = rc.provider_credential_id
		where rc.id = $1;
	`, routeID).Scan(&requestedModel, &providerCredentialID, &routeLabel, &requestMode); err != nil {
		return err
	}

	_, err = c.db.Exec(ctx, `
		insert into model_healthcheck_history (
			id,
			route_id,
			requested_model,
			provider_credential_id,
			route_label,
			health_status,
			last_health_error,
			request_mode,
			latency_ms,
			first_token_latency_ms,
			checked_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
	`, uuid.NewString(), routeID, requestedModel, providerCredentialID, routeLabel, normalizeModelHealthStatus(update.HealthStatus, update.LastHealthError), strings.TrimSpace(update.LastHealthError), requestMode, maxInt64(update.LatencyMS, 0), maxInt64(update.FirstTokenLatencyMS, 0), checkedAt)
	return err
}

func isModelHealthcheckChatRequestMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "聊天", "推理":
		return true
	default:
		return false
	}
}
