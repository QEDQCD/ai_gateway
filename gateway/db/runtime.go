package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const migrationAdvisoryLockKey int64 = 5504261723447799379

type SeedConfig struct {
	PlatformAPIKey      string
	ProviderBaseURL     string
	ProviderAPIKey      string
	Provider            string
	ProviderDisplayName string
	SecretCodec         *secret.Codec
}

type governanceSeedConfig struct {
	TenantID                 string
	AdminUserID              string
	AdminEmail               string
	AdminName                string
	MemberAUserID            string
	MemberAEmail             string
	MemberAName              string
	MemberBUserID            string
	MemberBEmail             string
	MemberBName              string
	PlatformAPIKeyID         string
	ApprovedApplicationID    string
	ApprovedApplicationEmail string
	ApprovedApplicationName  string
	RejectedApplicationID    string
	RejectedApplicationEmail string
	RejectedApplicationName  string
}

type seedDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func OpenPostgres(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, config)
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) (err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, migrationAdvisoryLockKey); unlockErr != nil && err == nil {
			err = fmt.Errorf("release migration advisory lock: %w", unlockErr)
		}
	}()

	if _, err := conn.Exec(ctx, `
		create table if not exists schema_migrations (
			name text primary key,
			applied_at timestamptz not null default now()
		);
	`); err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1);`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations (name) values ($1);`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}

