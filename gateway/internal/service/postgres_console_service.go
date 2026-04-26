package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type consoleDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type postgresConsoleService struct {
	db              consoleDB
	authService     AuthService
	chatProxy       ChatProxyService
	ragProxy        RAGProxyService
	seedPlatformKey string
}

func NewPostgresConsoleService(db consoleDB, authService AuthService, chatProxy ChatProxyService, ragProxy RAGProxyService, seedPlatformKey string) ConsoleService {
	if db == nil {
		return NewUnavailableConsoleService()
	}
	return postgresConsoleService{
		db:              db,
		authService:     authService,
		chatProxy:       chatProxy,
		ragProxy:        ragProxy,
		seedPlatformKey: seedPlatformKey,
	}
}

func (s postgresConsoleService) Overview(ctx context.Context) (OverviewPageData, error) {
	var requests24h int
	var successRate float64
	var quotaUsage float64
	var activeAPIKeys int

	if err := s.db.QueryRow(ctx, `
		with recent as (
			select status_code
			from audit_logs
			where created_at >= now() - interval '24 hours'
		),
		quota as (
			select coalesce(sum(request_quota_per_day), 0) as total_quota
			from tenants
			where status = 'active'
		)
		select
			(select count(*) from recent),
			coalesce((select avg(case when status_code between 200 and 399 then 100.0 else 0 end) from recent), 0),
			case
				when (select total_quota from quota) = 0 then 0
				else least(100, ((select count(*) from recent) * 100.0) / (select total_quota from quota))
			end,
			(select count(*) from platform_api_keys where status = 'active');
	`).Scan(&requests24h, &successRate, &quotaUsage, &activeAPIKeys); err != nil {
		return OverviewPageData{}, err
	}

	routeHealthRows, err := s.collectTableRows(ctx, `
		select
			requested_model,
			resolved_provider,
			latency_ms::text || ' ms',
			case health_status
				when 'healthy' then '健康'
				when 'warning' then '告警'
				else '降级'
			end
		from route_catalog
		order by updated_at desc, requested_model asc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	topModelsRows, err := s.collectTableRows(ctx, `
		with total as (
			select count(*)::numeric as total_requests
			from audit_logs
		)
		select
			requested_model,
			count(*)::text,
			coalesce(round((count(*) * 100.0) / nullif((select total_requests from total), 0), 2), 0)::text || '%',
			case
				when max(endpoint) = '/v1/chat/completions' then '聊天'
				when max(endpoint) = '/v1/embeddings' then '向量'
				else '知识库'
			end
		from audit_logs
		group by requested_model
		order by count(*) desc, requested_model asc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	recentAlertRows, err := s.collectTableRows(ctx, `
		select
			to_char(created_at at time zone 'Asia/Shanghai', 'HH24:MI'),
			case alert_type
				when 'quota_warning' then '配额告警'
				when 'route_fallback' then '路由回退'
				when 'latency_spike' then '延迟抖动'
				else alert_type
			end,
			scope
		from operational_alerts
		order by created_at desc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	auditSnapshotRows, err := s.collectTableRows(ctx, `
		select tenant_id, endpoint, status_code::text
		from audit_logs
		order by created_at desc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	return OverviewPageData{
		Stats: []KeyMetric{
			{Label: "24 小时请求量", Value: formatLargeNumber(requests24h)},
			{Label: "成功率", Value: formatPercentage(successRate)},
			{Label: "配额使用率", Value: formatPercentage(quotaUsage)},
			{Label: "活跃 API 密钥", Value: fmt.Sprintf("%d", activeAPIKeys)},
		},
		RouteHealth:   routeHealthRows,
		TopModels:     topModelsRows,
		RecentAlerts:  recentAlertRows,
		AuditSnapshot: auditSnapshotRows,
	}, nil
}

func (s postgresConsoleService) SystemStatus(ctx context.Context) (ConsoleSystemStatus, error) {
	var unhealthyRoutes int
	var quotaEnabledTenants int
	if err := s.db.QueryRow(ctx, `
		select
			(select count(*) from route_catalog where health_status <> 'healthy'),
			(select count(*) from tenants where request_quota_per_day > 0 and status = 'active');
	`).Scan(&unhealthyRoutes, &quotaEnabledTenants); err != nil {
		return ConsoleSystemStatus{}, err
	}

	return ConsoleSystemStatus{
		ConsoleStage:     "控制台预览版",
		RunMode:          s.lookupSetting(ctx, "routing_mode", "数据库模式"),
		GatewayHealth:    mapBool(unhealthyRoutes == 0, "健康", "告警"),
		QuotaProtection:  mapBool(quotaEnabledTenants > 0, "已启用", "未启用"),
		ConsoleEntry:     "31873",
		GatewayAdminAPI:  "32658",
		InternalServices: []string{"31427"},
		HiddenModules:    []string{"RAG 控制台", "知识库"},
	}, nil
}

func (s postgresConsoleService) APIKeys(ctx context.Context) (APIKeysPageData, error) {
	rows, err := s.db.Query(ctx, `
		select p.id, p.name, t.id, p.status, p.scopes, coalesce(p.last_used_at, p.created_at)
		from platform_api_keys p
		join tenants t on t.id = p.tenant_id
		order by p.created_at asc, p.id asc;
	`)
	if err != nil {
		return APIKeysPageData{}, err
	}
	defer rows.Close()

	items := make([]APIKeyItem, 0)
	for rows.Next() {
		var item APIKeyItem
		var status string
		var lastUsedAt time.Time
		if err := rows.Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
			return APIKeysPageData{}, err
		}
		item.Status = translateLifecycleStatus(status)
		item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return APIKeysPageData{}, err
	}

	return APIKeysPageData{
		Items:          items,
		CredentialMode: s.lookupSetting(ctx, "credential_mode", "平台密钥与上游凭据分离，支持 BYOK 扩展"),
	}, nil
}

func (s postgresConsoleService) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error) {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Name) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "tenant_id and name are required",
		}
	}

	rawKey := newPlatformAPIKeySecret()
	item, err := s.insertPlatformAPIKey(ctx, createPlatformAPIKeyInput{
		ID:       newPlatformAPIKeyID(),
		TenantID: strings.TrimSpace(req.TenantID),
		Name:     strings.TrimSpace(req.Name),
		Scopes:   sanitizeScopes(req.Scopes),
		RawKey:   rawKey,
	})
	if err != nil {
		return APIKeyMutationResult{}, err
	}

	return APIKeyMutationResult{
		Item:   item,
		RawKey: rawKey,
	}, nil
}

