# SEO/GEO 与智能模型路由 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为公开页补齐最小可上线的 SEO/GEO 能力，并为 `POST /v1/chat/completions` 增加规则驱动的智能模型路由与可观测记录。

**Architecture:** 公开页部分保持 SPA 架构不变，只补站点级静态抓取资产、页面级 head 元信息和可抽取语义文案。智能路由部分不引入新服务，而是在现有 `AuthService.Resolve -> RouteService.Resolve -> ChatProxyService -> UsageRecorder -> ConsoleService` 链路上扩展“规则分类 -> 目标模型档位 -> 路由原因记录”的最小能力，并把熔断/降级只留在文档与配置接口层面，不在本轮执行。

**Tech Stack:** Go, PostgreSQL, Fiber, React, TypeScript, Vitest, existing usage/audit pipeline, Vite static assets

---

## 文件边界

### 后端配置与路由分类

- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Create: `gateway/internal/service/smart_routing.go`
- Create: `gateway/internal/service/smart_routing_test.go`
- Modify: `gateway/internal/domain/routing.go`
- Modify: `gateway/internal/service/auth_service.go`
- Modify: `gateway/internal/service/auth_service_test.go`

### 请求链路与使用记录

- Create: `gateway/db/migrations/0014_add_smart_routing_fields.sql`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`

### HTTP 与集成测试

- Modify: `gateway/internal/http/handlers/chat.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `gateway/tests/integration/proxy_test.go`

### 前端 SEO/GEO 与控制台展示

- Create: `web/public/robots.txt`
- Create: `web/public/sitemap.xml`
- Create: `web/public/llms.txt`
- Create: `web/src/lib/page-meta.ts`
- Modify: `web/index.html`
- Modify: `web/src/pages/login.tsx`
- Modify: `web/src/pages/application-form.tsx`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/pages/member-usage.tsx`
- Modify: `web/src/test/router.test.tsx`

### 文档

- Modify: `README.md`
- Modify: `docs/specs/2026-04-30-seo-geo-and-smart-routing-design.md`

## Task 1: 增加智能路由配置与规则分类器

**Files:**
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Create: `gateway/internal/service/smart_routing.go`
- Create: `gateway/internal/service/smart_routing_test.go`

- [ ] **Step 1: 先写规则分类器红灯测试**

```go
package service_test

import (
	"testing"

	"github.com/example/ai_gateway/gateway/internal/service"
)

func TestRuleBasedSmartRouterClassifiesComplexCodingPrompt(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords: []string{
			"写代码",
			"debug",
			"报错",
			"重构",
		},
		LongPromptThreshold: 240,
		EnableCodeFenceRule: true,
		EnableStackTraceRule: true,
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role: "user",
				Content: "请帮我 debug 下面 Go 代码为什么 panic:\n```go\nfunc main(){ panic(\"x\") }\n```",
			},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
	if len(result.MatchedRules) == 0 {
		t.Fatal("expected matched rules to be recorded")
	}
}

