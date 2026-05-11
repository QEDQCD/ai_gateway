# Content Moderation And Privacy Protection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `POST /v1/chat/completions` 增加请求前内容审核与隐私保护链路：用 `qwen-mt-flash` 识别明显网络攻击类请求并拦截，同时把手机号等敏感信息脱敏后再发送给真实业务模型。

**Architecture:** 在现有 `proxy_service` 进入上游模型之前插入 `ContentGuardService`。该服务先调用审核模型返回结构化判定；若命中攻击则直接拦截；若审核失败则降级为本地正则脱敏；若审核通过则组合“大模型识别片段 + 本地规则兜底”的脱敏结果，再把脱敏后的 `messages` 继续送入原有代理链路。日志与观测继续复用现有 usage/audit 体系，只保存脱敏后的摘要。

**Tech Stack:** Go, Fiber, existing proxy service, existing OpenAI-compatible upstream client, PostgreSQL usage/audit pipeline, Vitest not required, Go test with TDD.

---

## File Map

- `gateway/internal/security/redaction.go`
  - 现有展示/响应脱敏规则；需要扩展“上游转发脱敏”接口，避免破坏已有显示语义。
- `gateway/internal/security/redaction_test.go`
  - 现有脱敏测试；需要补充 `***` 样式的请求前脱敏测试。
- `gateway/internal/service/content_guard_service.go`
  - 新增内容审核与请求脱敏编排服务。
- `gateway/internal/service/content_guard_service_test.go`
  - 新增内容审核服务测试，覆盖 allow/block/fallback/redaction。
- `gateway/internal/service/moderation_client.go`
  - 新增审核模型调用与结构化 JSON 解析封装。
- `gateway/internal/service/proxy_service.go`
  - 在 `chat/completions` 主链路接入 `ContentGuardService`。
- `gateway/internal/service/proxy_service_test.go`
  - 新增/修改代理测试，验证上游收到的是脱敏正文，攻击请求被拦截。
- `gateway/internal/config/config.go`
  - 如需要，新增内容审核配置。
- `gateway/cmd/server/main.go`
  - 如需要，完成服务装配与配置注入。

---

### Task 1: 扩展本地脱敏能力，区分展示脱敏与上游转发脱敏

**Files:**
- Modify: `gateway/internal/security/redaction.go`
- Modify: `gateway/internal/security/redaction_test.go`

- [ ] **Step 1: 写失败测试，锁定“展示脱敏保持原样、上游脱敏统一替换为 ***”**

```go
func TestSanitizeTextForUpstreamReplacesPhoneWithStars(t *testing.T) {
    t.Parallel()

    got := security.SanitizeTextForUpstream("请联系我 13812345678")
    if got != "请联系我 ***" {
        t.Fatalf("expected upstream sanitized phone, got %q", got)
    }
}

func TestRedactTextForDisplayKeepsMaskedPreviewStyle(t *testing.T) {
    t.Parallel()

    got := security.RedactTextForDisplay("请联系我 13812345678")
    if got != "请联系我 138XXXX5678" {
        t.Fatalf("expected display redaction, got %q", got)
    }
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/security -run 'Test(SanitizeTextForUpstreamReplacesPhoneWithStars|RedactTextForDisplayKeepsMaskedPreviewStyle)' -count=1`
Expected: FAIL，提示缺少 `SanitizeTextForUpstream` / `RedactTextForDisplay` 或行为不匹配。

- [ ] **Step 3: 写最小实现，保留旧 `RedactText` 兼容层**

```go
func RedactTextForDisplay(input string) string {
    // 沿用现有 138XXXX5678 / 身份证 / 邮箱脱敏逻辑
}

func SanitizeTextForUpstream(input string) string {
    // 手机号整段替换为 ***
}

func RedactText(input string) string {
    return RedactTextForDisplay(input)
}
```

- [ ] **Step 4: 回跑测试确认通过**

Run: `go test ./internal/security -run 'Test(SanitizeTextForUpstreamReplacesPhoneWithStars|RedactTextForDisplayKeepsMaskedPreviewStyle)' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add gateway/internal/security/redaction.go gateway/internal/security/redaction_test.go
git commit -m "feat: add upstream sanitization helpers"
```

---

### Task 2: 新增审核模型客户端，解析 `qwen-mt-flash` 结构化输出

**Files:**
- Create: `gateway/internal/service/moderation_client.go`
- Create: `gateway/internal/service/content_guard_service_test.go`

