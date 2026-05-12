# FAQ Semantic Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `POST /v1/chat/completions` 的非流式请求实现“`qwen-mt-flash` 判定 + 平台内置 FAQ 白名单缓存”能力，命中后直接返回标准答案，不再调用后端主 LLM，同时补齐缓存命中观测字段与测试。

**Architecture:** 在现有 `ChatProxyService` 主链路前插入一个轻量 FAQ 命中编排层：先抽取最后一条用户消息，调用 `qwen-mt-flash` 进行固定问答意图识别，再按 `platform_api_key_id + faq_key + version` 查 Redis。命中则用兼容 OpenAI chat completion 的响应直接返回，未命中或判定异常则回退原上游调用；调用日志新增 `cache_hit / cache_faq_key / classifier_status` 等字段，并通过现有 `UsageRecorder` / `llm_request_events` 统一入库。

**Tech Stack:** Go, Fiber, PostgreSQL, Redis, pgx, go-redis/v9, 现有 `ChatProxyService`, `ContentGuardService`, `UsageRecorder`, `RouteService`, `cmd/server` 配置装配

---

## 文件结构与职责映射

### 新增文件

- `gateway/db/migrations/0022_add_faq_semantic_cache_fields.sql`
  - 为 `llm_request_logs` 增加缓存相关观测字段
- `gateway/internal/service/faq_registry.go`
  - 平台内置 FAQ 注册表与 key/answer/version 定义
- `gateway/internal/service/faq_classifier.go`
  - `qwen-mt-flash` 判定接口、结果结构、结果解析
- `gateway/internal/service/faq_cache_service.go`
  - Redis key 生成、读写、TTL、命中计数
- `gateway/internal/service/faq_semantic_cache.go`
  - FAQ 语义缓存总编排：抽取问题、调用 classifier、查 cache、构造命中响应
- `gateway/internal/service/faq_registry_test.go`
- `gateway/internal/service/faq_classifier_test.go`
- `gateway/internal/service/faq_cache_service_test.go`
- `gateway/internal/service/faq_semantic_cache_test.go`

### 修改文件

- `gateway/internal/config/config.go`
  - 新增开关、超时、阈值、TTL、判定模型配置
- `gateway/cmd/server/main.go`
  - Redis/FAQ classifier 装配，注入 `ChatProxyService`
- `gateway/internal/provider/openai_client.go`
  - 复用现有 openai 兼容调用能力为 classifier 提供客户端支撑（若现有接口不足则补小扩展）
- `gateway/internal/service/proxy_service.go`
  - 在非流式 `Complete()` 路径插入 FAQ 语义缓存逻辑
- `gateway/internal/service/usage_recording.go`
  - 请求日志新增 cache/classifier 字段持久化
- `gateway/internal/service/usage_recording_test.go`
  - 增加缓存命中字段断言
- `gateway/internal/service/usage_types.go`
  - 视需要增加 classifier/cache 相关 event/status 常量
- `gateway/tests/integration/proxy_test.go`
  - 增加 FAQ 命中、未命中、stream 绕过等端到端场景
- `gateway/cmd/server/main_test.go`
  - 增加配置启用后的主流程验证
- `docs/specs/2026-05-12-faq-semantic-cache-design.md`
  - 如实现期间发现字段名/阈值需微调，回写文档

---

### Task 1: 为请求日志补齐 FAQ 缓存观测字段

