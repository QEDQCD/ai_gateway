package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/config"
	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubModelHealthcheckCatalog struct {
	routes       []ModelHealthcheckRoute
	listErr      error
	updates      []ModelHealthcheckUpdate
	updatedRoute []string
	updateCh     chan struct{}
}

func (s *stubModelHealthcheckCatalog) ListRunnableRoutes(ctx context.Context) ([]ModelHealthcheckRoute, error) {
	return s.routes, s.listErr
}

func (s *stubModelHealthcheckCatalog) UpdateRouteHealth(ctx context.Context, routeID string, update ModelHealthcheckUpdate) error {
	s.updatedRoute = append(s.updatedRoute, routeID)
	s.updates = append(s.updates, update)
	if s.updateCh != nil {
		select {
		case s.updateCh <- struct{}{}:
		default:
		}
	}
	return nil
}

type stubModelHealthcheckClient struct {
	stream ChatCompletionStream
	err    error
	gotReq ChatRequest
	gotTgt domain.ProviderTarget
}

func (s *stubModelHealthcheckClient) Complete(context.Context, domain.ProviderTarget, ChatRequest) (ChatResponse, int, error) {
	return ChatResponse{}, 0, errors.New("unexpected Complete call")
}

func (s *stubModelHealthcheckClient) StreamComplete(_ context.Context, target domain.ProviderTarget, req ChatRequest) (ChatCompletionStream, int, error) {
	s.gotTgt = target
	s.gotReq = req
	if s.err != nil {
		return ChatCompletionStream{}, 502, s.err
	}
	return s.stream, 200, nil
}

func TestModelHealthcheckRunnerMarksHealthyOnFirstContentToken(t *testing.T) {
	t.Parallel()

	catalog := &stubModelHealthcheckCatalog{
		routes: []ModelHealthcheckRoute{{
			RouteID:              "route:pc_demo:qwen-flash",
			RequestedModel:       "qwen-flash",
			ProviderCredentialID: "pc_demo",
			Provider:             "openai",
			BaseURL:              "https://provider.example/v1",
			APIKey:               "provider-secret",
		}},
	}
	emitCalls := 0
	client := &stubModelHealthcheckClient{
		stream: ChatCompletionStream{
			StatusCode:  200,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error) {
				if err := emit([]byte("data: first-blank\n\n")); err != nil {
					return ChatStreamResult{}, err
				}
				emitCalls++
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: first-content\n\n")); err == nil {
					t.Fatal("expected emit to stop stream after first token")
				}
				return ChatStreamResult{
					SawContentToken: true,
					Response:        ChatResponse{Model: "qwen-flash"},
				}, nil
			},
		},
	}
	runner := NewModelHealthcheckRunner(catalog, client, config.Config{
		ModelHealthcheckPrompt:    "你好",
		ModelHealthcheckMaxTokens: 1,
		ModelHealthcheckTimeout:   20 * time.Second,
	})

	summary, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 || summary.Healthy != 1 || summary.Unhealthy != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if emitCalls != 1 {
		t.Fatalf("expected stream to stop after first token, got %d pre-stop emits", emitCalls)
	}
	if len(catalog.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(catalog.updates))
	}
	if got := catalog.updatedRoute[0]; got != "route:pc_demo:qwen-flash" {
		t.Fatalf("expected route id %q, got %q", "route:pc_demo:qwen-flash", got)
	}
	if got := catalog.updates[0].HealthStatus; got != "healthy" {
		t.Fatalf("expected health status %q, got %q", "healthy", got)
	}
	if got := catalog.updates[0].LastHealthError; got != "" {
		t.Fatalf("expected empty health error, got %q", got)
	}
	if got := catalog.updates[0].FirstTokenLatencyMS; got < 0 {
		t.Fatalf("expected non-negative first token latency, got %d", got)
	}
	if got := catalog.updates[0].LatencyMS; got < 0 {
		t.Fatalf("expected non-negative latency, got %d", got)
	}
	if client.gotReq.Model != "qwen-flash" {
		t.Fatalf("expected request model %q, got %q", "qwen-flash", client.gotReq.Model)
	}
	if !client.gotReq.Stream {
		t.Fatal("expected streaming request")
	}
	if client.gotReq.Messages[0].Content != "你好" {
		t.Fatalf("expected prompt %q, got %q", "你好", client.gotReq.Messages[0].Content)
	}
	if client.gotTgt.APIKey != "provider-secret" {
		t.Fatalf("expected API key %q, got %q", "provider-secret", client.gotTgt.APIKey)
	}
}