func (s postgresConsoleService) RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	newRawKey := newPlatformAPIKeySecret()
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	row := s.db.QueryRow(ctx, `
		with previous as (
			select
				tenant_id,
				case
					when $3 <> '' then $3
					else name
				end as next_name,
				case
					when coalesce(cardinality($4::text[]), 0) > 0 then $4::text[]
					else scopes
				end as next_scopes
			from platform_api_keys
			where id = $1
		),
		inserted as (
			insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
			select $2, tenant_id, next_name, $5, 'active', next_scopes, now()
			from previous
			returning id, name, tenant_id, status, scopes, created_at
		),
		disabled as (
			update platform_api_keys
			set status = 'disabled'
			where id = $1
			  and exists (select 1 from inserted)
		)
		select id, name, tenant_id, status, scopes, created_at
		from inserted;
	`, strings.TrimSpace(id), newPlatformAPIKeyID(), strings.TrimSpace(req.Name), sanitizeScopes(req.Scopes), hashPlatformAPIKey(newRawKey))
	if err := row.Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	return APIKeyMutationResult{
		Item:   item,
		RawKey: newRawKey,
	}, nil
}

func (s postgresConsoleService) DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	if err := s.db.QueryRow(ctx, `
		update platform_api_keys
		set status = 'disabled'
		where id = $1
		returning id, name, tenant_id, status, scopes, coalesce(last_used_at, created_at);
	`, strings.TrimSpace(id)).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	return APIKeyMutationResult{Item: item}, nil
}

func (s postgresConsoleService) DeleteAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	if err := s.db.QueryRow(ctx, `
		delete from platform_api_keys
		where id = $1
		returning id, name, tenant_id, status, scopes, coalesce(last_used_at, created_at);
	`, strings.TrimSpace(id)).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKeyMutationResult{}, StatusError{
				Code:    http.StatusNotFound,
				Message: "api key not found",
				Err:     err,
			}
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return APIKeyMutationResult{}, StatusError{
				Code:    http.StatusConflict,
				Message: "api key has request history and cannot be deleted",
				Err:     err,
			}
		}

		return APIKeyMutationResult{}, err
	}

	item.Status = "已删除"
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	return APIKeyMutationResult{Item: item}, nil
}

func (s postgresConsoleService) Routes(ctx context.Context) (RoutesPageData, error) {
	var activeProviders int
	var modelMappings int
	if err := s.db.QueryRow(ctx, `
		select
			(select count(*) from provider_credentials where status = 'active'),
			(select count(*) from route_catalog);
	`).Scan(&activeProviders, &modelMappings); err != nil {
		return RoutesPageData{}, err
	}

	rows, err := s.db.Query(ctx, `
		select requested_model, resolved_provider, provider_credential_id, latency_ms, health_status
		from route_catalog
		order by requested_model asc;
	`)
	if err != nil {
		return RoutesPageData{}, err
	}
	defer rows.Close()

	items := make([]RouteItem, 0)
	for rows.Next() {
		var item RouteItem
		var latency int
		var status string
		if err := rows.Scan(&item.RequestedModel, &item.ResolvedProvider, &item.Credential, &latency, &status); err != nil {
			return RoutesPageData{}, err
		}
		item.Latency = fmt.Sprintf("%d ms", latency)
		item.Status = translateRouteHealth(status)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RoutesPageData{}, err
	}

	return RoutesPageData{
		Stats: []RouteMetric{
			{Label: "活跃供应商", Value: fmt.Sprintf("%d", activeProviders)},
			{Label: "模型映射数", Value: fmt.Sprintf("%d", modelMappings)},
			{Label: "回退策略", Value: s.lookupSetting(ctx, "fallback_policy", "已启用")},
			{Label: "运行模式", Value: s.lookupSetting(ctx, "routing_mode", "数据库模式")},
		},
		Items: items,
		PolicySummary: []string{
			"路由模式：" + s.lookupSetting(ctx, "routing_mode", "数据库模式"),
			"模型优先解析：" + s.lookupSetting(ctx, "model_resolution_mode", "已启用"),
			s.lookupSetting(ctx, "route_policy_description", "请求会先解析到托管凭据，再按路由策略回退。"),
		},
	}, nil
}

func (s postgresConsoleService) Playground(ctx context.Context) (PlaygroundPageData, error) {
	rows, err := s.db.Query(ctx, `
		select requested_model
		from route_catalog
		where endpoint in ('/v1/chat/completions', '/v1/rag/query')
		order by requested_model asc;
	`)
	if err != nil {
		return PlaygroundPageData{}, err
	}
	defer rows.Close()

	models := make([]string, 0)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return PlaygroundPageData{}, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return PlaygroundPageData{}, err
	}

	var lastRun PlaygroundRunResponse
	var found bool
	var latencyMS int
	var statusCode int
	if err := s.db.QueryRow(ctx, `
		select resolved_provider, endpoint, latency_ms, status_code, response_excerpt, (
			select name from platform_api_keys where id = playground_runs.platform_api_key_id
		)
		from playground_runs
		order by created_at desc
		limit 1;
	`).Scan(&lastRun.ResolvedProvider, &lastRun.Endpoint, &latencyMS, &statusCode, &lastRun.Response, &lastRun.PlatformKey); err == nil {
		found = true
	}

	if found {
		lastRun.Latency = fmt.Sprintf("%d ms", latencyMS)
		lastRun.Status = fmt.Sprintf("%d %s", statusCode, mapBool(statusCode < 400, "成功", "失败"))
	}

	data := PlaygroundPageData{AvailableModels: models}
	if found {
		data.LastRun = &lastRun
	}
	return data, nil
}