func TestRuleBasedSmartRouterFallsBackToFastModelForSimpleQuestion(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"写代码", "debug", "报错"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请用一句话解释什么是 API Gateway。",
			},
		},
	})

	if result.TaskClass != "simple_qa" {
		t.Fatalf("expected task class %q, got %q", "simple_qa", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-fast" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-fast", result.TargetModelTier)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/service -run 'TestRuleBasedSmartRouter(ClassifiesComplexCodingPrompt|FallsBackToFastModelForSimpleQuestion)' -v`

Expected: FAIL，提示 `NewRuleBasedSmartRouter`、`SmartRoutingConfig`、`Decide` 未定义。

- [ ] **Step 3: 实现最小规则分类器与配置结构**

```go
package service

import (
	"strings"
)

type SmartRoutingConfig struct {
	FastModelTier       string
	ReasoningModelTier  string
	CodingKeywords      []string
	LongPromptThreshold int
	EnableCodeFenceRule bool
	EnableStackTraceRule bool
}

type SmartRoutingDecision struct {
	TaskClass       string
	TargetModelTier string
	MatchedRules    []string
}

type SmartRouter interface {
	Decide(req ChatRequest) SmartRoutingDecision
}

type ruleBasedSmartRouter struct {
	cfg SmartRoutingConfig
}

func NewRuleBasedSmartRouter(cfg SmartRoutingConfig) SmartRouter {
	if strings.TrimSpace(cfg.FastModelTier) == "" {
		cfg.FastModelTier = "gateway-chat-fast"
	}
	if strings.TrimSpace(cfg.ReasoningModelTier) == "" {
		cfg.ReasoningModelTier = "gateway-chat-reasoning"
	}
	if cfg.LongPromptThreshold <= 0 {
		cfg.LongPromptThreshold = 240
	}
	return ruleBasedSmartRouter{cfg: cfg}
}

func (r ruleBasedSmartRouter) Decide(req ChatRequest) SmartRoutingDecision {
	content := aggregateChatMessages(req.Messages)
	normalized := strings.ToLower(content)
	matched := make([]string, 0, 4)

	for _, keyword := range r.cfg.CodingKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" && strings.Contains(normalized, strings.ToLower(keyword)) {
			matched = append(matched, "keyword:"+keyword)
		}
	}
	if r.cfg.EnableCodeFenceRule && strings.Contains(content, "```") {
		matched = append(matched, "pattern:code_fence")
	}
	if r.cfg.EnableStackTraceRule {
		if strings.Contains(content, "panic:") || strings.Contains(content, "Traceback") || strings.Contains(content, "Exception") {
			matched = append(matched, "pattern:stack_trace")
		}
	}

	if len(content) >= r.cfg.LongPromptThreshold && len(matched) > 0 {
		matched = append(matched, "signal:long_prompt")
	}

	if len(matched) > 0 {
		return SmartRoutingDecision{
			TaskClass:       "coding_complex",
			TargetModelTier: r.cfg.ReasoningModelTier,
			MatchedRules:    matched,
		}
	}

	return SmartRoutingDecision{
		TaskClass:       "simple_qa",
		TargetModelTier: r.cfg.FastModelTier,
		MatchedRules:    nil,
	}
}