func TestModelHealthcheckRunnerReturnsUnhealthySummaryOnUpstreamFailure(t *testing.T) {
	t.Parallel()

	catalog := &stubModelHealthcheckCatalog{
		routes: []ModelHealthcheckRoute{{
			RouteID:              "route:pc_demo:mimo",
			RequestedModel:       "mimo-v2.5-pro",
			ProviderCredentialID: "pc_demo",
			Provider:             "openai",
			BaseURL:              "https://provider.example/v1",
			APIKey:               "provider-secret",
		}},
	}
	client := &stubModelHealthcheckClient{err: errors.New("upstream failed")}
	runner := NewModelHealthcheckRunner(catalog, client, config.Config{ModelHealthcheckTimeout: 20 * time.Second})

	summary, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 || summary.Healthy != 0 || summary.Unhealthy != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(catalog.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(catalog.updates))
	}
	if catalog.updates[0].LatencyMS < 0 {
		t.Fatalf("expected latency to be populated, got %d", catalog.updates[0].LatencyMS)
	}
}

func TestModelHealthcheckRunnerMarksHealthyOnReasoningFirstToken(t *testing.T) {
	t.Parallel()

	catalog := &stubModelHealthcheckCatalog{
		routes: []ModelHealthcheckRoute{{
			RouteID:              "route:pc_demo:mimo-v2.5-pro",
			RequestedModel:       "mimo-v2.5-pro",
			ProviderCredentialID: "pc_demo",
			Provider:             "mimo",
			BaseURL:              "https://provider.example/v1",
			APIKey:               "provider-secret",
		}},
	}
	client := &stubModelHealthcheckClient{
		stream: ChatCompletionStream{
			StatusCode:  200,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				return ChatStreamResult{
					SawContentToken: true,
					Response:        ChatResponse{Model: "mimo-v2.5-pro"},
				}, nil
			},
		},
	}
	runner := NewModelHealthcheckRunner(catalog, client, config.Config{
		ModelHealthcheckPrompt:    "你好",
		ModelHealthcheckMaxTokens: 1,
		ModelHealthcheckTimeout:   20 * time.Second,
	})

	summary, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 || summary.Healthy != 1 || summary.Unhealthy != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(catalog.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(catalog.updates))
	}
	if got := catalog.updates[0].HealthStatus; got != "healthy" {
		t.Fatalf("expected health status healthy, got %q", got)
	}
}

func TestModelHealthcheckRunnerMarksHealthyWhenUsageReportsCompletionTokens(t *testing.T) {
	t.Parallel()

	catalog := &stubModelHealthcheckCatalog{
		routes: []ModelHealthcheckRoute{{
			RouteID:              "route:pc_demo:mimo-v2.5-pro",
			RequestedModel:       "mimo-v2.5-pro",
			ProviderCredentialID: "pc_demo",
			Provider:             "mimo",
			BaseURL:              "https://provider.example/v1",
			APIKey:               "provider-secret",
		}},
	}
	client := &stubModelHealthcheckClient{
		stream: ChatCompletionStream{
			StatusCode:  200,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error) {
				return ChatStreamResult{
					Response: ChatResponse{
						Model: "mimo-v2.5-pro",
						Usage: &TokenUsage{
							PromptTokens:     10,
							CompletionTokens: 1,
							TotalTokens:      11,
						},
					},
				}, nil
			},
		},
	}
	runner := NewModelHealthcheckRunner(catalog, client, config.Config{
		ModelHealthcheckPrompt:    "你好",
		ModelHealthcheckMaxTokens: 1,
		ModelHealthcheckTimeout:   20 * time.Second,
	})

	summary, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 || summary.Healthy != 1 || summary.Unhealthy != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(catalog.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(catalog.updates))
	}
	if got := catalog.updates[0].HealthStatus; got != "healthy" {
		t.Fatalf("expected health status healthy, got %q", got)
	}
	if got := catalog.updates[0].FirstTokenLatencyMS; got < 0 {
		t.Fatalf("expected non-negative first token latency, got %d", got)
	}
}

