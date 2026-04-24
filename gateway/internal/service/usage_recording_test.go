package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func TestUsageRecorderRecordWritesSuccessLifecycleEvent(t *testing.T) {
	t.Parallel()

	db := &recordingDB{}
	recorder := service.NewUsageRecorder(db)
	record := newUsageRecord(service.UsageStatusSuccess, service.UsageSourceUpstream)

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("recorder.Record failed: %v", err)
	}

	if len(db.execCalls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(db.execCalls))
	}
	if !strings.Contains(db.execCalls[0].query, "llm_request_logs") {
		t.Fatalf("expected first exec to insert llm_request_logs, got %q", db.execCalls[0].query)
	}
	if !strings.Contains(db.execCalls[1].query, "llm_request_events") {
		t.Fatalf("expected second exec to insert llm_request_events, got %q", db.execCalls[1].query)
	}
	if got := db.execCalls[1].args[3]; got != "response_received" {
		t.Fatalf("expected lifecycle event_type %q, got %#v", "response_received", got)
	}
}

func TestUsageRecorderRecordWritesFailureLifecycleEvent(t *testing.T) {
	t.Parallel()

	db := &recordingDB{}
	recorder := service.NewUsageRecorder(db)
	record := newUsageRecord(service.UsageStatusFailed, service.UsageSourceEstimated)
	record.StatusCode = 502
	record.ErrorMessage = "upstream bad gateway"

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("recorder.Record failed: %v", err)
	}

	if len(db.execCalls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(db.execCalls))
	}
	if got := db.execCalls[1].args[3]; got != "request_failed" {
		t.Fatalf("expected lifecycle event_type %q, got %#v", "request_failed", got)
	}
	if got := db.execCalls[1].args[7]; got == "" {
		t.Fatal("expected lifecycle failure detail to be populated")
	}
}

func newUsageRecord(status service.UsageStatus, source service.UsageSource) service.UsageRecord {
	startedAt := time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(250 * time.Millisecond)

	return service.UsageRecord{
		RequestID:            "llmreq_test_001",
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		PlatformAPIKeyName:   "demo key",
		ProviderCredentialID: "provider_openai_demo",
		Provider:             "openai",
		RouteID:              "route:provider_openai_demo:default",
		RequestPath:          "/v1/chat/completions",
		RequestModel:         "gpt-4o-mini",
		UpstreamModel:        "gpt-4o-mini",
		Status:               status,
		UsageSource:          source,
		StatusCode:           200,
		LatencyMS:            250,
		PromptTokens:         10,
		CompletionTokens:     5,
		TotalTokens:          15,
		RequestStartedAt:     startedAt,
		RequestCompletedAt:   completedAt,
	}
}

type recordingDB struct {
	execCalls []execCall
}

type execCall struct {
	query string
	args  []any
}

func (db *recordingDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.execCalls = append(db.execCalls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	return pgconn.CommandTag{}, nil
}

func (db *recordingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (db *recordingDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}