**Files:**
- Create: `gateway/db/migrations/0022_add_faq_semantic_cache_fields.sql`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/db/runtime.go`

- [ ] **Step 1: 写失败测试，先锁定新增列与 insert SQL**

在 `gateway/internal/service/usage_recording_test.go` 增加一个失败测试，目标是验证 `UsageRecorder` 写入 `llm_request_logs` 时包含新增字段：

```go
func TestUsageRecorderStoresFAQSemanticCacheFields(t *testing.T) {
    db := newRecordingExecDB()
    recorder := NewUsageRecorder(db, stubPricingResolver{})

    record := UsageRecord{
        RequestID: "req_cache_hit_001",
        TenantID: "tenant_alpha",
        PlatformAPIKeyID: "pak_live_console",
        RequestPath: "/v1/chat/completions",
        RequestedModel: "qwen-flash",
        ResolvedModel: "qwen-flash",
        StatusCode: 200,
        StartedAt: time.Unix(100, 0).UTC(),
        FinishedAt: time.Unix(101, 0).UTC(),
        CacheHit: true,
        CacheType: "faq_semantic",
        CacheKey: "faq_cache:pak_live_console:faq.identity.who_are_you:v1",
        CacheFAQKey: "faq.identity.who_are_you",
        ClassifierModel: "qwen-mt-flash",
        ClassifierStatus: "hit",
        ClassifierLatencyMS: 87,
    }

    if err := recorder.Record(context.Background(), record); err != nil {
        t.Fatalf("Record returned error: %v", err)
    }

    query := db.execCalls[0].query
    for _, needle := range []string{"cache_hit", "cache_type", "cache_key", "cache_faq_key", "classifier_model", "classifier_status", "classifier_latency_ms"} {
        if !strings.Contains(query, needle) {
            t.Fatalf("expected insert query to contain %q, got %q", needle, query)
        }
    }
}
```

- [ ] **Step 2: 新增数据库迁移**

在 `gateway/db/migrations/0022_add_faq_semantic_cache_fields.sql` 写入：

```sql
alter table llm_request_logs
  add column cache_hit boolean not null default false,
  add column cache_type text not null default '',
  add column cache_key text not null default '',
  add column cache_faq_key text not null default '',
  add column classifier_model text not null default '',
  add column classifier_status text not null default '',
  add column classifier_latency_ms integer not null default 0 check (classifier_latency_ms >= 0);
```

- [ ] **Step 3: 扩展 `UsageRecord` 与 insert SQL**

在 `gateway/internal/service/usage_recording.go` 的 `UsageRecord`（或等价结构）增加：

```go
CacheHit            bool
CacheType           string
CacheKey            string
CacheFAQKey         string
ClassifierModel     string
ClassifierStatus    string
ClassifierLatencyMS int
```

并更新 `insert into llm_request_logs` 语句，把新字段加入列名与参数列表。

- [ ] **Step 4: 更新 seed / runtime 初始化**

在 `gateway/db/runtime.go` 的 `llm_request_logs`、`llm_usage_agg_hourly` seed 语句中，给新增字段补默认值，避免本地 demo 数据因列数不匹配启动失败：

```sql
cache_hit,
cache_type,
cache_key,
cache_faq_key,
classifier_model,
classifier_status,
classifier_latency_ms
```

对应值统一先填：

```sql
false, '', '', '', '', '', 0
```

- [ ] **Step 5: 跑定向测试验证失败转成功**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestUsageRecorderStoresFAQSemanticCacheFields' -count=1 -v`
Expected: 初次 FAIL（缺列/缺 SQL 片段），实现后 PASS。

- [ ] **Step 6: 跑迁移/启动相关测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./cmd/server -run 'TestServer.*|TestNewQuotaGuard.*' -count=1 -v`
Expected: PASS，说明 seed 与 schema 未被破坏。

- [ ] **Step 7: Commit**

```bash
git add gateway/db/migrations/0022_add_faq_semantic_cache_fields.sql gateway/internal/service/usage_recording.go gateway/internal/service/usage_recording_test.go gateway/db/runtime.go
git commit -m "feat: add faq semantic cache log fields"
```

### Task 2: 实现内置 FAQ 注册表与问题抽取/标准化

**Files:**
- Create: `gateway/internal/service/faq_registry.go`
- Create: `gateway/internal/service/faq_registry_test.go`
- Create: `gateway/internal/service/faq_semantic_cache_test.go`

- [ ] **Step 1: 先写 FAQ 注册表测试**

在 `gateway/internal/service/faq_registry_test.go` 写两个失败测试：

```go
func TestBuiltinFAQRegistryContainsExpectedEntries(t *testing.T) {
    registry := NewBuiltinFAQRegistry()

    faq, ok := registry.Get("faq.identity.who_are_you")
    if !ok {
        t.Fatal("expected faq.identity.who_are_you to exist")
    }
    if strings.TrimSpace(faq.Answer) == "" {
        t.Fatal("expected faq answer to be non-empty")
    }
    if faq.Version != "v1" {
        t.Fatalf("expected version v1, got %q", faq.Version)
    }
}