- [ ] **Step 1: 写失败测试，锁定审核 JSON 的 allow/block/非法响应解析**

```go
func TestModerationClientParsesAllowDecision(t *testing.T) {
    t.Parallel()

    client := newStubModerationClient(`{"decision":"allow","reason":"ok","attack_type":"","redactions":[{"type":"phone","text":"13812345678","replacement":"***"}]}`)
    result, err := client.Moderate(context.Background(), ModerationRequest{UserText: "手机号 13812345678"})
    if err != nil {
        t.Fatalf("Moderate returned error: %v", err)
    }
    if result.Decision != GuardDecisionAllow {
        t.Fatalf("expected allow, got %q", result.Decision)
    }
    if len(result.Redactions) != 1 {
        t.Fatalf("expected 1 redaction, got %d", len(result.Redactions))
    }
}

func TestModerationClientRejectsInvalidJSON(t *testing.T) {
    t.Parallel()

    client := newStubModerationClient(`not-json`)
    _, err := client.Moderate(context.Background(), ModerationRequest{UserText: "hello"})
    if err == nil {
        t.Fatal("expected invalid JSON error")
    }
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/service -run 'TestModerationClient(ParsesAllowDecision|RejectsInvalidJSON)' -count=1`
Expected: FAIL，提示缺少 `ModerationRequest` / `Moderate` / 解析实现。

- [ ] **Step 3: 写最小实现，定义结构体与 JSON 校验**

```go
type GuardDecision string

const (
    GuardDecisionAllow GuardDecision = "allow"
    GuardDecisionBlock GuardDecision = "block"
)

type ModerationRedaction struct {
    Type        string `json:"type"`
    Text        string `json:"text"`
    Replacement string `json:"replacement"`
}

type ModerationResult struct {
    Decision   GuardDecision        `json:"decision"`
    Reason     string               `json:"reason"`
    AttackType string               `json:"attack_type"`
    Redactions []ModerationRedaction `json:"redactions"`
}
```

实现一个最小 `Moderate`：
- 发送 prompt（先允许用 stub 注入，真实 HTTP 在同文件后续补）
- 解析 JSON
- 校验 `decision` 只能是 `allow/block`

- [ ] **Step 4: 回跑测试确认通过**

Run: `go test ./internal/service -run 'TestModerationClient(ParsesAllowDecision|RejectsInvalidJSON)' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add gateway/internal/service/moderation_client.go gateway/internal/service/content_guard_service_test.go
git commit -m "feat: add moderation client result parsing"
```

---

### Task 3: 实现 `ContentGuardService`，覆盖 allow / block / fallback / 脱敏合并

**Files:**
- Create: `gateway/internal/service/content_guard_service.go`
- Modify: `gateway/internal/service/content_guard_service_test.go`

- [ ] **Step 1: 写失败测试，锁定四类核心行为**

```go
func TestContentGuardServiceBlocksAttackRequest(t *testing.T) {
    t.Parallel()

    svc := NewContentGuardService(stubModerationServiceResult(ModerationResult{
        Decision: GuardDecisionBlock,
        Reason: "检测到疑似 SQL 注入内容",
        AttackType: "sql_injection",
    }))

    result, err := svc.GuardChatMessages(context.Background(), GuardChatRequest{
        Messages: []ChatMessage{{Role: "user", Content: "' OR 1=1; DROP TABLE users; --"}},
    })
    if err != nil {
        t.Fatalf("GuardChatMessages returned error: %v", err)
    }
    if result.Decision != GuardDecisionBlock {
        t.Fatalf("expected block, got %q", result.Decision)
    }
}

func TestContentGuardServiceFallsBackToRegexWhenModerationFails(t *testing.T) {
    t.Parallel()

    svc := NewContentGuardService(stubModerationServiceError(errors.New("timeout")))
    result, err := svc.GuardChatMessages(context.Background(), GuardChatRequest{
        Messages: []ChatMessage{{Role: "user", Content: "我手机号是13812345678"}},
    })
    if err != nil {
        t.Fatalf("GuardChatMessages returned error: %v", err)
    }
    if got := result.SanitizedMessages[0].Content; got != "我手机号是***" {
        t.Fatalf("expected fallback sanitized content, got %q", got)
    }
    if result.AuditSource != "fallback_regex" {
        t.Fatalf("expected fallback audit source, got %q", result.AuditSource)
    }
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/service -run 'TestContentGuardService(BlocksAttackRequest|FallsBackToRegexWhenModerationFails)' -count=1`
Expected: FAIL