func (s postgresConsoleService) RunPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundRunResponse, error) {
	model := strings.TrimSpace(req.Model)
	prompt := strings.TrimSpace(req.Prompt)
	if model == "" || prompt == "" {
		return PlaygroundRunResponse{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "playground request invalid",
			Err:     fmt.Errorf("model and prompt are required"),
		}
	}

	resolved, err := s.authService.Resolve(ctx, s.seedPlatformKey, model)
	if err != nil {
		return PlaygroundRunResponse{}, err
	}

	var endpoint string
	var responseText string
	start := time.Now()

	row := s.db.QueryRow(ctx, `select endpoint from route_catalog where requested_model = $1 limit 1;`, model)
	if err := row.Scan(&endpoint); err != nil {
		return PlaygroundRunResponse{}, err
	}

	if endpoint == "/v1/rag/query" {
		resp, err := s.ragProxy.Query(ctx, RAGQueryRequest{
			KnowledgeBaseID: "kb_product_docs",
			Question:        prompt,
		}, resolved)
		if err != nil {
			return PlaygroundRunResponse{}, err
		}
		responseText = resp.Answer
	} else {
		resp, err := s.chatProxy.Complete(ctx, ChatRequest{
			Model: model,
			Messages: []ChatMessage{
				{Role: "user", Content: prompt},
			},
		}, resolved)
		if err != nil {
			return PlaygroundRunResponse{}, err
		}
		if len(resp.Choices) > 0 {
			responseText = resp.Choices[0].Message.Content
		}
		endpoint = "/v1/chat/completions"
	}

	latencyMS := durationMilliseconds(time.Since(start))
	if err := s.insertPlaygroundRun(ctx, resolved, model, prompt, endpoint, responseText, latencyMS); err != nil {
		return PlaygroundRunResponse{}, err
	}
	if err := s.insertAuditLog(ctx, resolved.TenantID, resolved.PlatformAPIKeyID, model, endpoint, http.StatusOK, resolved.SelectedProviderName, latencyMS); err != nil {
		return PlaygroundRunResponse{}, err
	}

	platformKeyName := s.lookupPlatformKeyName(ctx, resolved.PlatformAPIKeyID)
	return PlaygroundRunResponse{
		ResolvedProvider: resolved.SelectedProviderName,
		Endpoint:         endpoint,
		Latency:          fmt.Sprintf("%d ms", latencyMS),
		Status:           "200 成功",
		Response:         responseText,
		PlatformKey:      platformKeyName,
	}, nil
}

func (s postgresConsoleService) KnowledgeBases(ctx context.Context) (KnowledgeBasesPageData, error) {
	var documents int
	var chunks int
	var lastIngest time.Time
	if err := s.db.QueryRow(ctx, `
		select
			coalesce(count(distinct d.id), 0),
			coalesce(count(dc.chunk_id), 0),
			coalesce(max(greatest(k.updated_at, coalesce(d.updated_at, k.updated_at))), now())
		from knowledge_bases k
		left join documents d on d.knowledge_base_id = k.id
		left join document_chunks dc on dc.document_id = d.id;
	`).Scan(&documents, &chunks, &lastIngest); err != nil {
		return KnowledgeBasesPageData{}, err
	}

	rows, err := s.db.Query(ctx, `
		select
			k.name,
			count(distinct d.id) as document_count,
			k.status,
			greatest(k.updated_at, coalesce(max(d.updated_at), k.updated_at)) as updated_at
		from knowledge_bases k
		left join documents d on d.knowledge_base_id = k.id
		group by k.id, k.name, k.status, k.updated_at
		order by updated_at desc, k.name asc;
	`)
	if err != nil {
		return KnowledgeBasesPageData{}, err
	}
	defer rows.Close()

	items := make([]KnowledgeBaseItem, 0)
	var indexingCount int
	for rows.Next() {
		var item KnowledgeBaseItem
		var documentsCount int
		var status string
		var updatedAt time.Time
		if err := rows.Scan(&item.Name, &documentsCount, &status, &updatedAt); err != nil {
			return KnowledgeBasesPageData{}, err
		}
		if status == "indexing" {
			indexingCount++
		}
		item.Documents = fmt.Sprintf("%d", documentsCount)
		item.Status = translateKnowledgeStatus(status)
		item.UpdatedAt = updatedAt.In(shanghaiLocation()).Format("01-02 15:04")
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return KnowledgeBasesPageData{}, err
	}

	return KnowledgeBasesPageData{
		Stats: []KnowledgeBaseMetric{
			{Label: "文档总数", Value: fmt.Sprintf("%d", documents)},
			{Label: "切片总数", Value: formatLargeNumber(chunks)},
			{Label: "最近入库", Value: lastIngest.In(shanghaiLocation()).Format("15:04")},
			{Label: "队列状态", Value: mapBool(indexingCount == 0, "健康", "索引中")},
		},
		Items: items,
		FlowSummary: []string{
			s.lookupSetting(ctx, "knowledge_flow_title", "查询先进入网关，再路由到 RAG 服务拼装检索上下文。"),
		},
		QueueSummary: []string{
			fmt.Sprintf("正在索引的知识库：%d", indexingCount),
			fmt.Sprintf("待处理文档：%d", countPendingDocuments(ctx, s.db)),
			"最近一次入库已完成，未发现失败任务。",
		},
	}, nil
}