func aggregateChatMessages(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 4: 补配置加载与默认值**

```go
type Config struct {
	// ...
	ChatFastModel             string
	ChatReasoningModel        string
	SmartRoutingCodingKeywords []string
	SmartRoutingLongPromptThreshold int
}

func Load() Config {
	// ...
	return Config{
		// ...
		ChatFastModel:                    defaultString(os.Getenv("GATEWAY_CHAT_FAST_MODEL"), "qwen-flash"),
		ChatReasoningModel:               defaultString(os.Getenv("GATEWAY_CHAT_REASONING_MODEL"), "qwen-plus"),
		SmartRoutingCodingKeywords:       splitCommaSeparatedEnv(defaultString(os.Getenv("GATEWAY_SMART_ROUTING_CODING_KEYWORDS"), "写代码,实现,重构,debug,报错,异常,单元测试,架构设计")),
		SmartRoutingLongPromptThreshold:  int(lookupInt64Env("GATEWAY_SMART_ROUTING_LONG_PROMPT_THRESHOLD", 240)),
	}
}
```

- [ ] **Step 5: 跑测试到绿**

Run: `go test ./gateway/internal/config ./gateway/internal/service -run 'TestRuleBasedSmartRouter(ClassifiesComplexCodingPrompt|FallsBackToFastModelForSimpleQuestion)|TestLoad' -v`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/config/config.go gateway/internal/config/config_test.go gateway/internal/service/smart_routing.go gateway/internal/service/smart_routing_test.go
git commit -m "feat: add rule based smart routing"
```

## Task 2: 把智能路由决策接入鉴权与请求上下文

**Files:**
- Modify: `gateway/internal/domain/routing.go`
- Modify: `gateway/internal/service/auth_service.go`
- Modify: `gateway/internal/service/auth_service_test.go`
- Modify: `gateway/internal/http/handlers/chat.go`

- [ ] **Step 1: 先写 AuthService 红灯测试**

```go
func TestAuthServiceResolveAppliesTargetModelTierFromDecision(t *testing.T) {
	repository := newStubAuthRepository()
	routeService := &capturingRouteService{
		delegate: service.NewRouteService(repository),
	}
	authService := service.NewAuthServiceWithConsoleSessions(repository, service.NewAllowAllQuotaGuard(), routeService, "secret")

	requestCtx, err := authService.Resolve(context.Background(), "platform-live-key", "gateway-chat-reasoning")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if routeService.requestedModel != "gateway-chat-reasoning" {
		t.Fatalf("expected requested model %q, got %q", "gateway-chat-reasoning", routeService.requestedModel)
	}
	if requestCtx.RouteID == "" {
		t.Fatal("expected route id to be populated")
	}
}
```

- [ ] **Step 2: 运行测试确认当前行为不可承载新上下文**

Run: `go test ./gateway/internal/service -run TestAuthServiceResolveAppliesTargetModelTierFromDecision -v`

Expected: FAIL，至少缺少智能路由上下文字段或测试辅助对象。

- [ ] **Step 3: 扩展 RequestContext 保存智能路由结果**

```go
type RequestContext struct {
	TenantID             string
	UserID               string
	PlatformAPIKeyID     string
	PlatformAPIKeyName   string
	SelectedProviderID   string
	SelectedProviderName string
	RouteID              string
	ProviderTarget       ProviderTarget
	TaskClass            string
	TargetModelTier      string
	RoutingReason        string
	RequestedModel       string
	ResolvedModel        string
}
```

- [ ] **Step 4: 在 chat handler 里执行规则分类并把结果写入 Locals**

```go
type ChatRoutingDecision struct {
	TaskClass       string
	TargetModelTier string
	RoutingReason   string
}

const chatRoutingDecisionLocalKey = "chatRoutingDecision"

func ChatCompletion(proxy service.ChatProxyService, router service.SmartRouter, authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.ChatRequest
		if err := c.BodyParser(&req); err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), fiber.StatusBadRequest)
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		decision := router.Decide(req)
		c.Locals(chatRoutingDecisionLocalKey, ChatRoutingDecision{
			TaskClass:       decision.TaskClass,
			TargetModelTier: decision.TargetModelTier,
			RoutingReason:   strings.Join(decision.MatchedRules, ","),
		})

		raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		resolved, err := authService.Resolve(c.UserContext(), raw, decision.TargetModelTier)
		if err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), fiber.StatusUnauthorized)
			return proxyError(err)
		}
		resolved.RequestedModel = strings.TrimSpace(req.Model)
		resolved.TaskClass = decision.TaskClass
		resolved.TargetModelTier = decision.TargetModelTier
		resolved.RoutingReason = strings.Join(decision.MatchedRules, ",")
		resolved.ResolvedModel = decision.TargetModelTier
		c.Locals("requestContext", resolved)

		if req.Stream {
			// existing stream path
		}
		resp, err := proxy.Complete(c.UserContext(), req, resolved)
		if err != nil {
			return proxyError(err)
		}
		return c.JSON(resp)
	}
}
```

- [ ] **Step 5: 保持 middleware 对 embeddings / rag 不变**

```go
func RequirePlatformAPIKey(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		requestCtx := scopedRequestContext(c)
		ctx, err := authService.Resolve(requestCtx, raw, requestedModel(c))
		if err != nil {
			return authError(err)
		}
		c.Locals(requestContextLocalKey, ctx)
		return c.Next()
	}
}
```

这里只改 `chat.go` 和 `router.go` 的接线，不去改变 embeddings / rag 认证中间件。

- [ ] **Step 6: 跑服务层与路由层测试**

Run: `go test ./gateway/internal/service ./gateway/internal/http -run 'TestAuthServiceResolveAppliesTargetModelTierFromDecision|TestChatCompletion' -v`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add gateway/internal/domain/routing.go gateway/internal/service/auth_service.go gateway/internal/service/auth_service_test.go gateway/internal/http/handlers/chat.go gateway/internal/http/router.go gateway/internal/http/router_test.go
git commit -m "feat: apply smart routing before chat proxy"
```

## Task 3: 为请求日志与控制台查询增加智能路由字段

**Files:**
- Create: `gateway/db/migrations/0014_add_smart_routing_fields.sql`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`

- [ ] **Step 1: 先写 migration 红灯测试**

```go
func TestApplyMigrationsAddsSmartRoutingColumns(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}

	assertColumnExists(t, ctx, pool, "llm_request_logs", "task_class")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "routing_reason")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "target_model_tier")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "resolved_model")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/db -run TestApplyMigrationsAddsSmartRoutingColumns -v`

Expected: FAIL，提示新列不存在。

- [ ] **Step 3: 写 migration**

```sql
alter table llm_request_logs
  add column task_class text not null default '',
  add column routing_reason text not null default '',
  add column target_model_tier text not null default '',
  add column resolved_model text not null default '';