func TestModelHealthcheckRunnerStartRunsImmediatelyBeforeFirstInterval(t *testing.T) {
	t.Parallel()

	updateCh := make(chan struct{}, 1)
	catalog := &stubModelHealthcheckCatalog{
		routes: []ModelHealthcheckRoute{{
			RouteID:              "route:pc_demo:qwen-flash",
			RequestedModel:       "qwen-flash",
			ProviderCredentialID: "pc_demo",
			Provider:             "openai",
			BaseURL:              "https://provider.example/v1",
			APIKey:               "provider-secret",
		}},
		updateCh: updateCh,
	}
	client := &stubModelHealthcheckClient{
		stream: ChatCompletionStream{
			StatusCode:  200,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				return ChatStreamResult{
					SawContentToken: true,
					Response:        ChatResponse{Model: "qwen-flash"},
				}, nil
			},
		},
	}
	runner := NewModelHealthcheckRunner(catalog, client, config.Config{
		ModelHealthcheckEnabled:  true,
		ModelHealthcheckInterval: time.Hour,
		ModelHealthcheckTimeout:  20 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runner.Start(ctx)

	select {
	case <-updateCh:
		cancel()
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected immediate healthcheck run before first interval elapses")
	}
}

func TestIsModelHealthcheckChatRequestMode(t *testing.T) {
	t.Parallel()

	if !isModelHealthcheckChatRequestMode("聊天") {
		t.Fatal("expected 聊天 to be accepted")
	}
	if !isModelHealthcheckChatRequestMode("推理") {
		t.Fatal("expected 推理 to be accepted")
	}
	if isModelHealthcheckChatRequestMode("向量") {
		t.Fatal("expected 向量 to be rejected")
	}
	if isModelHealthcheckChatRequestMode("知识库") {
		t.Fatal("expected 知识库 to be rejected")
	}
}

func TestPostgresModelHealthcheckCatalogListRunnableRoutesIgnoresUnrelatedBrokenSecretRef(t *testing.T) {
	catalog := postgresModelHealthcheckCatalog{
		db: &stubModelHealthcheckQueryDB{
			rows: [][]any{
				{"route:provider_good:qwen-flash", "qwen-flash", "provider_good", "dashscope", "https://dashscope.aliyuncs.com/compatible-mode/v1", "聊天"},
			},
		},
		repository: stubModelHealthcheckRepository{
			listErr: errors.New("should not list all credentials"),
			credentialsByID: map[string]store.ProviderCredentialRecord{
				"provider_good": {
					ID:       "provider_good",
					Provider: "dashscope",
					BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
					APIKey:   "provider-secret",
				},
			},
		},
	}

	routes, err := catalog.ListRunnableRoutes(context.Background())
	if err != nil {
		t.Fatalf("ListRunnableRoutes returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 runnable route, got %d", len(routes))
	}
	if routes[0].RouteID != "route:provider_good:qwen-flash" {
		t.Fatalf("expected runnable good route, got %+v", routes[0])
	}
	if routes[0].APIKey != "provider-secret" {
		t.Fatalf("expected resolved API key, got %+v", routes[0])
	}
}

func TestPostgresModelHealthcheckCatalogListRunnableRoutesSkipsRouteWithMissingCredentialSecret(t *testing.T) {
	catalog := postgresModelHealthcheckCatalog{
		db: &stubModelHealthcheckQueryDB{
			rows: [][]any{
				{"route:provider_missing_secret:qwen-flash", "qwen-flash", "provider_missing_secret", "dashscope", "https://dashscope.aliyuncs.com/compatible-mode/v1", "聊天"},
			},
		},
		repository: stubModelHealthcheckRepository{
			credentialsErrByID: map[string]error{
				"provider_missing_secret": errors.New("missing secret"),
			},
		},
	}

	routes, err := catalog.ListRunnableRoutes(context.Background())
	if err != nil {
		t.Fatalf("ListRunnableRoutes returned error: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected missing-secret route to be skipped, got %+v", routes)
	}
}

func TestPostgresModelHealthcheckCatalogUpdateRouteHealthPersistsHistory(t *testing.T) {
	db := &stubModelHealthcheckUpdateDB{
		queryRowValues: map[string][]any{
			"select": {"qwen-flash", "provider_good", "Qwen 主线路", "聊天"},
		},
	}
	catalog := postgresModelHealthcheckCatalog{db: db}

	update := ModelHealthcheckUpdate{
		LastHealthCheckedAt: time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC),
		HealthStatus:        "healthy",
		LastHealthError:     "",
		FirstTokenLatencyMS: 82,
		LatencyMS:           218,
	}
	if err := catalog.UpdateRouteHealth(context.Background(), "route:provider_good:qwen-flash", update); err != nil {
		t.Fatalf("UpdateRouteHealth returned error: %v", err)
	}
	if len(db.execs) != 2 {
		t.Fatalf("expected 2 exec calls(update+history insert), got %d", len(db.execs))
	}
	if !strings.Contains(db.execs[1].query, "insert into model_healthcheck_history") {
		t.Fatalf("expected second exec to insert history, got query %q", db.execs[1].query)
	}
	if got := db.execs[1].args[2]; got != "qwen-flash" {
		t.Fatalf("expected requested_model argument, got %#v", got)
	}
	if got := db.execs[1].args[5]; got != "healthy" {
		t.Fatalf("expected normalized health_status healthy, got %#v", got)
	}
	if got := db.execs[1].args[8]; got != int64(218) {
		t.Fatalf("expected latency argument 218, got %#v", got)
	}
}