func TestBuiltinFAQRegistryRejectsDisabledOrUnknownKeys(t *testing.T) {
    registry := NewBuiltinFAQRegistry()
    if _, ok := registry.Get("faq.not_exists"); ok {
        t.Fatal("expected unknown faq key to be absent")
    }
}
```

- [ ] **Step 2: 先写问题提取/标准化测试**

在 `gateway/internal/service/faq_semantic_cache_test.go` 先加两个失败测试：

```go
func TestExtractLastUserQuestion(t *testing.T) {
    question, ok := extractLastUserQuestion([]ChatMessage{
        {Role: "system", Content: "你是助手"},
        {Role: "user", Content: "你好！！！  "},
        {Role: "assistant", Content: "你好"},
        {Role: "user", Content: "  你是谁？？？"},
    })
    if !ok {
        t.Fatal("expected last user question to be extracted")
    }
    if question != "你是谁？" {
        t.Fatalf("expected normalized question, got %q", question)
    }
}

func TestExtractLastUserQuestionReturnsFalseWhenNoUserMessage(t *testing.T) {
    _, ok := extractLastUserQuestion([]ChatMessage{{Role: "assistant", Content: "hello"}})
    if ok {
        t.Fatal("expected extraction to fail without user message")
    }
}
```

- [ ] **Step 3: 实现 FAQ 注册表最小结构**

在 `gateway/internal/service/faq_registry.go` 写入：

```go
type FAQEntry struct {
    Key     string
    Title   string
    Answer  string
    Version string
    Enabled bool
    Tags    []string
}

type FAQRegistry interface {
    Get(key string) (FAQEntry, bool)
}

type builtinFAQRegistry struct {
    entries map[string]FAQEntry
}

func NewBuiltinFAQRegistry() FAQRegistry {
    entries := map[string]FAQEntry{
        "faq.greeting.hello": {
            Key:     "faq.greeting.hello",
            Title:   "问候语",
            Answer:  "你好！我是企业 AI Gateway 的智能助手，有什么可以帮你？",
            Version: "v1",
            Enabled: true,
            Tags:    []string{"greeting"},
        },
        "faq.identity.who_are_you": {
            Key:     "faq.identity.who_are_you",
            Title:   "身份说明",
            Answer:  "我是企业 AI Gateway 提供的智能助手，用于统一接入和管理大模型能力。",
            Version: "v1",
            Enabled: true,
            Tags:    []string{"identity"},
        },
    }
    return builtinFAQRegistry{entries: entries}
}

func (r builtinFAQRegistry) Get(key string) (FAQEntry, bool) {
    entry, ok := r.entries[strings.TrimSpace(key)]
    if !ok || !entry.Enabled {
        return FAQEntry{}, false
    }
    return entry, true
}
```

- [ ] **Step 4: 实现问题提取与标准化最小逻辑**

在 `gateway/internal/service/faq_semantic_cache.go`（先创建空文件）补最小辅助函数：

```go
func extractLastUserQuestion(messages []ChatMessage) (string, bool) {
    for index := len(messages) - 1; index >= 0; index-- {
        if strings.TrimSpace(messages[index].Role) != "user" {
            continue
        }
        content := normalizeFAQQuestion(messages[index].Content)
        if content == "" {
            return "", false
        }
        return content, true
    }
    return "", false
}

func normalizeFAQQuestion(input string) string {
    text := strings.TrimSpace(input)
    text = strings.Join(strings.Fields(text), " ")
    text = strings.ReplaceAll(text, "？？？", "？")
    text = strings.ReplaceAll(text, "！！！", "！")
    text = strings.ReplaceAll(text, "??", "？")
    text = strings.ReplaceAll(text, "!!", "！")
    return text
}
```

- [ ] **Step 5: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestBuiltinFAQRegistry|TestExtractLastUserQuestion' -count=1 -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/service/faq_registry.go gateway/internal/service/faq_registry_test.go gateway/internal/service/faq_semantic_cache.go gateway/internal/service/faq_semantic_cache_test.go
git commit -m "feat: add builtin faq registry and normalization"
```

### Task 3: 实现 `qwen-mt-flash` 判定服务