```

- [ ] **Step 4: 扩 UsageRecord 与 insert SQL**

```go
type UsageRecord struct {
	// ...
	TaskClass       string
	RoutingReason   string
	TargetModelTier string
	ResolvedModel   string
}

const insertUsageRecordSQL = `
insert into llm_request_logs (
	-- existing columns,
	task_class,
	routing_reason,
	target_model_tier,
	resolved_model
) values (
	-- existing params,
	$30, $31, $32, $33
)`
```

- [ ] **Step 5: 从 RequestContext 把智能路由字段带进 UsageRecord**

```go
func NewChatUsageRecord(
	requestID string,
	requestContext domain.RequestContext,
	req ChatRequest,
	resp ChatResponse,
	statusCode int,
	start time.Time,
	end time.Time,
	err error,
) UsageRecord {
	record := UsageRecord{
		RequestID:            requestID,
		TenantID:             requestContext.TenantID,
		UserID:               requestContext.UserID,
		PlatformAPIKeyID:     requestContext.PlatformAPIKeyID,
		PlatformAPIKeyName:   requestContext.PlatformAPIKeyName,
		ProviderCredentialID: requestContext.SelectedProviderID,
		Provider:             requestContext.ProviderTarget.Provider,
		RouteID:              requestContext.RouteID,
		RequestPath:          "/v1/chat/completions",
		RequestModel:         firstNonEmpty(requestContext.RequestedModel, req.Model),
		UpstreamModel:        firstNonEmpty(resp.Model, requestContext.ResolvedModel),
		TaskClass:            requestContext.TaskClass,
		RoutingReason:        requestContext.RoutingReason,
		TargetModelTier:      requestContext.TargetModelTier,
		ResolvedModel:        firstNonEmpty(requestContext.ResolvedModel, resp.Model),
		// remaining fields...
	}
	return record
}
```

- [ ] **Step 6: 扩控制台 DTO 与查询**

```go
type AuditItem struct {
	// ...
	TaskClass       string `json:"task_class"`
	RoutingReason   string `json:"routing_reason"`
	TargetModelTier string `json:"target_model_tier"`
	ResolvedModel   string `json:"resolved_model"`
}

type UsageRequestItem struct {
	// ...
	TaskClass       string `json:"task_class"`
	RoutingReason   string `json:"routing_reason"`
	TargetModelTier string `json:"target_model_tier"`
	ResolvedModel   string `json:"resolved_model"`
}
```

并在 `postgres_console_service.go`、`postgres_member_console_service.go` 的查询里把这四个字段读出。

- [ ] **Step 7: 跑数据库与服务测试**

Run: `go test ./gateway/db ./gateway/internal/service -run 'TestApplyMigrationsAddsSmartRoutingColumns|TestUsageRecorder|TestPostgresConsoleService|TestPostgresMemberConsoleService' -v`

Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add gateway/db/migrations/0014_add_smart_routing_fields.sql gateway/internal/service/usage_recording.go gateway/internal/service/usage_recording_test.go gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/service/postgres_member_console_service.go
git commit -m "feat: persist smart routing audit fields"
```

## Task 4: 完成 chat 路由接线与集成测试

**Files:**
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `gateway/tests/integration/proxy_test.go`
- Modify if needed: `gateway/cmd/server/main.go`

- [ ] **Step 1: 先写路由层红灯测试**

