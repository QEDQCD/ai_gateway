package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewUsageRecorderRequiresTransactionalDB(t *testing.T) {
	t.Parallel()

	var ctor any = service.NewUsageRecorder
	if _, ok := ctor.(func(store.DBTX) service.UsageRecorder); ok {
		t.Fatal("expected NewUsageRecorder to require transaction support instead of accepting bare store.DBTX")
	}
}

func TestUsageRecorderRecordWritesSuccessLifecycleEvent(t *testing.T) {
	t.Parallel()

	db := newRecordingTxDB()
	recorder := service.NewUsageRecorder(db)
	record := newUsageRecord(service.UsageStatusSuccess, service.UsageSourceUpstream)

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("recorder.Record failed: %v", err)
	}

	if db.beginCalls != 1 {
		t.Fatalf("expected 1 begin call, got %d", db.beginCalls)
	}
	if db.tx.commitCalls != 1 {
		t.Fatalf("expected 1 commit call, got %d", db.tx.commitCalls)
	}
	if db.tx.rollbackCalls != 0 {
		t.Fatalf("expected 0 rollback calls, got %d", db.tx.rollbackCalls)
	}
	if len(db.execCalls) != 0 {
		t.Fatalf("expected outer db exec calls to stay at 0, got %d", len(db.execCalls))
	}
	if len(db.tx.execCalls) != 2 {
		t.Fatalf("expected 2 tx exec calls, got %d", len(db.tx.execCalls))
	}
	if !strings.Contains(db.tx.execCalls[0].query, "llm_request_logs") {
		t.Fatalf("expected first exec to insert llm_request_logs, got %q", db.tx.execCalls[0].query)
	}
	if !strings.Contains(db.tx.execCalls[1].query, "llm_request_events") {
		t.Fatalf("expected second exec to insert llm_request_events, got %q", db.tx.execCalls[1].query)
	}
	if got := db.tx.execCalls[1].args[3]; got != "response_received" {
		t.Fatalf("expected lifecycle event_type %q, got %#v", "response_received", got)
	}
}

func TestUsageRecorderRecordWritesFailureLifecycleEvent(t *testing.T) {
	t.Parallel()

	db := newRecordingTxDB()
	recorder := service.NewUsageRecorder(db)
	record := newUsageRecord(service.UsageStatusFailed, service.UsageSourceEstimated)
	record.StatusCode = 502
	record.ErrorMessage = "upstream bad gateway"

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("recorder.Record failed: %v", err)
	}

	if db.beginCalls != 1 {
		t.Fatalf("expected 1 begin call, got %d", db.beginCalls)
	}
	if db.tx.commitCalls != 1 {
		t.Fatalf("expected 1 commit call, got %d", db.tx.commitCalls)
	}
	if len(db.tx.execCalls) != 2 {
		t.Fatalf("expected 2 tx exec calls, got %d", len(db.tx.execCalls))
	}
	if got := db.tx.execCalls[1].args[3]; got != "request_failed" {
		t.Fatalf("expected lifecycle event_type %q, got %#v", "request_failed", got)
	}
	if got := db.tx.execCalls[1].args[7]; got == "" {
		t.Fatal("expected lifecycle failure detail to be populated")
	}
}

func TestUsageRecorderRecordUsesIndependentContextWhenParentContextIsDone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		parentCtx func(t *testing.T) context.Context
	}{
		{
			name: "canceled",
			parentCtx: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name: "deadline_exceeded",
			parentCtx: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				t.Cleanup(cancel)
				time.Sleep(10 * time.Millisecond)
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatalf("expected parent context deadline to expire, got %v", ctx.Err())
				}
				return ctx
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newRecordingTxDB()
			recorder := service.NewUsageRecorder(db)
			record := newUsageRecord(service.UsageStatusSuccess, service.UsageSourceUpstream)

			if err := recorder.Record(tc.parentCtx(t), record); err != nil {
				t.Fatalf("recorder.Record failed: %v", err)
			}

			if db.beginCalls != 1 {
				t.Fatalf("expected 1 begin call, got %d", db.beginCalls)
			}
			assertActiveBoundedContext(t, db.beginCtxs[0])
			if len(db.tx.execCtxs) != 2 {
				t.Fatalf("expected 2 tx exec calls, got %d", len(db.tx.execCtxs))
			}
			for _, ctx := range db.tx.execCtxs {
				assertActiveBoundedContext(t, ctx)
			}
			if len(db.tx.commitCtxs) != 1 {
				t.Fatalf("expected 1 commit call, got %d", len(db.tx.commitCtxs))
			}
			assertActiveBoundedContext(t, db.tx.commitCtxs[0])
		})
	}
}