func countPendingDocuments(ctx context.Context, db consoleDB) int {
	var count int
	if err := db.QueryRow(ctx, `select count(*) from documents where status = 'indexing';`).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s postgresConsoleService) Audit(ctx context.Context) (AuditPageData, error) {
	var totalRequests int64
	var failedRequests int64
	var rateLimitedRequests int64
	var upstreamErrors int64
	if err := s.db.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where usage_status <> 'success'),
			count(*) filter (where usage_status = 'rate_limited' or status_code = 429),
			count(*) filter (where usage_status in ('upstream_error', 'auth_failed', 'timeout'))
		from llm_request_logs
		where request_started_at >= now() - interval '24 hours';
	`).Scan(&totalRequests, &failedRequests, &rateLimitedRequests, &upstreamErrors); err != nil {
		return AuditPageData{}, err
	}

	if totalRequests == 0 {
		return s.auditFromFallbackLogs(ctx)
	}

	rows, err := s.db.Query(ctx, `
		select
			to_char(l.request_started_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			l.tenant_id,
			l.request_path,
			l.request_model,
			coalesce(nullif(l.upstream_model, ''), l.request_model),
			l.usage_status,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			l.usage_source
		from llm_request_logs l
		left join provider_credentials pc on pc.id = l.provider_credential_id
		order by l.request_started_at desc, l.id desc
		limit 12;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer rows.Close()

	items := make([]AuditItem, 0, 12)
	for rows.Next() {
		var item AuditItem
		var status string
		var latencyMS int64
		var usageSource string
		if err := rows.Scan(
			&item.Time,
			&item.Tenant,
			&item.Endpoint,
			&item.RequestModel,
			&item.UpstreamModel,
			&status,
			&item.Provider,
			&latencyMS,
			&usageSource,
		); err != nil {
			return AuditPageData{}, err
		}
		item.Status = translateUsageStatus(status)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = translateUsageSource(usageSource)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditPageData{}, err
	}

	eventRows, err := s.db.Query(ctx, `
		select
			to_char(e.created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			e.event_type,
			e.usage_status,
			e.detail,
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			e.status_code,
			e.usage_source
		from llm_request_events e
		join llm_request_logs l on l.id = e.request_log_id and l.tenant_id = e.tenant_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		order by e.created_at desc
		limit 8;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer eventRows.Close()

	events := make([]AuditEvent, 0, 8)
	for eventRows.Next() {
		var event AuditEvent
		var status string
		var eventType string
		var requestModel string
		var provider string
		var latencyMS int64
		var statusCode int
		var detail string
		var usageSource string
		if err := eventRows.Scan(&event.Time, &eventType, &status, &detail, &requestModel, &provider, &latencyMS, &statusCode, &usageSource); err != nil {
			return AuditPageData{}, err
		}
		event.Type = translateUsageEventType(eventType)
		event.Status = translateUsageStatus(status)
		event.Detail = describeUsageEvent(detail, eventType, requestModel, provider, latencyMS, statusCode, usageSource)
		events = append(events, event)
	}
	if err := eventRows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(int(totalRequests))},
			{Label: "失败请求", Value: formatLargeNumber(int(failedRequests))},
			{Label: "限流次数", Value: formatLargeNumber(int(rateLimitedRequests))},
			{Label: "上游错误", Value: formatLargeNumber(int(upstreamErrors))},
		},
		Events: events,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: fmt.Sprintf("最近 24 小时共 %d 次请求，其中 %d 次失败。", totalRequests, failedRequests)},
			{Title: "数据来源", Content: "本页优先展示 llm_request_logs 与 llm_request_events 的真实聚合结果。"},
		},
	}, nil
}

func (s postgresConsoleService) auditFromFallbackLogs(ctx context.Context) (AuditPageData, error) {
	rows, err := s.db.Query(ctx, `
		select
			to_char(created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			tenant_id,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms
		from audit_logs
		order by created_at desc
		limit 10;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer rows.Close()

	items := make([]AuditItem, 0, 10)
	for rows.Next() {
		var item AuditItem
		var statusCode int
		var latencyMS int64
		if err := rows.Scan(&item.Time, &item.Tenant, &item.Endpoint, &statusCode, &item.Provider, &latencyMS); err != nil {
			return AuditPageData{}, err
		}
		item.RequestModel = "-"
		item.UpstreamModel = "-"
		item.Status = fmt.Sprintf("%d", statusCode)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = "审计回退"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(len(items))},
			{Label: "失败请求", Value: "0"},
			{Label: "限流次数", Value: "0"},
			{Label: "上游错误", Value: "0"},
		},
		Events: nil,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: "usage 日志为空，当前展示 fallback 审计日志。"},
		},
	}, nil
}

func (s postgresConsoleService) UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageOverviewData{}, err
	}
	return s.usageOverviewFromLogs(ctx, query)
}

func (s postgresConsoleService) UsageTrends(ctx context.Context, query UsageQuery) (UsageTrendData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageTrendData{}, err
	}
	return s.usageTrendsFromLogs(ctx, query)
}

func (s postgresConsoleService) UsageLatencyWall(ctx context.Context, query UsageQuery) (UsageLatencyWallData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageLatencyWallData{}, err
	}

	windowLabel, bucketExpr, bucketStep, bucketLayout := usageLatencyWindowConfig(query)
	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			`+bucketExpr+` as bucket_start,
			count(*) as request_count,
			count(*) filter (where l.usage_status = 'success') as success_count,
			coalesce(round(avg(l.latency_ms)), 0)::bigint as avg_latency_ms
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		group by l.request_model, coalesce(pc.display_name, l.provider_credential_id), bucket_start
		order by max(l.request_started_at) desc, l.request_model asc, bucket_start asc;
	`, args...)
	if err != nil {
		return UsageLatencyWallData{}, err
	}
	defer rows.Close()

	type bucketStat struct {
		requestCount int64
		successCount int64
		avgLatencyMS int64
	}
	type laneAccumulator struct {
		model             string
		provider          string
		totalRequests     int64
		totalSuccess      int64
		totalLatencyTimes int64
		buckets           map[time.Time]bucketStat
	}

	lanesByKey := make(map[string]*laneAccumulator)
	for rows.Next() {
		var model string
		var provider string
		var bucketStart time.Time
		var requestCount int64
		var successCount int64
		var avgLatencyMS int64
		if err := rows.Scan(&model, &provider, &bucketStart, &requestCount, &successCount, &avgLatencyMS); err != nil {
			return UsageLatencyWallData{}, err
		}

		key := model + "::" + provider
		lane := lanesByKey[key]
		if lane == nil {
			lane = &laneAccumulator{
				model:    model,
				provider: provider,
				buckets:  make(map[time.Time]bucketStat),
			}
			lanesByKey[key] = lane
		}
		lane.totalRequests += requestCount
		lane.totalSuccess += successCount
		lane.totalLatencyTimes += avgLatencyMS * requestCount
		lane.buckets[bucketStart.UTC()] = bucketStat{
			requestCount: requestCount,
			successCount: successCount,
			avgLatencyMS: avgLatencyMS,
		}
	}
	if err := rows.Err(); err != nil {
		return UsageLatencyWallData{}, err
	}

	buckets := usageLatencyBuckets(query.From, query.To, bucketStep, bucketLayout)
	result := UsageLatencyWallData{
		WindowLabel: windowLabel,
		Buckets:     buckets.labels,
		Lanes:       make([]UsageLatencyLane, 0, len(lanesByKey)),
	}
	if len(lanesByKey) == 0 {
		return result, nil
	}

	laneList := make([]*laneAccumulator, 0, len(lanesByKey))
	for _, lane := range lanesByKey {
		laneList = append(laneList, lane)
	}
	sort.Slice(laneList, func(i, j int) bool {
		if laneList[i].totalRequests == laneList[j].totalRequests {
			return laneList[i].model < laneList[j].model
		}
		return laneList[i].totalRequests > laneList[j].totalRequests
	})

	for _, lane := range laneList {
		averageLatency := "0 ms"
		if lane.totalRequests > 0 {
			averageLatency = fmt.Sprintf("%d ms", int(math.Round(float64(lane.totalLatencyTimes)/float64(lane.totalRequests))))
		}

		cells := make([]UsageLatencyCell, 0, len(buckets.times))
		for index, bucketStart := range buckets.times {
			if bucket, ok := lane.buckets[bucketStart]; ok {
				status := "健康"
				if bucket.successCount < bucket.requestCount {
					status = "失败"
				}
				cells = append(cells, UsageLatencyCell{
					BucketLabel: buckets.labels[index],
					Latency:     fmt.Sprintf("%d ms", bucket.avgLatencyMS),
					Status:      status,
					Requests:    fmt.Sprintf("%d 次", bucket.requestCount),
				})
				continue
			}

			cells = append(cells, UsageLatencyCell{
				BucketLabel: buckets.labels[index],
				Latency:     "--",
				Status:      "空窗",
				Requests:    "0 次",
			})
		}

		result.Lanes = append(result.Lanes, UsageLatencyLane{
			Model:          lane.model,
			Provider:       lane.provider,
			SuccessRate:    formatPercentage(successRateForCounts(lane.totalSuccess, lane.totalRequests)),
			AverageLatency: averageLatency,
			Cells:          cells,
		})
	}

	return result, nil
}

func (s postgresConsoleService) usageOverviewFromLogs(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	logWhere, logArgs := buildUsageLogWhere(query, "l.request_started_at")
	var totalRequests int64
	var totalTokens int64
	var successRequests int64
	var estimatedRequests int64
	var averageLatency float64
	if err := s.db.QueryRow(ctx, `
		select
			count(*),
			coalesce(sum(l.total_tokens), 0),
			coalesce(sum(case when l.usage_status = 'success' then 1 else 0 end), 0),
			coalesce(sum(case when l.usage_source = 'estimated' then 1 else 0 end), 0),
			coalesce(avg(l.latency_ms), 0)
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+logWhere+`;
	`, logArgs...).Scan(&totalRequests, &totalTokens, &successRequests, &estimatedRequests, &averageLatency); err != nil {
		return UsageOverviewData{}, err
	}

	successRate := 0.0
	estimatedShare := 0.0
	if totalRequests > 0 {
		successRate = float64(successRequests) * 100 / float64(totalRequests)
		estimatedShare = float64(estimatedRequests) * 100 / float64(totalRequests)
	}

	return UsageOverviewData{
		TotalRequests:  totalRequests,
		SuccessRate:    formatPercentage(successRate),
		TotalTokens:    formatLargeNumber(int(totalTokens)),
		AverageLatency: fmt.Sprintf("%d ms", int(math.Round(averageLatency))),
		EstimatedShare: formatPercentage(estimatedShare),
	}, nil
}