- [ ] **Step 3: 写最小实现**

实现：
- `GuardChatRequest`
- `GuardChatResult`
- `ContentGuardService`
- 仅提取 `role=user` 的文本进行审核
- `block` 时直接返回结论，不改写消息
- `allow` 时先按 `redactions` 替换，再跑 `SanitizeTextForUpstream`
- 审核异常时走 fallback regex

- [ ] **Step 4: 回跑测试确认通过**

Run: `go test ./internal/service -run 'TestContentGuardService(BlocksAttackRequest|FallsBackToRegexWhenModerationFails)' -count=1`
Expected: PASS

- [ ] **Step 5: 扩充测试，覆盖多消息与非 user 角色不参与审核文本拼接**

```go
func TestContentGuardServiceOnlyModeratesUserMessages(t *testing.T) {
    t.Parallel()
    // system / assistant 不应进入用户审核正文
}
```

Run: `go test ./internal/service -run 'TestContentGuardService' -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/service/content_guard_service.go gateway/internal/service/content_guard_service_test.go
git commit -m "feat: add content guard service"
```

---

### Task 4: 在代理主链路接入内容审核与请求前脱敏

**Files:**
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/service/proxy_service_test.go`

- [ ] **Step 1: 写失败测试，锁定“攻击请求不调用上游、正常请求上游收到脱敏正文”**

```go
func TestChatProxyCompleteBlocksAttackBeforeUpstream(t *testing.T) {
    t.Parallel()

    upstreamCalled := false
    proxy := service.NewChatProxyService(
        stubChatClient{
            completeFn: func(ctx context.Context, target domain.ProviderTarget, req service.ChatRequest) (service.ChatResponse, int, error) {
                upstreamCalled = true
                return service.ChatResponse{}, 200, nil
            },
        },
        queue.NewNoopUsagePublisher(),
        nil,
        service.NewStubContentGuard(service.GuardChatResult{
            Decision: service.GuardDecisionBlock,
            Reason:   "检测到疑似 SQL 注入内容",
        }),
    )

    _, err := proxy.Complete(context.Background(), service.ChatRequest{
        Model: "qwen-flash",
        Messages: []service.ChatMessage{{Role: "user", Content: "' OR 1=1 --"}},
    }, validRequestContext())
    if err == nil {
        t.Fatal("expected block error")
    }
    if upstreamCalled {
        t.Fatal("expected upstream not to be called")
    }
}