func SeedDemoData(ctx context.Context, db seedDB, cfg SeedConfig) error {
	keyHash := hashPlatformAPIKey(cfg.PlatformAPIKey)
	if cfg.Provider == "" {
		cfg.Provider = "dashscope"
	}
	if cfg.ProviderDisplayName == "" {
		cfg.ProviderDisplayName = defaultSeedProviderDisplayName(cfg.Provider)
	}

	providerCredentialID := seedProviderCredentialID(cfg.Provider)
	chatModel, embeddingModel, supportedModels := seededModels(cfg.Provider)
	chatRouteID := service.RouteIDForCredential(providerCredentialID, supportedModels, chatModel)
	embeddingRouteID := service.RouteIDForCredential(providerCredentialID, supportedModels, embeddingModel)
	ragRouteID := service.RouteIDForCredential("provider_rag_service", []string{"rag-query"}, "rag-query")
	providerName := escapeLiteral(cfg.Provider)
	providerDisplayName := escapeLiteral(cfg.ProviderDisplayName)
	providerSecret, err := encryptSeedSecret(cfg.SecretCodec, cfg.ProviderAPIKey)
	if err != nil {
		return err
	}
	ragSecret, err := encryptSeedSecret(cfg.SecretCodec, "rag-internal")
	if err != nil {
		return err
	}

	statements := []string{
		`insert into tenants (id, name, status, request_quota_per_day) values
			('tenant_alpha', 'Alpha 租户', 'active', 800000),
			('tenant_beta', 'Beta 租户', 'active', 600000),
			('tenant_gamma', 'Gamma 租户', 'active', 400000)
		on conflict (id) do update set
			name = excluded.name,
			status = excluded.status,
			request_quota_per_day = excluded.request_quota_per_day;`,
		fmt.Sprintf(`insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, last_used_at) values
			('pak_live_console', 'tenant_alpha', 'prod-gateway', '%s', 'active', '{"chat","rag","embeddings"}', now()),
			('pak_batch_worker', 'tenant_beta', 'batch-worker', 'sha256:batch-worker', 'active', '{"embeddings"}', now() - interval '14 minutes')
		on conflict (id) do update set
			name = excluded.name,
			tenant_id = excluded.tenant_id,
			key_hash = excluded.key_hash,
			status = excluded.status,
			scopes = excluded.scopes,
			last_used_at = excluded.last_used_at;`, keyHash),
		fmt.Sprintf(`insert into provider_credentials (id, provider, display_name, encrypted_secret, status, supported_models, base_url) values
			('%s', '%s', '%s', '%s', 'active', '{%s}', '%s'),
			('provider_rag_service', 'rag', '知识库检索服务', '%s', 'active', '{"rag-query"}', 'http://rag-service:8000')
		on conflict (id) do update set
			provider = excluded.provider,
			display_name = excluded.display_name,
			encrypted_secret = excluded.encrypted_secret,
			status = excluded.status,
			supported_models = excluded.supported_models,
			base_url = excluded.base_url;`, providerCredentialID, providerName, providerDisplayName, escapeLiteral(providerSecret), joinArrayLiteral(supportedModels), escapeLiteral(cfg.ProviderBaseURL), escapeLiteral(ragSecret)),
		`insert into knowledge_bases (id, tenant_id, name, status, document_count, chunk_count, updated_at) values
			('kb_product_docs', 'tenant_alpha', '产品文档库', 'ready', 84, 8400, now() - interval '12 minutes'),
			('kb_support_archive', 'tenant_beta', '支持工单库', 'indexing', 62, 4000, now() - interval '28 minutes')
		on conflict (id) do update set
			tenant_id = excluded.tenant_id,
			name = excluded.name,
			status = excluded.status,
			document_count = excluded.document_count,
			chunk_count = excluded.chunk_count,
			updated_at = excluded.updated_at;`,
		`insert into documents (id, tenant_id, knowledge_base_id, name, content, status, chunk_count, updated_at) values
			('doc_product_overview', 'tenant_alpha', 'kb_product_docs', '产品概览', 'AI Gateway 提供统一模型路由、审计和知识库检索能力。', 'ready', 12, now() - interval '20 minutes'),
			('doc_rag_arch', 'tenant_alpha', 'kb_product_docs', 'RAG 架构说明', '查询先进入网关，再由 RAG 服务拉取知识片段并生成回答。', 'ready', 10, now() - interval '18 minutes'),
			('doc_ticket_flow', 'tenant_beta', 'kb_support_archive', '工单流转', '支持工单库正在索引中，当前用于客服问答。', 'indexing', 6, now() - interval '28 minutes')
		on conflict (id) do update set
			tenant_id = excluded.tenant_id,
			knowledge_base_id = excluded.knowledge_base_id,
			name = excluded.name,
			content = excluded.content,
			status = excluded.status,
			chunk_count = excluded.chunk_count,
			updated_at = excluded.updated_at;`,
		fmt.Sprintf(`insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at) values
			('%s', '%s', '%s', '%s', '/v1/chat/completions', 218, 'healthy', '聊天', now() - interval '2 minutes'),
			('%s', '%s', '%s', '%s', '/v1/embeddings', 64, 'healthy', '向量', now() - interval '3 minutes'),
			('%s', 'rag-query', '知识库检索服务', 'provider_rag_service', '/v1/rag/query', 312, 'warning', '知识库', now() - interval '5 minutes')
		on conflict (requested_model) do update set
			id = excluded.id,
			resolved_provider = excluded.resolved_provider,
			provider_credential_id = excluded.provider_credential_id,
			endpoint = excluded.endpoint,
			latency_ms = excluded.latency_ms,
			health_status = excluded.health_status,
			request_mode = excluded.request_mode,
			updated_at = excluded.updated_at;`, chatRouteID, escapeLiteral(chatModel), providerDisplayName, providerCredentialID, embeddingRouteID, escapeLiteral(embeddingModel), providerDisplayName, providerCredentialID, ragRouteID),
		fmt.Sprintf(`insert into audit_logs (tenant_id, platform_api_key_id, requested_model, endpoint, status_code, provider_display_name, latency_ms, created_at)
		select * from (values
			('tenant_alpha', 'pak_live_console', '%s', '/v1/chat/completions', 200, '%s', 218, now() - interval '3 minutes'),
			('tenant_beta', 'pak_batch_worker', 'rag-query', '/v1/rag/query', 200, '知识库检索服务', 312, now() - interval '6 minutes'),
			('tenant_gamma', 'pak_batch_worker', '%s', '/v1/embeddings', 429, '%s', 64, now() - interval '9 minutes')
		) as seed(tenant_id, platform_api_key_id, requested_model, endpoint, status_code, provider_display_name, latency_ms, created_at)
		where not exists (select 1 from audit_logs);`, escapeLiteral(chatModel), providerDisplayName, escapeLiteral(embeddingModel), providerDisplayName),
		fmt.Sprintf(`insert into operational_alerts (alert_type, scope, severity, created_at)
		select * from (values
			('quota_warning', 'tenant_beta', 'warning', now() - interval '8 minutes'),
			('route_fallback', '%s', 'warning', now() - interval '17 minutes'),
			('latency_spike', 'rag-service', 'warning', now() - interval '27 minutes')
		) as seed(alert_type, scope, severity, created_at)
		where not exists (select 1 from operational_alerts);`, escapeLiteral(chatModel)),
		fmt.Sprintf(`insert into playground_runs (tenant_id, platform_api_key_id, requested_model, prompt, response_excerpt, endpoint, resolved_provider, status_code, latency_ms, created_at)
		select * from (values
			('tenant_alpha', 'pak_live_console', '%s', '请介绍 AI Gateway', 'AI Gateway 已就绪，可统一路由聊天、向量和 RAG 请求。', '/v1/chat/completions', '%s', 200, 218, now() - interval '10 minutes')
		) as seed(tenant_id, platform_api_key_id, requested_model, prompt, response_excerpt, endpoint, resolved_provider, status_code, latency_ms, created_at)
		where not exists (select 1 from playground_runs);`, escapeLiteral(chatModel), providerDisplayName),
		`insert into system_settings (key, value) values
			('credential_mode', '平台 API Key 与上游凭据分离，支持 BYOK 扩展'),
			('fallback_policy', '已启用'),
			('routing_mode', '数据库模式'),
			('model_resolution_mode', '已启用'),
			('route_policy_description', '请求会先解析到托管凭据，再按路由策略回退。'),
			('knowledge_flow_title', '查询先进入网关，再路由到 RAG 服务拼装检索上下文。')
		on conflict (key) do update set value = excluded.value, updated_at = now();`,
	}
	statements = append(statements, governanceSeedStatements(governanceSeedConfig{
		TenantID:                 "tenant_alpha",
		AdminUserID:              "user_admin_alpha",
		AdminEmail:               "admin-alpha@example.com",
		AdminName:                "平台管理员 Alpha",
		MemberAUserID:            "user_member_alpha_a",
		MemberAEmail:             "member-alpha-a@example.com",
		MemberAName:              "租户用户 Alpha A",
		MemberBUserID:            "user_member_alpha_b",
		MemberBEmail:             "member-alpha-b@example.com",
		MemberBName:              "租户用户 Alpha B",
		PlatformAPIKeyID:         "pak_live_console",
		ApprovedApplicationID:    "app_alpha_approved",
		ApprovedApplicationEmail: "approved-alpha@example.com",
		ApprovedApplicationName:  "已审批用户 Alpha",
		RejectedApplicationID:    "app_alpha_rejected",
		RejectedApplicationEmail: "rejected-alpha@example.com",
		RejectedApplicationName:  "被拒绝用户 Alpha",
	})...)

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func escapeLiteral(value string) string {
	return strings.ReplaceAll(value, `'`, `''`)
}

func seededModels(provider string) (chatModel string, embeddingModel string, supportedModels []string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "dashscope":
		return "qwen-flash", "text-embedding-v4", []string{"qwen-flash", "text-embedding-v4"}
	default:
		return "gpt-4o-mini", "text-embedding-3-small", []string{"gpt-4o-mini", "text-embedding-3-small"}
	}
}