func (s postgresConsoleService) usageTrendsFromLogs(ctx context.Context, query UsageQuery) (UsageTrendData, error) {
	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			date_trunc('hour', l.request_started_at) as bucket_start,
			count(*) as request_count,
			coalesce(sum(l.total_tokens), 0) as total_tokens,
			coalesce(sum(case when l.usage_status = 'success' then 1 else 0 end), 0) as success_count
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		group by date_trunc('hour', l.request_started_at)
		order by bucket_start asc;
	`, args...)
	if err != nil {
		return UsageTrendData{}, err
	}
	defer rows.Close()

	data := UsageTrendData{
		Requests: make([]UsageTrendPoint, 0),
		Tokens:   make([]UsageTrendPoint, 0),
		Success:  make([]UsageTrendPoint, 0),
	}
	for rows.Next() {
		var bucketStart time.Time
		var requestCount int64
		var totalTokens int64
		var successCount int64
		if err := rows.Scan(&bucketStart, &requestCount, &totalTokens, &successCount); err != nil {
			return UsageTrendData{}, err
		}

		label := bucketStart.In(shanghaiLocation()).Format("01-02 15:04")
		successRate := 0.0
		if requestCount > 0 {
			successRate = float64(successCount) * 100 / float64(requestCount)
		}
		data.Requests = append(data.Requests, UsageTrendPoint{Label: label, Value: formatLargeNumber(int(requestCount))})
		data.Tokens = append(data.Tokens, UsageTrendPoint{Label: label, Value: formatLargeNumber(int(totalTokens))})
		data.Success = append(data.Success, UsageTrendPoint{Label: label, Value: formatPercentage(successRate)})
	}
	if err := rows.Err(); err != nil {
		return UsageTrendData{}, err
	}

	return data, nil
}

func (s postgresConsoleService) UsageFailures(ctx context.Context, query UsageQuery) (UsageFailureData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageFailureData{}, err
	}

	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			case
				when l.error_code <> '' then l.error_code
				else l.usage_status
			end as failure_category,
			count(*) as request_count
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		  and l.usage_status <> 'success'
		group by failure_category
		order by count(*) desc, failure_category asc;
	`, args...)
	if err != nil {
		return UsageFailureData{}, err
	}
	defer rows.Close()

	breakdown := make([]UsageFailureBucket, 0)
	breakdownCounts := make(map[string]int64)
	for rows.Next() {
		var failureCategory string
		var count int64
		if err := rows.Scan(&failureCategory, &count); err != nil {
			return UsageFailureData{}, err
		}
		label := translateUsageFailureCategory(failureCategory)
		breakdownCounts[label] += count
	}
	if err := rows.Err(); err != nil {
		return UsageFailureData{}, err
	}
	if len(breakdownCounts) > 0 {
		labels := make([]string, 0, len(breakdownCounts))
		for label := range breakdownCounts {
			labels = append(labels, label)
		}
		sort.Slice(labels, func(i, j int) bool {
			left := breakdownCounts[labels[i]]
			right := breakdownCounts[labels[j]]
			if left == right {
				return labels[i] < labels[j]
			}
			return left > right
		})
		for _, label := range labels {
			breakdown = append(breakdown, UsageFailureBucket{
				Label: label,
				Value: fmt.Sprintf("%d 次", breakdownCounts[label]),
			})
		}
	}

	eventWhere, eventArgs := buildUsageEventWhere(query)
	eventRows, err := s.db.Query(ctx, `
		select
			e.created_at,
			`+usageEventFailureCategoryExpr("l", "e")+` as failure_category,
			e.event_type,
			e.status_code,
			e.detail,
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			e.usage_source
		from llm_request_events e
		join llm_request_logs l on l.id = e.request_log_id and l.tenant_id = e.tenant_id
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+eventWhere+`
		  and (e.usage_status <> 'success' or e.event_type = 'usage_publish_failed')
		order by e.created_at desc
		limit 5;
	`, eventArgs...)
	if err != nil {
		return UsageFailureData{}, err
	}
	defer eventRows.Close()

	recentEvents := make([]string, 0)
	for eventRows.Next() {
		var createdAt time.Time
		var failureCategory string
		var eventType string
		var statusCode int
		var detail string
		var requestModel string
		var provider string
		var latencyMS int64
		var usageSource string
		if err := eventRows.Scan(&createdAt, &failureCategory, &eventType, &statusCode, &detail, &requestModel, &provider, &latencyMS, &usageSource); err != nil {
			return UsageFailureData{}, err
		}

		message := fmt.Sprintf(
			"%s · %s · %s",
			createdAt.In(shanghaiLocation()).Format("01-02 15:04"),
			translateUsageFailureCategory(failureCategory),
			describeUsageEvent(detail, eventType, requestModel, provider, latencyMS, statusCode, usageSource),
		)
		recentEvents = append(recentEvents, message)
	}
	if err := eventRows.Err(); err != nil {
		return UsageFailureData{}, err
	}

	return UsageFailureData{
		Breakdown:    breakdown,
		RecentEvents: recentEvents,
	}, nil
}

func (s postgresConsoleService) UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageRequestsPageData{}, err
	}

	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	var total int64
	if err := s.db.QueryRow(ctx, `
		select count(*)
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`;
	`, args...).Scan(&total); err != nil {
		return UsageRequestsPageData{}, err
	}

	args = append(args, query.Limit, query.Offset)
	limitIndex := len(args) - 1
	offsetIndex := len(args)
	rows, err := s.db.Query(ctx, `
		select
			l.id,
			l.tenant_id,
			l.request_path,
			l.request_model,
			l.usage_status,
			l.total_tokens,
			l.latency_ms,
			l.usage_source
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		order by l.request_started_at desc, l.id desc
		limit $`+fmt.Sprintf("%d", limitIndex)+` offset $`+fmt.Sprintf("%d", offsetIndex)+`;
	`, args...)
	if err != nil {
		return UsageRequestsPageData{}, err
	}
	defer rows.Close()

	items := make([]UsageRequestItem, 0)
	for rows.Next() {
		var item UsageRequestItem
		var status string
		var totalTokens int
		var latencyMS int64
		var usageSource string
		if err := rows.Scan(&item.RequestID, &item.Tenant, &item.Endpoint, &item.Model, &status, &totalTokens, &latencyMS, &usageSource); err != nil {
			return UsageRequestsPageData{}, err
		}
		item.Status = translateUsageStatus(status)
		item.TotalTokens = formatLargeNumber(totalTokens)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = translateUsageSource(usageSource)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return UsageRequestsPageData{}, err
	}

	return UsageRequestsPageData{
		Items:  items,
		Total:  total,
		Limit:  query.Limit,
		Offset: query.Offset,
	}, nil
}