func TestChatProxyCompleteSendsSanitizedMessagesUpstream(t *testing.T) {
    t.Parallel()
    // 断言上游收到 `我手机号是***`
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/service -run 'TestChatProxyComplete(BlocksAttackBeforeUpstream|SendsSanitizedMessagesUpstream)' -count=1`
Expected: FAIL

- [ ] **Step 3: 修改代理构造与执行路径**

实现点：
- `chatProxyService` 增加 `guard ContentGuardService`
- `NewChatProxyService` 支持可选注入 guard；未注入则走 noop guard
- `Complete` 在调用上游前先 `GuardChatMessages`
- `Stream` 在调用上游前同样先 `GuardChatMessages`
- `block` 时返回中文 `StatusError`

- [ ] **Step 4: 回跑定向测试确认通过**

Run: `go test ./internal/service -run 'TestChatProxyComplete(BlocksAttackBeforeUpstream|SendsSanitizedMessagesUpstream)|TestChatProxyStream' -count=1`
Expected: PASS

- [ ] **Step 5: 确认现有响应侧脱敏测试仍通过**

Run: `go test ./internal/service -run 'TestChatProxy(CompleteRedactsSensitiveContentInResponse|StreamRedactsSensitiveContentInFinalResponseAndForwardedChunks)' -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/service/proxy_service.go gateway/internal/service/proxy_service_test.go
git commit -m "feat: guard chat requests before upstream"
```

---

### Task 5: 配置与装配 `qwen-mt-flash` 审核链路

**Files:**
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/cmd/server/main.go`
- Test: `gateway/cmd/server/main_test.go`

- [ ] **Step 1: 写失败测试，锁定内容审核配置已加载并注入服务**

```go
func TestLoadConfigIncludesContentGuardSettings(t *testing.T) {
    t.Setenv("GATEWAY_CONTENT_GUARD_ENABLED", "true")
    t.Setenv("GATEWAY_CONTENT_GUARD_MODEL", "qwen-mt-flash")
    t.Setenv("GATEWAY_CONTENT_GUARD_TIMEOUT_MS", "3000")

    cfg := config.Load()
    if !cfg.ContentGuardEnabled {
        t.Fatal("expected content guard enabled")
    }
    if cfg.ContentGuardModel != "qwen-mt-flash" {
        t.Fatalf("unexpected guard model: %q", cfg.ContentGuardModel)
    }
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/config ./cmd/server -run 'TestLoadConfigIncludesContentGuardSettings' -count=1`
Expected: FAIL

- [ ] **Step 3: 写最小配置与装配实现**

实现：
- `config.Config` 增加：
  - `ContentGuardEnabled bool`
  - `ContentGuardModel string`
  - `ContentGuardTimeoutMS int`
- `main.go` 中：
  - 构造 `ModerationClient`
  - 构造 `ContentGuardService`
  - 注入 `NewChatProxyService`
- 若未启用，则注入 noop guard

- [ ] **Step 4: 回跑测试确认通过**

Run: `go test ./internal/config ./cmd/server -run 'TestLoadConfigIncludesContentGuardSettings' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add gateway/internal/config/config.go gateway/cmd/server/main.go gateway/cmd/server/main_test.go
git commit -m "feat: wire content guard configuration"
```

---

### Task 6: 验证审计语义与整体验证

**Files:**
- Modify: `gateway/internal/service/proxy_service_test.go`
- Modify: `gateway/internal/service/content_guard_service_test.go`
- Optional Modify: `gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: 写失败测试，锁定 block / fallback 的中文语义**

```go
func TestChatProxyBlockErrorUsesChineseMessage(t *testing.T) {
    t.Parallel()
    // 断言错误消息包含 “请求被安全策略拦截”
}

func TestContentGuardFallbackMarksAuditSource(t *testing.T) {
    t.Parallel()
    // 断言 fallback_regex
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/service -run 'Test(ChatProxyBlockErrorUsesChineseMessage|ContentGuardFallbackMarksAuditSource)' -count=1`
Expected: FAIL

- [ ] **Step 3: 补齐最小实现并统一错误文案**

要求：
- block 错误文案使用中文
- fallback 保留 `fallback_regex`
- 不泄露审核 prompt 或内部实现细节

- [ ] **Step 4: 运行服务层完整相关测试**

Run: `go test ./internal/service ./internal/security -count=1`
Expected: PASS

- [ ] **Step 5: 如配置/装配有改动，再跑入口层定向测试**

Run: `go test ./cmd/server ./internal/config -count=1`
Expected: PASS 或仅暴露与本功能直接相关的问题

- [ ] **Step 6: 部署并验证**

Run: `docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build gateway`
Expected: `gateway` 容器重新构建并启动成功

- [ ] **Step 7: 手工验证两个场景**

Run:

```bash
curl -sS -H "Authorization: Bearer <API_KEY>" -H "Content-Type: application/json" -X POST http://127.0.0.1:32658/v1/chat/completions -d '{"model":"qwen-flash","messages":[{"role":"user","content":"我手机号是13812345678，你看到的手机号是什么"}]}'
```

Expected: 上游看到的应是脱敏后的 `***`，响应与日志里不应出现明文手机号。

Run:

```bash
curl -sS -H "Authorization: Bearer <API_KEY>" -H "Content-Type: application/json" -X POST http://127.0.0.1:32658/v1/chat/completions -d '{"model":"qwen-flash","messages":[{"role":"user","content":"SELECT * FROM users WHERE name = '' OR 1=1 --"}]}'
```

Expected: 返回中文安全拦截错误，不调用业务模型。

- [ ] **Step 8: 提交**

```bash
git add gateway/internal/service/proxy_service_test.go gateway/internal/service/content_guard_service_test.go gateway/internal/service/postgres_console_service_test.go

git commit -m "test: verify moderation fallback and block semantics"
```

---

## Spec Coverage Self-Review

- 审核模型 `qwen-mt-flash`：Task 2 / Task 5
- 攻击类请求拦截：Task 3 / Task 4 / Task 6
- 手机号替换为 `***` 并发送到上游：Task 1 / Task 3 / Task 4
- 审核失败时放行但本地脱敏：Task 3 / Task 4 / Task 6
- 仅覆盖 `chat/completions`：Task 4
- 中文错误语义：Task 6
- 保持现有展示侧脱敏不被破坏：Task 1 / Task 4