**Files:**
- Create: `gateway/internal/service/faq_classifier.go`
- Create: `gateway/internal/service/faq_classifier_test.go`
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/cmd/server/main.go`

- [ ] **Step 1: 先写 classifier 结果解析测试**

在 `gateway/internal/service/faq_classifier_test.go` 写失败测试：

```go
func TestFAQClassifierParsesMatchedJSONResult(t *testing.T) {
    raw := `{"matched":true,"faq_key":"faq.identity.who_are_you","confidence":0.97,"reason":"用户在问身份"}`
    result, err := parseFAQClassifierResult(raw)
    if err != nil {
        t.Fatalf("parseFAQClassifierResult returned error: %v", err)
    }
    if !result.Matched || result.FAQKey != "faq.identity.who_are_you" || result.Confidence != 0.97 {
        t.Fatalf("unexpected result: %#v", result)
    }
}

func TestFAQClassifierRejectsInvalidJSON(t *testing.T) {
    _, err := parseFAQClassifierResult(`hello`)
    if err == nil {
        t.Fatal("expected invalid json to fail")
    }
}
```

- [ ] **Step 2: 新增配置项与默认值**

在 `gateway/internal/config/config.go` 的 `Config` 增加：

```go
FAQSemanticCacheEnabled             bool
FAQSemanticCacheModel               string
FAQSemanticCacheTimeoutMS           int
FAQSemanticCacheConfidenceThreshold float64
FAQSemanticCacheRedisTTLSeconds     int
```

并在 `Load()` 中设置：

```go
FAQSemanticCacheEnabled:             lookupEnvBool("FAQ_SEMANTIC_CACHE_ENABLED", false),
FAQSemanticCacheModel:               lookupEnvDefault("FAQ_SEMANTIC_CACHE_MODEL", "qwen-mt-flash"),
FAQSemanticCacheTimeoutMS:           lookupEnvInt("FAQ_SEMANTIC_CACHE_TIMEOUT_MS", 1500),
FAQSemanticCacheConfidenceThreshold: lookupEnvFloat64("FAQ_SEMANTIC_CACHE_CONFIDENCE_THRESHOLD", 0.90),
FAQSemanticCacheRedisTTLSeconds:     lookupEnvInt("FAQ_SEMANTIC_CACHE_REDIS_TTL_SECONDS", 86400),
```

- [ ] **Step 3: 实现 classifier 结构与解析器**

在 `gateway/internal/service/faq_classifier.go` 写入：

```go
type FAQClassifierResult struct {
    Matched    bool    `json:"matched"`
    FAQKey     string  `json:"faq_key"`
    Confidence float64 `json:"confidence"`
    Reason     string  `json:"reason"`
}

func parseFAQClassifierResult(raw string) (FAQClassifierResult, error) {
    var result FAQClassifierResult
    if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
        return FAQClassifierResult{}, err
    }
    result.FAQKey = strings.TrimSpace(result.FAQKey)
    result.Reason = strings.TrimSpace(result.Reason)
    return result, nil
}
```

- [ ] **Step 4: 实现 classifier 调用接口**

在同文件增加接口与最小实现：

```go
type FAQClassifier interface {
    Classify(ctx context.Context, question string) (FAQClassifierResult, error)
}

type faqClassifierService struct {
    client          UpstreamChatClient
    target          domain.ProviderTarget
    model           string
    timeout         time.Duration
}
```

`Classify()` 使用现有 OpenAI 兼容 client 发一个非流式 chat completion，请求体强约束输出 JSON。

- [ ] **Step 5: 在 `cmd/server` 装配 classifier**

在 `gateway/cmd/server/main.go` 增加：

```go
faqClassifier := service.NewNoopFAQClassifier()
if cfg.FAQSemanticCacheEnabled {
    faqClassifier = service.NewFAQClassifierService(
        upstreamClient,
        domain.ProviderTarget{
            Provider: "qwen",
            BaseURL: cfg.BootstrapProviderBaseURL,
            APIKey: cfg.BootstrapProviderAPIKey,
            Model: cfg.FAQSemanticCacheModel,
        },
        cfg.FAQSemanticCacheModel,
        time.Duration(cfg.FAQSemanticCacheTimeoutMS)*time.Millisecond,
    )
}
```

如果当前项目已有更合适的 `ProviderTarget` 构造方式，则在实现时以现有路由/凭据模式为准，不另造第二套 provider 管理。

- [ ] **Step 6: 跑配置与解析定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./cmd/server -run 'TestFAQClassifier|TestLoadConfig|TestMain' -count=1 -v`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add gateway/internal/service/faq_classifier.go gateway/internal/service/faq_classifier_test.go gateway/internal/config/config.go gateway/cmd/server/main.go
git commit -m "feat: add faq classifier service and config"
```

### Task 4: 实现 Redis FAQ 缓存服务

**Files:**
- Create: `gateway/internal/service/faq_cache_service.go`
- Create: `gateway/internal/service/faq_cache_service_test.go`
- Modify: `gateway/cmd/server/main.go`

- [ ] **Step 1: 先写 key/value 结构测试**

在 `gateway/internal/service/faq_cache_service_test.go` 写失败测试：

```go
func TestFAQCacheKeyUsesAPIKeyAndVersion(t *testing.T) {
    key := buildFAQCacheKey("pak_live_console", "faq.identity.who_are_you", "v1")
    want := "faq_cache:pak_live_console:faq.identity.who_are_you:v1"
    if key != want {
        t.Fatalf("expected %q, got %q", want, key)
    }
}