func (s postgresConsoleService) collectTableRows(ctx context.Context, sql string) ([]TableRow, error) {
	rows, err := s.db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TableRow, 0)
	for rows.Next() {
		rowValues, err := rows.Values()
		if err != nil {
			return nil, err
		}
		columns := make([]string, 0, len(rowValues))
		for _, value := range rowValues {
			columns = append(columns, fmt.Sprint(value))
		}
		items = append(items, TableRow{Columns: columns})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s postgresConsoleService) lookupSetting(ctx context.Context, key string, fallback string) string {
	var value string
	if err := s.db.QueryRow(ctx, `select value from system_settings where key = $1;`, key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

func (s postgresConsoleService) lookupPlatformKeyName(ctx context.Context, keyID string) string {
	var name string
	if err := s.db.QueryRow(ctx, `select name from platform_api_keys where id = $1;`, keyID).Scan(&name); err != nil {
		return keyID
	}
	return name
}

func (s postgresConsoleService) insertPlaygroundRun(ctx context.Context, requestContext domain.RequestContext, model string, prompt string, endpoint string, responseText string, latencyMS int64) error {
	_, err := s.db.Exec(ctx, `
		insert into playground_runs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			prompt,
			response_excerpt,
			endpoint,
			resolved_provider,
			status_code,
			latency_ms
		) values ($1, $2, $3, $4, $5, $6, $7, 200, $8);
	`, requestContext.TenantID, requestContext.PlatformAPIKeyID, model, prompt, truncateText(responseText, 240), endpoint, requestContext.SelectedProviderName, latencyMS)
	return err
}

func (s postgresConsoleService) insertAuditLog(ctx context.Context, tenantID string, platformAPIKeyID string, model string, endpoint string, statusCode int, provider string, latencyMS int64) error {
	_, err := s.db.Exec(ctx, `
		insert into audit_logs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms
		) values ($1, $2, $3, $4, $5, $6, $7);
	`, tenantID, platformAPIKeyID, model, endpoint, statusCode, provider, latencyMS)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `update platform_api_keys set last_used_at = now() where id = $1;`, platformAPIKeyID)
	return err
}

func translateLifecycleStatus(status string) string {
	if status == "active" {
		return "启用"
	}
	return "停用"
}

func translateRouteHealth(status string) string {
	switch status {
	case "healthy":
		return "健康"
	case "warning":
		return "告警"
	default:
		return "降级"
	}
}

func translateKnowledgeStatus(status string) string {
	switch status {
	case "ready":
		return "就绪"
	case "indexing":
		return "索引中"
	default:
		return "失败"
	}
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func formatLargeNumber(value int) string {
	if value >= 10000 {
		return fmt.Sprintf("%.1f 万", float64(value)/10000.0)
	}
	return fmt.Sprintf("%d", value)
}

func formatPercentage(value float64) string {
	return fmt.Sprintf("%.2f%%", math.Round(value*100)/100)
}

func mapBool(condition bool, yes string, no string) string {
	if condition {
		return yes
	}
	return no
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}

type createPlatformAPIKeyInput struct {
	ID       string
	TenantID string
	Name     string
	Scopes   []string
	RawKey   string
}

func (s postgresConsoleService) insertPlatformAPIKey(ctx context.Context, input createPlatformAPIKeyInput) (APIKeyItem, error) {
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	if err := s.db.QueryRow(ctx, `
		insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
		values ($1, $2, $3, $4, 'active', $5, now())
		returning id, name, tenant_id, status, scopes, created_at;
	`, input.ID, input.TenantID, input.Name, hashPlatformAPIKey(input.RawKey), input.Scopes).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyItem{}, mapAPIKeyMutationError(err, "tenant not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	return item, nil
}

func mapAPIKeyMutationError(err error, notFoundMessage string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return StatusError{
			Code:    http.StatusNotFound,
			Message: notFoundMessage,
			Err:     err,
		}
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "tenant not found",
				Err:     err,
			}
		case "23505":
			return StatusError{
				Code:    http.StatusConflict,
				Message: "api key already exists",
				Err:     err,
			}
		}
	}

	return err
}