type stubModelHealthcheckRepository struct {
	listErr            error
	credentialsByID    map[string]store.ProviderCredentialRecord
	credentialsErrByID map[string]error
}

func (s stubModelHealthcheckRepository) FindPlatformAPIKeyByHash(context.Context, string) (store.PlatformAPIKeyRecord, error) {
	return store.PlatformAPIKeyRecord{}, store.ErrAuthRecordNotFound
}

func (s stubModelHealthcheckRepository) FindTenantByID(context.Context, string) (store.TenantRecord, error) {
	return store.TenantRecord{}, store.ErrAuthRecordNotFound
}

func (s stubModelHealthcheckRepository) ListActiveProviderCredentials(context.Context) ([]store.ProviderCredentialRecord, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	items := make([]store.ProviderCredentialRecord, 0, len(s.credentialsByID))
	for _, credential := range s.credentialsByID {
		items = append(items, credential)
	}
	return items, nil
}

func (s stubModelHealthcheckRepository) ResolveProviderCredential(_ context.Context, id string) (store.ProviderCredentialRecord, error) {
	if err, ok := s.credentialsErrByID[id]; ok {
		return store.ProviderCredentialRecord{}, err
	}
	credential, ok := s.credentialsByID[id]
	if !ok {
		return store.ProviderCredentialRecord{}, store.ErrAuthRecordNotFound
	}
	return credential, nil
}

type stubModelHealthcheckQueryDB struct {
	rows [][]any
	err  error
}

func (s *stubModelHealthcheckQueryDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &stubModelHealthcheckRows{rows: s.rows}, nil
}

func (s *stubModelHealthcheckQueryDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func (s *stubModelHealthcheckQueryDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec call")
}

type stubModelHealthcheckExecCall struct {
	query string
	args  []any
}

type stubModelHealthcheckUpdateDB struct {
	execs          []stubModelHealthcheckExecCall
	queryRowValues map[string][]any
}

func (s *stubModelHealthcheckUpdateDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (s *stubModelHealthcheckUpdateDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	row := s.queryRowValues["select"]
	if strings.Contains(query, "from route_catalog") {
		return stubModelHealthcheckRow{values: row}
	}
	return stubModelHealthcheckRow{err: errors.New("unexpected QueryRow call")}
}

func (s *stubModelHealthcheckUpdateDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.execs = append(s.execs, stubModelHealthcheckExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	return pgconn.CommandTag{}, nil
}

type stubModelHealthcheckRow struct {
	values []any
	err    error
}

func (s stubModelHealthcheckRow) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.values) {
		return errors.New("scan dest/value length mismatch")
	}
	for index := range dest {
		switch target := dest[index].(type) {
		case *string:
			value, ok := s.values[index].(string)
			if !ok {
				return errors.New("unsupported row value type")
			}
			*target = value
		default:
			return errors.New("unsupported scan dest")
		}
	}
	return nil
}

type stubModelHealthcheckRows struct {
	rows   [][]any
	index  int
	closed bool
}

func (s *stubModelHealthcheckRows) Close()                                       { s.closed = true }
func (s *stubModelHealthcheckRows) Err() error                                   { return nil }
func (s *stubModelHealthcheckRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (s *stubModelHealthcheckRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (s *stubModelHealthcheckRows) Next() bool {
	if s.index >= len(s.rows) {
		s.closed = true
		return false
	}
	s.index++
	return true
}
func (s *stubModelHealthcheckRows) Scan(dest ...any) error {
	row := s.rows[s.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		default:
			return errors.New("unsupported scan dest")
		}
	}
	return nil
}
func (s *stubModelHealthcheckRows) Values() ([]any, error) { return s.rows[s.index-1], nil }
func (s *stubModelHealthcheckRows) RawValues() [][]byte    { return nil }
func (s *stubModelHealthcheckRows) Conn() *pgx.Conn        { return nil }