func TestFAQCacheServiceReturnsMissForEmptyValue(t *testing.T) {
    service := NewFAQCacheService(newFakeRedisClient(), time.Hour)
    _, hit, err := service.Get(context.Background(), "pak_live_console", FAQEntry{Key: "faq.identity.who_are_you", Version: "v1"})
    if err != nil {
        t.Fatalf("Get returned error: %v", err)
    }
    if hit {
        t.Fatal("expected cache miss")
    }
}
```

- [ ] **Step 2: 定义缓存值结构**

在 `gateway/internal/service/faq_cache_service.go` 写入：

```go
type FAQCacheEntry struct {
    FAQKey    string    `json:"faq_key"`
    Answer    string    `json:"answer"`
    Version   string    `json:"version"`
    Source    string    `json:"source"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    HitCount  int64     `json:"hit_count"`
}
```

- [ ] **Step 3: 实现 cache service 最小接口**

同文件补：

```go
type FAQCacheClient interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

type FAQCacheService interface {
    Get(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, bool, error)
    Set(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, error)
}
```

实现 `Get()` / `Set()`，用 JSON 存储，`Set()` 的 `Source` 固定为 `builtin`。

- [ ] **Step 4: 在 `cmd/server` 接 Redis**

在 `gateway/cmd/server/main.go` 里，如果 `cfg.RedisURL` 非空且 FAQ cache enabled，则复用现有 Redis client 或新建轻量 client 装配：

```go
faqCache := service.NewNoopFAQCacheService()
if cfg.FAQSemanticCacheEnabled && redisClient != nil {
    faqCache = service.NewFAQCacheService(service.NewGoRedisFAQCacheClient(redisClient), time.Duration(cfg.FAQSemanticCacheRedisTTLSeconds)*time.Second)
}
```

若当前 `main.go` 已创建 Redis client 用于 quota guard，则优先复用，避免重复连接。

- [ ] **Step 5: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestFAQCache' -count=1 -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/service/faq_cache_service.go gateway/internal/service/faq_cache_service_test.go gateway/cmd/server/main.go
git commit -m "feat: add faq semantic cache redis service"
```

### Task 5: 把 FAQ 语义缓存编排接入非流式聊天主链路

**Files:**
- Create: `gateway/internal/service/faq_semantic_cache.go`
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/service/proxy_service_test.go`
- Modify: `gateway/tests/integration/proxy_test.go`

- [ ] **Step 1: 先写 `ChatProxyService` 单测，锁定命中/未命中行为**

在 `gateway/internal/service/proxy_service_test.go` 写两个失败测试：

```go
func TestChatProxyServiceReturnsFAQCachedResponseForNonStreamRequest(t *testing.T) {
    publisher := queue.NewRecordingUsagePublisher()
    cache := stubFAQSemanticCacheOrchestrator{
        result: FAQSemanticCacheOutcome{
            Hit: true,
            Response: ChatResponse{
                Model: "qwen-flash",
                Choices: []ChatChoice{{Message: ChatMessage{Role: "assistant", Content: "我是企业 AI Gateway 提供的智能助手。"}}},
                Usage: &TokenUsage{PromptTokens: 12, CompletionTokens: 10, TotalTokens: 22},
            },
            Metadata: FAQSemanticCacheMetadata{
                CacheHit: true,
                CacheType: "faq_semantic",
                CacheFAQKey: "faq.identity.who_are_you",
                ClassifierModel: "qwen-mt-flash",
                ClassifierStatus: "hit",
                ClassifierLatencyMS: 88,
            },
        },
    }
    upstream := stubUpstreamChatClient{t: t, forbidComplete: true}

    proxy := NewChatProxyServiceWithFAQCache(upstream, publisher, nil, cache)
    _, err := proxy.Complete(newRequestContext(t), ChatRequest{Model: "qwen-flash", Messages: []ChatMessage{{Role: "user", Content: "你是谁"}}}, resolvedContext(t))
    if err != nil {
        t.Fatalf("Complete returned error: %v", err)
    }
}

func TestChatProxyServiceFallsBackToUpstreamWhenFAQCacheMisses(t *testing.T) {
    publisher := queue.NewRecordingUsagePublisher()
    cache := stubFAQSemanticCacheOrchestrator{result: FAQSemanticCacheOutcome{Hit: false}}
    upstream := stubUpstreamChatClient{response: ChatResponse{Choices: []ChatChoice{{Message: ChatMessage{Role: "assistant", Content: "upstream"}}}}, statusCode: 200}

    proxy := NewChatProxyServiceWithFAQCache(upstream, publisher, nil, cache)
    resp, err := proxy.Complete(newRequestContext(t), ChatRequest{Model: "qwen-flash", Messages: []ChatMessage{{Role: "user", Content: "真实问题"}}}, resolvedContext(t))
    if err != nil {
        t.Fatalf("Complete returned error: %v", err)
    }
    if resp.Choices[0].Message.Content != "upstream" {
        t.Fatalf("expected upstream response, got %#v", resp)
    }
}
```

- [ ] **Step 2: 定义 FAQ 语义缓存编排接口**

在 `gateway/internal/service/faq_semantic_cache.go` 写入：

```go
type FAQSemanticCacheMetadata struct {
    CacheHit            bool
    CacheType           string
    CacheKey            string
    CacheFAQKey         string
    ClassifierModel     string
    ClassifierStatus    string
    ClassifierLatencyMS int
}

type FAQSemanticCacheOutcome struct {
    Hit      bool
    Response ChatResponse
    Metadata FAQSemanticCacheMetadata
}

type FAQSemanticCacheOrchestrator interface {
    TryServe(ctx context.Context, req ChatRequest, resolved domain.ResolvedRoute) (FAQSemanticCacheOutcome, error)
}
```

- [ ] **Step 3: 实现最小编排逻辑**

`TryServe()` 最小流程：

1. `req.Stream == true` -> `Hit=false`
2. 提取最后一条 user 问题
3. 调 classifier
4. `matched != true` / `confidence < 阈值` / faq key 不存在 -> `Hit=false`
5. 先查 Redis
6. miss 时从 registry 取答案并写 Redis
7. 构造 `ChatResponse`
8. 返回 `Metadata`

命中返回体示例：

```go
ChatResponse{
    Model: req.Model,
    Usage: &TokenUsage{
        PromptTokens: estimatePromptTokens(req.Messages),
        CompletionTokens: estimateCompletionTokens(faq.Answer),
        TotalTokens: estimatePromptTokens(req.Messages) + estimateCompletionTokens(faq.Answer),
    },
    Choices: []ChatChoice{{
        Message: ChatMessage{Role: "assistant", Content: faq.Answer},
    }},
}
```

- [ ] **Step 4: 在 `proxy_service.go` 插入非流式命中逻辑**

在 `chatProxyService` 结构增加：

```go
faqCache FAQSemanticCacheOrchestrator
```

并在 `Complete()` 中、调用 `s.client.Complete()` 之前加入：

```go
if s.faqCache != nil && !req.Stream {
    outcome, cacheErr := s.faqCache.TryServe(ctx, req, requestContext)
    if cacheErr == nil && outcome.Hit {
        now := time.Now().UTC()
        record := NewChatUsageRecord(requestID, requestContext, req, outcome.Response, http.StatusOK, start, now, nil)
        record.CacheHit = outcome.Metadata.CacheHit
        record.CacheType = outcome.Metadata.CacheType
        record.CacheKey = outcome.Metadata.CacheKey
        record.CacheFAQKey = outcome.Metadata.CacheFAQKey
        record.ClassifierModel = outcome.Metadata.ClassifierModel
        record.ClassifierStatus = outcome.Metadata.ClassifierStatus
        record.ClassifierLatencyMS = outcome.Metadata.ClassifierLatencyMS
        s.recordWithEvents(ctx, record,
            usageRecordEvent{eventType: "classifier_hit", detail: outcome.Metadata.CacheFAQKey},
            usageRecordEvent{eventType: "cache_served", detail: outcome.Metadata.CacheType},
        )
        return outcome.Response, nil
    }
}
```

如 cache service 返回 error，不抛给用户，继续走上游；同时记录 `classifier_status=error` 或 event `fallback_upstream`。

- [ ] **Step 5: 增加集成测试**

在 `gateway/tests/integration/proxy_test.go` 新增三类场景：

1. 非流式问题“你是谁”命中 FAQ，断言上游 provider server **未被调用**
2. 非流式普通问题 miss，断言上游 provider server **被调用**
3. `stream=true` 请求，断言即使内容是 FAQ 也仍走上游 stream

- [ ] **Step 6: 跑单测与集成测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestChatProxyService.*FAQ|TestExtractLastUserQuestion' -count=1 -v`
Expected: PASS。

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./tests/integration -run 'TestChat.*FAQ|TestChatProxy' -count=1 -v`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add gateway/internal/service/faq_semantic_cache.go gateway/internal/service/proxy_service.go gateway/internal/service/proxy_service_test.go gateway/tests/integration/proxy_test.go
git commit -m "feat: serve builtin faq answers from semantic cache"
```

### Task 6: 让请求详情、事件流和失败回退具备可观测性

**Files:**
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `gateway/internal/service/usage_recording_test.go`

- [ ] **Step 1: 先写查询层测试，确保新字段能被读出**

在 `gateway/internal/http/router_test.go` 或 `gateway/internal/service/postgres_console_service_test.go` 增加失败测试，断言请求明细接口返回：

- `cache_hit`
- `cache_type`
- `cache_faq_key`
- `classifier_status`
- `classifier_latency_ms`

示例断言：

```go
if detail.CacheHit != true {
    t.Fatalf("expected cache hit true, got %#v", detail)
}
if detail.CacheFAQKey != "faq.identity.who_are_you" {
    t.Fatalf("expected faq key, got %#v", detail)
}
```

- [ ] **Step 2: 扩展事件写入**

在 `usage_recording.go` 中允许写入以下事件类型：

```go
usageRecordEvent{eventType: "classifier_started", detail: "qwen-mt-flash"}
usageRecordEvent{eventType: "classifier_miss", detail: "confidence_too_low"}
usageRecordEvent{eventType: "classifier_hit", detail: "faq.identity.who_are_you"}
usageRecordEvent{eventType: "cache_served", detail: "faq_semantic"}
usageRecordEvent{eventType: "fallback_upstream", detail: "classifier_timeout"}
```

如果当前事件表没有 event type 枚举限制，则只需确保展示层不会过滤掉新类型。

- [ ] **Step 3: 扩展控制台查询 SQL**

在 `gateway/internal/service/postgres_console_service.go` 相关查询中把新字段 select 出来，并映射到明细 DTO。

建议优先补：

- 调用明细列表
- 单条请求详情
- 异常事件流（至少看得到 fallback 事件）

- [ ] **Step 4: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'UsageRecording|AdminUsage|RequestDetail|Failures' -count=1 -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/service/usage_recording.go gateway/internal/service/postgres_console_service.go gateway/internal/http/router_test.go gateway/internal/service/usage_recording_test.go
git commit -m "feat: expose faq semantic cache observability"
```

### Task 7: 配置装配、本地部署验证与文档回写

**Files:**
- Modify: `deploy/compose/.env.local`（如本地需要打开开关；注意不要提交敏感值）
- Modify: `docs/specs/2026-05-12-faq-semantic-cache-design.md`
- Modify: `README.md`（仅当需要补部署说明时）

- [ ] **Step 1: 先写 `cmd/server` 级别的开关测试**

在 `gateway/cmd/server/main_test.go` 增加失败测试，覆盖：

1. `FAQ_SEMANTIC_CACHE_ENABLED=false` 时不装配 FAQ cache
2. `FAQ_SEMANTIC_CACHE_ENABLED=true` 且 Redis 可用时装配成功
3. FAQ cache 失败不会影响 `POST /v1/chat/completions` 正常回退上游

- [ ] **Step 2: 本地 env 打开开关（仅本地，不提交密钥）**

在本地运行时环境设置：

```env
FAQ_SEMANTIC_CACHE_ENABLED=true
FAQ_SEMANTIC_CACHE_MODEL=qwen-mt-flash
FAQ_SEMANTIC_CACHE_TIMEOUT_MS=1500
FAQ_SEMANTIC_CACHE_CONFIDENCE_THRESHOLD=0.90
FAQ_SEMANTIC_CACHE_REDIS_TTL_SECONDS=86400
```

如果 `qwen-mt-flash` 需要单独 provider 凭据，则复用现有 provider 配置，不在文档和提交中写入真实 key。

- [ ] **Step 3: 跑后端完整定向回归**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./tests/integration ./cmd/server -count=1`
Expected: PASS。

- [ ] **Step 4: 本地重部署**

Run: `cd /root/liwenjian/ai_gateway && docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build gateway`
Expected: `gateway` 容器成功重建并启动。

- [ ] **Step 5: 用 curl 做三条冒烟验证**

Run:

```bash
curl -sS -H "Authorization: Bearer <API_KEY>" -H "Content-Type: application/json" -X POST http://127.0.0.1:32658/v1/chat/completions -d '{"model":"qwen-flash","messages":[{"role":"user","content":"你是谁"}]}'
```

Expected: 200，返回平台内置标准答案。

Run:

```bash
curl -sS -H "Authorization: Bearer <API_KEY>" -H "Content-Type: application/json" -X POST http://127.0.0.1:32658/v1/chat/completions -d '{"model":"qwen-flash","messages":[{"role":"user","content":"请写一个 golang 并发示例"}]}'
```

Expected: 200，正常走上游模型，不返回固定 FAQ。

Run:

```bash
curl -sS -H "Authorization: Bearer <API_KEY>" -H "Content-Type: application/json" -X POST http://127.0.0.1:32658/v1/chat/completions -d '{"model":"qwen-flash","stream":true,"messages":[{"role":"user","content":"你是谁"}]}'
```

Expected: 200，仍走流式主链路。

- [ ] **Step 6: 回写 spec / README（如有实际差异）**

若实现中阈值、字段名、事件名有调整，把最终值同步回 `docs/specs/2026-05-12-faq-semantic-cache-design.md`；若部署方式需要额外说明，再补 `README.md` 中对应段落。

- [ ] **Step 7: Commit**

```bash
git add docs/specs/2026-05-12-faq-semantic-cache-design.md README.md gateway/cmd/server/main_test.go
# deploy/compose/.env.local 仅本地使用，不提交
git commit -m "docs: finalize faq semantic cache rollout notes"
```

---

## 自检结果

### 1. Spec 覆盖检查

- 非流式聊天请求拦截：Task 5 覆盖
- `qwen-mt-flash` 判定：Task 3 覆盖
- API Key 级缓存隔离：Task 4 / Task 5 覆盖
- FAQ 白名单与标准答案：Task 2 覆盖
- 判定失败直接放行：Task 5 覆盖
- 正常计费口径与新字段：Task 1 / Task 5 / Task 6 覆盖
- 观测字段与事件流：Task 1 / Task 6 覆盖
- 本地部署验证：Task 7 覆盖

### 2. Placeholder 扫描

- 未使用 `TODO`、`TBD`、`implement later` 等占位词
- 每个任务包含明确文件路径、测试命令和最小代码样例

### 3. 类型一致性检查

- 统一使用：`FAQEntry`、`FAQClassifierResult`、`FAQCacheEntry`、`FAQSemanticCacheOutcome`
- 统一日志字段：`CacheHit`、`CacheType`、`CacheKey`、`CacheFAQKey`、`ClassifierModel`、`ClassifierStatus`、`ClassifierLatencyMS`