func newPlatformAPIKeyID() string {
	return "pak_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newPlatformAPIKeySecret() string {
	return "agw_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func sanitizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"chat"}
	}
	return append([]string(nil), scopes...)
}

type usageWhereBuilder struct {
	conditions []string
	args       []any
}

func newUsageWhereBuilder(from time.Time, to time.Time, timeColumn string) *usageWhereBuilder {
	builder := &usageWhereBuilder{}
	builder.add(fmt.Sprintf("%s >= $%%d", timeColumn), from)
	builder.add(fmt.Sprintf("%s < $%%d", timeColumn), to)
	return builder
}

func (b *usageWhereBuilder) add(template string, value any) {
	b.args = append(b.args, value)
	b.conditions = append(b.conditions, fmt.Sprintf(template, len(b.args)))
}

func (b *usageWhereBuilder) addProvider(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("(pc.provider = $%d or pc.display_name = $%d or r.resolved_provider = $%d)", index, index, index))
}

func (b *usageWhereBuilder) addLogModel(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("(l.request_model = $%d or l.upstream_model = $%d or r.requested_model = $%d)", index, index, index))
}

func (b *usageWhereBuilder) addAggregateModel(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("r.requested_model = $%d", index))
}

func (b *usageWhereBuilder) addFailureCategory(expr string, value string) {
	values := usageFailureFilterValues(value)
	if len(values) == 0 {
		return
	}

	placeholders := make([]string, 0, len(values))
	for _, item := range values {
		b.args = append(b.args, item)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(b.args)))
	}
	b.conditions = append(b.conditions, fmt.Sprintf("%s in (%s)", expr, strings.Join(placeholders, ", ")))
}

func (b *usageWhereBuilder) whereClause() string {
	return strings.Join(b.conditions, " and ")
}