func defaultSeedProviderDisplayName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "dashscope":
		return "DashScope Primary"
	default:
		return "模型服务主路由"
	}
}

func seedProviderCredentialID(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = "provider"
	}

	var builder strings.Builder
	lastUnderscore := false
	for _, r := range provider {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	slug := strings.Trim(builder.String(), "_")
	if slug == "" {
		slug = "provider"
	}
	return "provider_" + slug + "_primary"
}

func encryptSeedSecret(codec *secret.Codec, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if codec == nil {
		return "", errors.New("provider secret codec is required for seeded provider credentials")
	}
	return codec.Encrypt(value)
}

func joinArrayLiteral(values []string) string {
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, `"`+escapeLiteral(value)+`"`)
	}
	return strings.Join(escaped, ",")
}

const (
	runtimeSeedProviderCredentialID = "provider_openai_demo"
	runtimeSeedProviderDisplayName  = "OpenAI Primary"
	runtimeSeedProviderBaseURL      = "https://demo-openai.example/v1"
	runtimeSeedEncryptedSecret      = ""
)

func runtimeSeedSupportedModels() []string {
	return []string{"gpt-4o-mini", "text-embedding-3-small"}
}

func runtimeSeedRouteID(requestedModel string) string {
	return service.RouteIDForCredential(runtimeSeedProviderCredentialID, runtimeSeedSupportedModels(), requestedModel)
}

