package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failingPublisher struct {
	err error
}

func (p failingPublisher) Publish(context.Context, UsageEvent) error {
	return p.err
}

func TestUsagePublisherWithConsumersSkipsRegularConsumersAfterPublishFailure(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("publish failed")
	consumerErr := errors.New("regular consumer should be skipped")
	consumerCalled := false
	publisher := NewUsagePublisherWithConsumers(
		failingPublisher{err: publishErr},
		UsageConsumerFunc(func(ctx context.Context, event UsageEvent) error {
			consumerCalled = true
			return consumerErr
		}),
	)

	err := publisher.Publish(context.Background(), UsageEvent{})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error %v, got %v", publishErr, err)
	}
	if errors.Is(err, consumerErr) {
		t.Fatalf("expected regular consumer to be skipped after publish failure, got %v", err)
	}
	if consumerCalled {
		t.Fatal("expected regular consumer not to run after publish failure")
	}
}

func TestUsagePublisherWithConsumersSeparatesConsumerFailureFromPublishFailure(t *testing.T) {
	t.Parallel()

	consumerErr := errors.New("consumer failed")
	publisher := NewUsagePublisherWithConsumers(
		NewNoopUsagePublisher(),
		UsageConsumerFunc(func(context.Context, UsageEvent) error {
			return consumerErr
		}),
	)

	err := publisher.Publish(context.Background(), UsageEvent{})
	if !errors.Is(err, consumerErr) {
		t.Fatalf("expected consumer error %v, got %v", consumerErr, err)
	}
	if publishErr := PublishFailure(err); publishErr != nil {
		t.Fatalf("expected consumer-only failure not to classify as publish failure, got %v", publishErr)
	}
}

func TestUsagePublisherWithConsumersPreservesPublishFailureWhenFallbackConsumerAlsoFails(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("publish failed")
	consumerErr := errors.New("fallback consumer failed")
	publisher := NewUsagePublisherWithConsumers(
		failingPublisher{err: publishErr},
		WithPublishFailureTimeout(
			UsageConsumerFunc(func(context.Context, UsageEvent) error {
				return consumerErr
			}),
			50*time.Millisecond,
		),
	)

	err := publisher.Publish(context.Background(), UsageEvent{})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected returned error to include publish error %v, got %v", publishErr, err)
	}
	if !errors.Is(err, consumerErr) {
		t.Fatalf("expected returned error to include consumer error %v, got %v", consumerErr, err)
	}
	if got := PublishFailure(err); !errors.Is(got, publishErr) {
		t.Fatalf("expected PublishFailure to return %v, got %v", publishErr, got)
	}
}

func TestUsagePublisherWithConsumersUsesIndependentFallbackDeadlineAfterPublishFailure(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("publish failed")
	publisher := NewUsagePublisherWithConsumers(
		failingPublisher{err: publishErr},
		WithPublishFailureTimeout(
			UsageConsumerFunc(func(ctx context.Context, event UsageEvent) error {
				if ctx.Err() != nil {
					return errors.New("fallback context should start active")
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("fallback context should have its own deadline")
				}
				if remaining := time.Until(deadline); remaining <= 0 || remaining > 100*time.Millisecond {
					return errors.New("fallback deadline should be short and independent")
				}
				<-ctx.Done()
				return ctx.Err()
			}),
			20*time.Millisecond,
		),
	)

	requestCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	err := publisher.Publish(requestCtx, UsageEvent{})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error %v, got %v", publishErr, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected fallback consumer to stop on its own deadline, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("expected bounded fallback consumer to finish quickly, took %s", elapsed)
	}
}

func TestUsagePublisherWithConsumersUsesIndependentDeadlineForWrappedConsumerOnNormalPath(t *testing.T) {
	t.Parallel()

	consumerCalled := false
	publisher := NewUsagePublisherWithConsumers(
		NewNoopUsagePublisher(),
		WithPublishFailureTimeout(
			UsageConsumerFunc(func(ctx context.Context, event UsageEvent) error {
				consumerCalled = true
				if ctx.Err() != nil {
					return errors.New("wrapped consumer should ignore canceled parent context")
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("wrapped consumer should have its own deadline")
				}
				if remaining := time.Until(deadline); remaining <= 0 || remaining > time.Second {
					return errors.New("wrapped consumer deadline should be independently bounded")
				}
				return nil
			}),
			50*time.Millisecond,
		),
	)

	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := publisher.Publish(requestCtx, UsageEvent{}); err != nil {
		t.Fatalf("expected wrapped consumer on normal path to succeed, got %v", err)
	}
	if !consumerCalled {
		t.Fatal("expected wrapped consumer to run on normal publish path")
	}
}

func TestUsagePublisherWithConsumersFallbackIgnoresCanceledParentContext(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("publish failed")
	consumerCalled := false
	publisher := NewUsagePublisherWithConsumers(
		failingPublisher{err: publishErr},
		WithPublishFailureTimeout(
			UsageConsumerFunc(func(ctx context.Context, event UsageEvent) error {
				consumerCalled = true
				if ctx.Err() != nil {
					return errors.New("fallback context should ignore parent cancellation")
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("fallback context should keep its own deadline")
				}
				if remaining := time.Until(deadline); remaining <= 0 || remaining > time.Second {
					return errors.New("fallback deadline should be independently bounded")
				}
				return nil
			}),
			50*time.Millisecond,
		),
	)

	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := publisher.Publish(requestCtx, UsageEvent{})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error %v, got %v", publishErr, err)
	}
	if !consumerCalled {
		t.Fatal("expected fallback consumer to run after parent context cancellation")
	}
}

func TestWithPublishFailureTimeoutPanicsOnNonPositiveTimeout(t *testing.T) {
	t.Parallel()

	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for timeout %s", timeout)
				}
			}()

			WithPublishFailureTimeout(NewNoopUsageConsumer(), timeout)
		})
	}
}