func TestUsageRecorderRecordRollsBackWhenLifecycleEventInsertFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	pool, err := gatewaydb.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("pool.Exec seed failed: %v", err)
		}
	}

	if _, err := pool.Exec(ctx, `
		create or replace function fail_usage_event_insert() returns trigger as $$
		begin
			raise exception 'forced lifecycle event insert failure';
		end;
		$$ language plpgsql;

		create trigger fail_usage_event_insert
		before insert on llm_request_events
		for each row execute function fail_usage_event_insert();
	`); err != nil {
		t.Fatalf("pool.Exec trigger setup failed: %v", err)
	}

	recorder := service.NewUsageRecorder(pool)
	record := newUsageRecord(service.UsageStatusSuccess, service.UsageSourceUpstream)
	record.RequestID = "llmreq_tx_rollback"

	err = recorder.Record(ctx, record)
	if err == nil || !strings.Contains(err.Error(), "forced lifecycle event insert failure") {
		t.Fatalf("expected lifecycle event insert failure, got %v", err)
	}

	var logCount int
	if err := pool.QueryRow(ctx, `select count(*) from llm_request_logs where id = $1`, record.RequestID).Scan(&logCount); err != nil {
		t.Fatalf("QueryRow llm_request_logs count failed: %v", err)
	}
	if logCount != 0 {
		t.Fatalf("expected request log rollback on lifecycle event failure, got %d rows", logCount)
	}

	var eventCount int
	if err := pool.QueryRow(ctx, `select count(*) from llm_request_events where request_log_id = $1`, record.RequestID).Scan(&eventCount); err != nil {
		t.Fatalf("QueryRow llm_request_events count failed: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected no lifecycle events after rollback, got %d rows", eventCount)
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

type recordingTxDB struct {
	execCalls  []execCall
	beginCalls int
	beginErr   error
	beginCtxs  []contextSnapshot
	tx         *recordingTx
}

func newRecordingTxDB() *recordingTxDB {
	return &recordingTxDB{tx: &recordingTx{}}
}

func (db *recordingTxDB) Begin(ctx context.Context) (pgx.Tx, error) {
	db.beginCalls++
	db.beginCtxs = append(db.beginCtxs, captureContextSnapshot(ctx))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	return db.tx, nil
}

func (db *recordingTxDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.execCalls = append(db.execCalls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	return pgconn.CommandTag{}, nil
}

func (db *recordingTxDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (db *recordingTxDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

type recordingTx struct {
	execCalls     []execCall
	execCtxs      []contextSnapshot
	commitCalls   int
	commitCtxs    []contextSnapshot
	rollbackCalls int
	rollbackCtxs  []contextSnapshot
	execErrAtCall map[int]error
}

func (tx *recordingTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested Begin call")
}

func (tx *recordingTx) Commit(ctx context.Context) error {
	tx.commitCalls++
	tx.commitCtxs = append(tx.commitCtxs, captureContextSnapshot(ctx))
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (tx *recordingTx) Rollback(ctx context.Context) error {
	tx.rollbackCalls++
	tx.rollbackCtxs = append(tx.rollbackCtxs, captureContextSnapshot(ctx))
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (tx *recordingTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom call")
}

func (tx *recordingTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch call")
}

func (tx *recordingTx) LargeObjects() pgx.LargeObjects {
	panic("unexpected LargeObjects call")
}

func (tx *recordingTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare call")
}

func (tx *recordingTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.execCtxs = append(tx.execCtxs, captureContextSnapshot(ctx))
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.execCalls = append(tx.execCalls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	callIndex := len(tx.execCalls)
	if err := tx.execErrAtCall[callIndex]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.CommandTag{}, nil
}

func (tx *recordingTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (tx *recordingTx) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (tx *recordingTx) Conn() *pgx.Conn {
	return nil
}

var _ pgx.Tx = (*recordingTx)(nil)
var _ interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
} = (*recordingTxDB)(nil)

type contextSnapshot struct {
	err         error
	hasDeadline bool
	deadline    time.Time
}

func captureContextSnapshot(ctx context.Context) contextSnapshot {
	if ctx == nil {
		return contextSnapshot{}
	}
	deadline, hasDeadline := ctx.Deadline()
	return contextSnapshot{
		err:         ctx.Err(),
		hasDeadline: hasDeadline,
		deadline:    deadline,
	}
}

func assertActiveBoundedContext(t *testing.T, snapshot contextSnapshot) {
	t.Helper()

	if snapshot.err != nil {
		t.Fatalf("expected active context at call time, got %v", snapshot.err)
	}
	if !snapshot.hasDeadline {
		t.Fatal("expected context deadline to be set")
	}
	if remaining := time.Until(snapshot.deadline); remaining <= 0 || remaining > 5*time.Second {
		t.Fatalf("expected bounded context deadline, got %s remaining", remaining)
	}
}