```go
func TestChatCompletionRouteUsesSmartRoutingReasoningTier(t *testing.T) {
	t.Parallel()

	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"debug", "panic"},
		EnableCodeFenceRule: true,
	})

	var capturedRequestedModel string
	authService := stubAuthService{
		resolveFunc: func(ctx context.Context, rawKey string, requestedModel string) (domain.RequestContext, error) {
			capturedRequestedModel = requestedModel
			return domain.RequestContext{
				TenantID:           "tenant_demo",
				PlatformAPIKeyID:   "pak_demo",
				PlatformAPIKeyName: "demo",
				SelectedProviderID: "provider_demo",
				RouteID:            "route_demo",
				ProviderTarget: domain.ProviderTarget{
					CredentialID: "provider_demo",
					Provider:     "openai",
					BaseURL:      "https://provider.example/v1",
					APIKey:       "provider-secret",
				},
			}, nil
		},
	}

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: authService,
		ChatProxy:   newCapturingChatProxy(),
		SmartRouter: router,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"qwen-flash","messages":[{"role":"user","content":"please debug this panic ```go\npanic(\"x\")\n```"}]}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedRequestedModel != "gateway-chat-reasoning" {
		t.Fatalf("expected requested model %q, got %q", "gateway-chat-reasoning", capturedRequestedModel)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/http -run TestChatCompletionRouteUsesSmartRoutingReasoningTier -v`

Expected: FAIL，提示 `RouterDependencies` 缺少 `SmartRouter` 或 chat route 仍未使用规则路由。

- [ ] **Step 3: 更新依赖注入与 router 接线**

```go
type RouterDependencies struct {
	// ...
	SmartRouter service.SmartRouter
}

func NewRouterWithDependencies(deps RouterDependencies) *fiber.App {
	// ...
	v1.Post("/chat/completions", handlers.ChatCompletion(deps.ChatProxy, deps.SmartRouter, deps.AuthService))
	// embeddings 保持原样，仍使用 RequirePlatformAPIKey 中间件
}
```

- [ ] **Step 4: 在 main.go 注入真实规则路由器**

```go
smartRouter := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
	FastModelTier:              cfg.ChatFastModel,
	ReasoningModelTier:         cfg.ChatReasoningModel,
	CodingKeywords:             cfg.SmartRoutingCodingKeywords,
	LongPromptThreshold:        cfg.SmartRoutingLongPromptThreshold,
	EnableCodeFenceRule:        true,
	EnableStackTraceRule:       true,
})

app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
	// existing deps...
	SmartRouter: smartRouter,
})
```

- [ ] **Step 5: 扩 integration test 断言真实上游模型**

```go
func TestChatProxyRoutesComplexCodingPromptToReasoningModel(t *testing.T) {
	var upstreamModel string
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload failed: %v", err)
		}
		upstreamModel, _ = payload["model"].(string)
		_, _ = w.Write([]byte(`{"model":"qwen-plus","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	}))
	t.Cleanup(providerServer.Close)

	app, _ := newGatewayAppWithSmartRouting(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"qwen-flash","messages":[{"role":"user","content":"请帮我 debug 这段 panic 代码 ```go\npanic(\"x\")\n```"}]}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if upstreamModel != "qwen-plus" {
		t.Fatalf("expected upstream model %q, got %q", "qwen-plus", upstreamModel)
	}
}
```

- [ ] **Step 6: 跑 HTTP 与集成测试**

Run: `go test ./gateway/internal/http ./gateway/tests/integration -run 'TestChatCompletionRouteUsesSmartRoutingReasoningTier|TestChatProxyRoutesComplexCodingPromptToReasoningModel' -v`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add gateway/internal/http/router.go gateway/internal/http/router_test.go gateway/tests/integration/proxy_test.go gateway/cmd/server/main.go
git commit -m "feat: wire smart routing into chat gateway"
```

## Task 5: 增加公开页 SEO/GEO 资产与页面元信息

**Files:**
- Create: `web/public/robots.txt`
- Create: `web/public/sitemap.xml`
- Create: `web/public/llms.txt`
- Create: `web/src/lib/page-meta.ts`
- Modify: `web/index.html`
- Modify: `web/src/pages/login.tsx`
- Modify: `web/src/pages/application-form.tsx`

- [ ] **Step 1: 先写公开页测试红灯**

```tsx
test("未登录时登录页写入页面标题与公开摘要", async () => {
  mockAnonymousSession();

  render(
    <RouterProvider router={createTestRouter(["/login"])} future={{ v7_startTransition: true }} />,
  );

  expect(await screen.findByRole("heading", { level: 1, name: "登录 AI Gateway 控制台" })).toBeInTheDocument();
  expect(document.title).toContain("AI Gateway");
  expect(screen.getByText(/统一管理平台 API Key、调用日志、失败记录与租户级 Token 消耗/)).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx -t '未登录时登录页写入页面标题与公开摘要'`

Expected: FAIL，`document.title` 仍是默认值或缺少公开摘要断言目标。