func RuntimeSeedStatements() []string {
	chatRouteID := runtimeSeedRouteID("gpt-4o-mini")
	embeddingRouteID := runtimeSeedRouteID("text-embedding-3-small")

	statements := []string{
		`
		insert into tenants (id, name, status, created_at)
		values ('tenant_demo', 'Demo Tenant', 'active', timestamptz '2026-04-24T09:45:00Z')
		on conflict (id) do nothing;
		`,
		`
		insert into platform_api_keys (id, tenant_id, name, key_hash, status, created_at)
		values ('pak_demo', 'tenant_demo', 'demo key', 'sha256:demo', 'active', timestamptz '2026-04-24T09:46:00Z')
		on conflict (id) do nothing;
		`,
		`
		insert into provider_credentials (
			id,
			provider,
			display_name,
			encrypted_secret,
			status,
			created_at,
			supported_models,
			base_url
		)
		values (
			'` + runtimeSeedProviderCredentialID + `',
			'openai',
			'` + runtimeSeedProviderDisplayName + `',
			'` + runtimeSeedEncryptedSecret + `',
			'active',
			timestamptz '2026-04-24T09:47:00Z',
			'{"gpt-4o-mini","text-embedding-3-small"}',
			'` + runtimeSeedProviderBaseURL + `'
		)
		on conflict (id) do nothing;
		`,
		`
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		)
		values
			(
				'llmreq_demo_001',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'` + runtimeSeedProviderCredentialID + `',
				'` + chatRouteID + `',
				'/v1/chat/completions',
				'gpt-4o-mini',
				'gpt-4o-mini',
				'upstream',
				'success',
				200,
				182,
				124,
				48,
				172,
				'',
				'',
				timestamptz '2026-04-24T10:00:00Z',
				timestamptz '2026-04-24T10:00:00.182Z',
				timestamptz '2026-04-24T10:00:01Z'
			),
			(
				'llmreq_demo_002',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'` + runtimeSeedProviderCredentialID + `',
				'` + embeddingRouteID + `',
				'/v1/embeddings',
				'text-embedding-3-small',
				'text-embedding-3-small',
				'estimated',
				'rate_limited',
				429,
				95,
				16,
				0,
				16,
				'rate_limit',
				'demo rate limit response',
				timestamptz '2026-04-24T10:05:00Z',
				timestamptz '2026-04-24T10:05:00.095Z',
				timestamptz '2026-04-24T10:05:01Z'
			)
		on conflict (id) do nothing;
		`,
		`
		insert into route_catalog (
			id,
			requested_model,
			resolved_provider,
			provider_credential_id,
			endpoint,
			latency_ms,
			health_status,
			request_mode,
			updated_at
		)
		values
			(
				'` + chatRouteID + `',
				'gpt-4o-mini',
				'` + runtimeSeedProviderDisplayName + `',
				'` + runtimeSeedProviderCredentialID + `',
				'/v1/chat/completions',
				182,
				'healthy',
				'聊天',
				timestamptz '2026-04-24T10:00:00Z'
			),
			(
				'` + embeddingRouteID + `',
				'text-embedding-3-small',
				'` + runtimeSeedProviderDisplayName + `',
				'` + runtimeSeedProviderCredentialID + `',
				'/v1/embeddings',
				95,
				'warning',
				'向量',
				timestamptz '2026-04-24T10:05:00Z'
			)
		on conflict (id) do nothing;
		`,
		`
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		)
		values
			(
				'llmevt_demo_001',
				'llmreq_demo_001',
				'tenant_demo',
				'response_received',
				'upstream',
				'success',
				200,
				'demo upstream completion succeeded',
				timestamptz '2026-04-24T10:00:00.182Z'
			),
			(
				'llmevt_demo_002',
				'llmreq_demo_002',
				'tenant_demo',
				'request_failed',
				'estimated',
				'rate_limited',
				429,
				'demo estimated usage emitted after upstream rate limit',
				timestamptz '2026-04-24T10:05:00.095Z'
			)
		on conflict (id) do nothing;
		`,
		`
		insert into llm_usage_agg_hourly (
			bucket_start,
			tenant_id,
			platform_api_key_id,
			provider_credential_id,
			route_id,
			request_path,
			usage_source,
			usage_status,
			request_count,
			prompt_tokens,
			completion_tokens,
			total_tokens
		)
		values
			(
				timestamptz '2026-04-24T10:00:00Z',
				'tenant_demo',
				'pak_demo',
				'provider_openai_demo',
				'` + chatRouteID + `',
				'/v1/chat/completions',
				'upstream',
				'success',
				1,
				124,
				48,
				172
			),
			(
				timestamptz '2026-04-24T10:00:00Z',
				'tenant_demo',
				'pak_demo',
				'provider_openai_demo',
				'` + embeddingRouteID + `',
				'/v1/embeddings',
				'estimated',
				'rate_limited',
				1,
				16,
				0,
				16
			)
		on conflict do nothing;
		`,
	}
	statements = append(statements, governanceSeedStatements(governanceSeedConfig{
		TenantID:                 "tenant_demo",
		AdminUserID:              "user_admin_demo",
		AdminEmail:               "admin@example.com",
		AdminName:                "平台管理员",
		MemberAUserID:            "user_member_a",
		MemberAEmail:             "member-a@example.com",
		MemberAName:              "租户用户A",
		MemberBUserID:            "user_member_b",
		MemberBEmail:             "member-b@example.com",
		MemberBName:              "租户用户B",
		PlatformAPIKeyID:         "pak_demo",
		ApprovedApplicationID:    "app_demo_approved",
		ApprovedApplicationEmail: "approved@example.com",
		ApprovedApplicationName:  "已审批用户",
		RejectedApplicationID:    "app_demo_rejected",
		RejectedApplicationEmail: "rejected@example.com",
		RejectedApplicationName:  "被拒绝用户",
	})...)
	return statements
}