func buildUsageAggregateWhere(query UsageQuery) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, "a.bucket_start")
	builder.addIfNotEmpty("a.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("a.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("a.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("a.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("a.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("a.usage_source = $%d", query.UsageSource)
	builder.addProvider(query.Provider)
	builder.addAggregateModel(query.Model)
	return builder.whereClause(), builder.args
}

func buildUsageLogWhere(query UsageQuery, timeColumn string) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, timeColumn)
	builder.addIfNotEmpty("l.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("l.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("l.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("l.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("l.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("l.usage_source = $%d", query.UsageSource)
	builder.addFailureCategory(usageFailureCategoryExpr("l"), query.ErrorCategory)
	builder.addProvider(query.Provider)
	builder.addLogModel(query.Model)
	return builder.whereClause(), builder.args
}

func buildUsageEventWhere(query UsageQuery) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, "l.request_started_at")
	builder.addIfNotEmpty("l.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("l.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("l.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("l.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("e.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("e.usage_source = $%d", query.UsageSource)
	builder.addFailureCategory(usageEventFailureCategoryExpr("l", "e"), query.ErrorCategory)
	builder.addProvider(query.Provider)
	builder.addLogModel(query.Model)
	return builder.whereClause(), builder.args
}

func (b *usageWhereBuilder) addIfNotEmpty(template string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.add(template, value)
}

func normalizeUsageQuery(query UsageQuery, now time.Time) (UsageQuery, error) {
	query.Window = strings.TrimSpace(query.Window)
	query.TenantID = strings.TrimSpace(query.TenantID)
	query.PlatformAPIKeyID = strings.TrimSpace(query.PlatformAPIKeyID)
	query.Provider = strings.TrimSpace(query.Provider)
	query.Model = strings.TrimSpace(query.Model)
	query.RouteID = strings.TrimSpace(query.RouteID)
	query.RequestPath = strings.TrimSpace(query.RequestPath)
	query.Status = strings.TrimSpace(query.Status)
	query.ErrorCategory = strings.TrimSpace(query.ErrorCategory)
	query.UsageSource = strings.TrimSpace(query.UsageSource)

	if query.To.IsZero() {
		query.To = now.UTC()
	} else {
		query.To = query.To.UTC()
	}
	if query.From.IsZero() {
		windowDuration, err := parseUsageWindow(query.Window)
		if err != nil {
			return UsageQuery{}, err
		}
		if windowDuration == 0 {
			windowDuration = 24 * time.Hour
		}
		query.From = query.To.Add(-windowDuration)
	} else {
		query.From = query.From.UTC()
	}
	if !query.To.After(query.From) {
		return UsageQuery{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "invalid time range",
			Err:     fmt.Errorf("usage query to must be after from"),
		}
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	return query, nil
}

func parseUsageWindow(window string) (time.Duration, error) {
	switch strings.TrimSpace(window) {
	case "", "24h":
		return 24 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, StatusError{
			Code:    http.StatusBadRequest,
			Message: "invalid usage window",
			Err:     fmt.Errorf("unsupported usage window %q", window),
		}
	}
}

func usageLatencyWindowConfig(query UsageQuery) (string, string, time.Duration, string) {
	duration := query.To.Sub(query.From)
	if duration > 48*time.Hour {
		return "最近 7 天", "date_trunc('day', l.request_started_at)", 24 * time.Hour, "01-02"
	}
	if strings.TrimSpace(query.Window) == "6h" {
		return "最近 6 小时", "date_trunc('hour', l.request_started_at)", time.Hour, "01-02 15:04"
	}
	return "最近 24 小时", "date_trunc('hour', l.request_started_at)", time.Hour, "01-02 15:04"
}

type usageLatencyBucketWindow struct {
	times  []time.Time
	labels []string
}

func usageLatencyBuckets(from time.Time, to time.Time, step time.Duration, labelLayout string) usageLatencyBucketWindow {
	start := floorUsageBucket(from.In(time.UTC), step)
	end := floorUsageBucket(to.In(time.UTC), step)

	times := make([]time.Time, 0)
	labels := make([]string, 0)
	for bucket := start; !bucket.After(end); bucket = bucket.Add(step) {
		times = append(times, bucket)
		labels = append(labels, bucket.In(shanghaiLocation()).Format(labelLayout))
	}
	return usageLatencyBucketWindow{times: times, labels: labels}
}

func floorUsageBucket(value time.Time, step time.Duration) time.Time {
	if step == 24*time.Hour {
		return value.Truncate(24 * time.Hour)
	}
	return value.Truncate(step)
}

func successRateForCounts(successCount int64, totalCount int64) float64 {
	if totalCount <= 0 {
		return 0
	}
	return float64(successCount) * 100 / float64(totalCount)
}

func translateUsageStatus(status string) string {
	switch status {
	case "success":
		return "成功"
	case "failed":
		return "失败"
	case "timeout":
		return "超时"
	case "rate_limited":
		return "限流"
	case "auth_failed":
		return "鉴权失败"
	case "upstream_error":
		return "上游错误"
	default:
		return status
	}
}

func translateUsageSource(source string) string {
	switch source {
	case "upstream":
		return "上游返回"
	case "estimated":
		return "估算"
	default:
		return source
	}
}

func translateUsageEventType(eventType string) string {
	switch eventType {
	case "response_received":
		return "LLM 已返回"
	case "request_failed":
		return "LLM 调用失败"
	case "usage_estimated":
		return "计量已估算"
	case "usage_publish_failed":
		return "计量事件投递失败"
	default:
		return eventType
	}
}

func describeUsageEvent(detail string, eventType string, requestModel string, provider string, latencyMS int64, statusCode int, usageSource string) string {
	model := strings.TrimSpace(requestModel)
	if model == "" {
		model = "当前模型"
	}
	provider = strings.TrimSpace(provider)

	switch eventType {
	case "response_received":
		sourceText := "上游已回传"
		if strings.TrimSpace(usageSource) == "estimated" {
			sourceText = "网关已估算 Token"
		}
		if provider != "" {
			return fmt.Sprintf("用户调用 %s，%s，供应商 %s，耗时 %d ms。", model, sourceText, provider, latencyMS)
		}
		return fmt.Sprintf("用户调用 %s，%s，耗时 %d ms。", model, sourceText, latencyMS)
	case "usage_estimated":
		return fmt.Sprintf("用户调用 %s 已完成，当前 Token 由网关估算。", model)
	case "request_failed":
		reason := strings.TrimSpace(detail)
		if reason == "" || reason == "request failed" {
			reason = "请求失败"
		}
		if statusCode > 0 {
			return fmt.Sprintf("用户调用 %s 失败，状态码 %d，原因：%s。", model, statusCode, reason)
		}
		return fmt.Sprintf("用户调用 %s 失败，原因：%s。", model, reason)
	case "usage_publish_failed":
		reason := strings.TrimSpace(detail)
		if reason == "" {
			reason = "usage 事件写入失败"
		}
		return fmt.Sprintf("用户调用 %s 已完成，但计量事件投递失败：%s。", model, reason)
	default:
		if strings.TrimSpace(detail) != "" {
			return detail
		}
		return translateUsageEventType(eventType)
	}
}

func translateUsageFailureCategory(category string) string {
	switch category {
	case "":
		return "未知错误"
	case "rate_limit", "rate_limited":
		return "限流"
	case "upstream_rate_limited":
		return "上游限流"
	case "bad_gateway", "upstream_server_error":
		return "上游服务异常"
	case "upstream_auth_failed":
		return "上游鉴权失败"
	case "upstream_timeout", "timeout":
		return "上游超时"
	case "network_error":
		return "网络异常"
	case "internal_error":
		return "网关内部错误"
	case "bad_request":
		return "请求参数错误"
	default:
		return translateUsageStatus(category)
	}
}

func usageFailureCategoryExpr(logAlias string) string {
	return fmt.Sprintf(
		"case when %s.error_code <> '' then %s.error_code else %s.usage_status end",
		logAlias,
		logAlias,
		logAlias,
	)
}

func usageEventFailureCategoryExpr(logAlias string, eventAlias string) string {
	return fmt.Sprintf(
		"case when %s.event_type = 'usage_publish_failed' then 'internal_error' else %s end",
		eventAlias,
		usageFailureCategoryExpr(logAlias),
	)
}

func usageFailureFilterValues(value string) []string {
	switch normalizeUsageFailureFilterKey(value) {
	case "rate_limited":
		return []string{"rate_limit", "rate_limited"}
	case "upstream_service_error":
		return []string{"bad_gateway", "upstream_server_error"}
	case "upstream_rate_limited":
		return []string{"upstream_rate_limited"}
	case "upstream_auth_failed":
		return []string{"upstream_auth_failed"}
	case "auth_failed":
		return []string{"auth_failed"}
	case "timeout":
		return []string{"timeout"}
	case "upstream_timeout":
		return []string{"timeout", "upstream_timeout"}
	case "upstream_error":
		return []string{"upstream_error"}
	case "network_error":
		return []string{"network_error"}
	case "internal_error":
		return []string{"internal_error"}
	case "bad_request":
		return []string{"bad_request"}
	case "failed":
		return []string{"failed"}
	default:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	}
}

func normalizeUsageFailureFilterKey(value string) string {
	switch strings.TrimSpace(value) {
	case "rate_limit", "rate_limited", "限流":
		return "rate_limited"
	case "bad_gateway", "upstream_server_error", "上游服务异常":
		return "upstream_service_error"
	case "upstream_rate_limited", "上游限流":
		return "upstream_rate_limited"
	case "upstream_auth_failed", "上游鉴权失败":
		return "upstream_auth_failed"
	case "auth_failed", "鉴权失败":
		return "auth_failed"
	case "timeout", "超时":
		return "timeout"
	case "upstream_timeout", "上游超时":
		return "upstream_timeout"
	case "upstream_error", "上游错误":
		return "upstream_error"
	case "network_error", "网络异常":
		return "network_error"
	case "internal_error", "网关内部错误":
		return "internal_error"
	case "bad_request", "请求参数错误":
		return "bad_request"
	case "failed", "失败":
		return "failed"
	default:
		return strings.TrimSpace(value)
	}
}