- [ ] **Step 3: 新增页面级元信息 hook**

```ts
import { useEffect } from "react";

type PageMetaInput = {
  title: string;
  description: string;
  canonicalPath: string;
};

function upsertMeta(name: string, content: string) {
  let element = document.head.querySelector(`meta[name="${name}"]`) as HTMLMetaElement | null;
  if (!element) {
    element = document.createElement("meta");
    element.setAttribute("name", name);
    document.head.appendChild(element);
  }
  element.setAttribute("content", content);
}

function upsertCanonical(href: string) {
  let element = document.head.querySelector(`link[rel="canonical"]`) as HTMLLinkElement | null;
  if (!element) {
    element = document.createElement("link");
    element.setAttribute("rel", "canonical");
    document.head.appendChild(element);
  }
  element.setAttribute("href", href);
}

export function usePageMeta(input: PageMetaInput) {
  useEffect(() => {
    document.title = input.title;
    upsertMeta("description", input.description);
    upsertMeta("robots", "index,follow");
    upsertCanonical(input.canonicalPath);
  }, [input]);
}
```

- [ ] **Step 4: 更新公开页与 index.html**

```html
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>AI Gateway</title>
    <meta
      name="description"
      content="AI Gateway 是一个面向企业与团队的统一 AI 接入平台，提供平台 API Key 分发、租户治理、调用审计与智能模型路由能力。"
    />
    <meta property="og:type" content="website" />
    <meta property="og:title" content="AI Gateway" />
    <meta
      property="og:description"
      content="统一管理平台 API Key、调用日志、失败记录与租户级 Token 消耗。"
    />
  </head>
```

并在 `login.tsx`、`application-form.tsx` 顶部调用：

```tsx
usePageMeta({
  title: "登录 AI Gateway 控制台",
  description: "登录 AI Gateway 控制台，统一管理平台 API Key、调用观测、审计与租户治理。",
  canonicalPath: "/login",
})
```

- [ ] **Step 5: 新增静态抓取资产**

```txt
User-agent: *
Allow: /login
Allow: /apply
Disallow: /api/
Disallow: /me
Disallow: /usage
Disallow: /audit
```

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/login</loc></url>
  <url><loc>https://example.com/apply</loc></url>
</urlset>
```

```txt
# AI Gateway

AI Gateway 是一个面向企业与团队的统一 AI 接入平台。

## 核心能力
- 平台 API Key 分发
- 租户治理与审批
- 调用审计与 Token 观测
- 规则驱动的智能模型路由

## 公开入口
- /login
- /apply

平台对外不暴露真实上游模型凭据。
```

- [ ] **Step 6: 跑前端测试与构建**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx && npm --prefix web run build`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add web/public/robots.txt web/public/sitemap.xml web/public/llms.txt web/src/lib/page-meta.ts web/index.html web/src/pages/login.tsx web/src/pages/application-form.tsx web/src/test/router.test.tsx
git commit -m "feat: add public seo geo assets"
```

## Task 6: 在控制台展示智能路由字段

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/pages/member-usage.tsx`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端联动红灯测试**

```tsx
test("调用观测页展示智能路由分类与路由原因", async () => {
  mockFetch({
    "/api/admin/usage/overview": createUsageOverviewMock(),
    "/api/admin/usage/trends": { requests: [], tokens: [], success: [], costs: [] },
    "/api/admin/usage/latency-wall?window=24h": { window_label: "最近 24 小时", buckets: [], lanes: [] },
    "/api/admin/usage/failures": { breakdown: [], recent_events: [] },
    "/api/admin/usage/requests?limit=20&offset=0": {
      items: [
        createUsageRequestMock({
          task_class: "coding_complex",
          target_model_tier: "gateway-chat-reasoning",
          routing_reason: "keyword:debug,pattern:code_fence",
          resolved_model: "qwen-plus",
        }),
      ],
      total: 1,
      limit: 20,
      offset: 0,
    },
  });

  renderRoute("/usage");

  expect(await screen.findByText("coding_complex")).toBeInTheDocument();
  expect(screen.getByText("gateway-chat-reasoning")).toBeInTheDocument();
  expect(screen.getByText("keyword:debug,pattern:code_fence")).toBeInTheDocument();
  expect(screen.getByText("qwen-plus")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx -t '调用观测页展示智能路由分类与路由原因'`