func governanceSeedStatements(cfg governanceSeedConfig) []string {
	return []string{
		fmt.Sprintf(`
		insert into users (id, email, name, role, status)
		values
			('%s', '%s', '%s', 'admin', 'active'),
			('%s', '%s', '%s', 'member', 'active'),
			('%s', '%s', '%s', 'member', 'active')
		on conflict (id) do update set
			email = excluded.email,
			name = excluded.name,
			role = excluded.role,
			status = excluded.status;
		`,
			cfg.AdminUserID, escapeLiteral(cfg.AdminEmail), escapeLiteral(cfg.AdminName),
			cfg.MemberAUserID, escapeLiteral(cfg.MemberAEmail), escapeLiteral(cfg.MemberAName),
			cfg.MemberBUserID, escapeLiteral(cfg.MemberBEmail), escapeLiteral(cfg.MemberBName),
		),
		fmt.Sprintf(`
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values
			('tm_%s_001', '%s', '%s', 'member', 'active'),
			('tm_%s_002', '%s', '%s', 'member', 'active')
		on conflict (id) do update set
			tenant_id = excluded.tenant_id,
			user_id = excluded.user_id,
			role = excluded.role,
			status = excluded.status;
		`,
			escapeLiteral(cfg.TenantID), escapeLiteral(cfg.TenantID), cfg.MemberAUserID,
			escapeLiteral(cfg.TenantID), escapeLiteral(cfg.TenantID), cfg.MemberBUserID,
		),
		fmt.Sprintf(`
		insert into account_applications (
			id,
			email,
			name,
			company_name,
			use_case,
			status,
			reviewer_id,
			review_comment,
			reviewed_at
		)
		values
			('%s', '%s', '%s', 'Demo Co', '内部知识问答', 'approved', '%s', 'seed approve', timestamptz '2026-04-24T09:44:00Z'),
			('%s', '%s', '%s', 'Demo Co', '压测脚本', 'rejected', '%s', 'seed reject', timestamptz '2026-04-24T09:43:00Z')
		on conflict (id) do update set
			email = excluded.email,
			name = excluded.name,
			company_name = excluded.company_name,
			use_case = excluded.use_case,
			status = excluded.status,
			reviewer_id = excluded.reviewer_id,
			review_comment = excluded.review_comment,
			reviewed_at = excluded.reviewed_at;
		`,
			cfg.ApprovedApplicationID, escapeLiteral(cfg.ApprovedApplicationEmail), escapeLiteral(cfg.ApprovedApplicationName), cfg.AdminUserID,
			cfg.RejectedApplicationID, escapeLiteral(cfg.RejectedApplicationEmail), escapeLiteral(cfg.RejectedApplicationName), cfg.AdminUserID,
		),
		fmt.Sprintf(`
		insert into audit_events (
			id,
			actor_type,
			actor_user_id,
			tenant_id,
			event_type,
			target_type,
			target_id,
			detail
		)
		values
			('audit_evt_%s_001', 'admin', '%s', '%s', 'application_approved', 'account_application', '%s', 'seed approve'),
			('audit_evt_%s_002', 'member', '%s', '%s', 'api_key_created', 'platform_api_key', '%s', 'seed key create'),
			('audit_evt_%s_003', 'system', null, '%s', 'quota_warning', 'tenant', '%s', 'seed quota warning')
		on conflict (id) do update set
			actor_type = excluded.actor_type,
			actor_user_id = excluded.actor_user_id,
			tenant_id = excluded.tenant_id,
			event_type = excluded.event_type,
			target_type = excluded.target_type,
			target_id = excluded.target_id,
			detail = excluded.detail;
		`,
			escapeLiteral(cfg.TenantID), cfg.AdminUserID, escapeLiteral(cfg.TenantID), cfg.ApprovedApplicationID,
			escapeLiteral(cfg.TenantID), cfg.MemberAUserID, escapeLiteral(cfg.TenantID), cfg.PlatformAPIKeyID,
			escapeLiteral(cfg.TenantID), escapeLiteral(cfg.TenantID), escapeLiteral(cfg.TenantID),
		),
	}
}