Expected: FAIL，提示字段未解析或页面未展示。

- [ ] **Step 3: 扩前端类型与解析**

```ts
export type AuditItem = {
  // existing fields...
  task_class: string;
  routing_reason: string;
  target_model_tier: string;
  resolved_model: string;
};

export type UsageRequestItem = {
  // existing fields...
  task_class: string;
  routing_reason: string;
  target_model_tier: string;
  resolved_model: string;
};
```

并在 `toUsageRequestItem` 与 `getAudit` 映射中读取同名字段。

- [ ] **Step 4: 更新页面表格列**

```tsx
columns={[
  "请求 ID",
  "任务分类",
  "目标档位",
  "路由原因",
  "实际模型",
  // existing columns...
]}
```

member 页面可只显示：

```tsx
columns={[
  "请求 ID",
  "任务类型",
  "平台策略",
  "实际模型",
  // remaining safe columns...
]}
```

但注意不要回退此前“隐藏真实 provider”的边界；这里显示的是平台模型档位和实际模型名，不是 provider credential。

- [ ] **Step 5: 跑前端测试**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/lib/console-api.ts web/src/pages/usage.tsx web/src/pages/audit.tsx web/src/pages/member-usage.tsx web/src/test/router.test.tsx
git commit -m "feat: show smart routing decisions in console"
```

## Task 7: 文档与端到端验证收尾

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-04-30-seo-geo-and-smart-routing-design.md`

- [ ] **Step 1: 更新 README 的配置与验证说明**

````md
## 智能路由配置

后端支持以下环境变量：

```env
GATEWAY_CHAT_FAST_MODEL=qwen-flash
GATEWAY_CHAT_REASONING_MODEL=qwen-plus
GATEWAY_SMART_ROUTING_CODING_KEYWORDS=写代码,实现,重构,debug,报错,异常,单元测试,架构设计
GATEWAY_SMART_ROUTING_LONG_PROMPT_THRESHOLD=240
```

## 公开页 SEO/GEO

公开页会输出：

- `/robots.txt`
- `/sitemap.xml`
- `/llms.txt`

## 智能路由验证

```bash
curl -sS http://127.0.0.1:32658/v1/chat/completions \
  -H "Authorization: Bearer <platform-key>" \
  -H "Content-Type: application/json" \
  --data '{"model":"qwen-flash","messages":[{"role":"user","content":"请帮我 debug 这段 panic 代码 ```go\npanic(\"x\")\n```"}]}'
```

期望：

- 请求被路由到强模型
- 审计和调用观测中能看到 `coding_complex`
````

- [ ] **Step 2: 跑全量测试**

Run: `./scripts/test.sh`

Expected: PASS

- [ ] **Step 3: 跑 lint 与 compose 配置校验**

Run: `./scripts/lint.sh && docker compose --env-file deploy/compose/.env.example -f deploy/compose/compose.yml config >/tmp/compose-smart-routing.out && tail -n 20 /tmp/compose-smart-routing.out`

Expected: PASS，且 compose 输出中包含智能路由相关 env 透传。

- [ ] **Step 4: 本地拉起并手工验证**

Run: `docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build`

Then verify:

```bash
curl -I http://127.0.0.1:31873/login
curl http://127.0.0.1:31873/robots.txt
curl http://127.0.0.1:31873/sitemap.xml
curl http://127.0.0.1:31873/llms.txt
curl http://127.0.0.1:32658/healthz
```

Expected:

- 公开页可访问
- 静态抓取资产存在
- 网关健康检查成功

- [ ] **Step 5: 提交**

```bash
git add README.md docs/specs/2026-04-30-seo-geo-and-smart-routing-design.md
git commit -m "docs: document seo geo smart routing rollout"
```

## 自检结论

- spec coverage: 已覆盖公开页 SEO/GEO、规则智能路由、可观测字段、前端展示、验证与文档。
- placeholder scan: 无 `TODO`、`TBD`、`implement later` 等占位描述。
- type consistency: 统一使用 `task_class`、`routing_reason`、`target_model_tier`、`resolved_model` 这组字段名；路由目标档位统一使用 `gateway-chat-fast` 与 `gateway-chat-reasoning`。
