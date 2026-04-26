package queue

import (
	"context"
	"errors"
	"time"
)

type UsageConsumer interface {
	Consume(ctx context.Context, event UsageEvent) error
}

type UsageConsumerFunc func(ctx context.Context, event UsageEvent) error

type noopUsageConsumer struct{}

type publishFailureConsumer interface {
	UsageConsumer
	ConsumeAfterPublishFailure(ctx context.Context, event UsageEvent) error
}

type publishFailureTimeoutConsumer struct {
	consumer UsageConsumer
	timeout  time.Duration
}

type publishFailureError struct {
	err error
}

type usageConsumingPublisher struct {
	publisher UsagePublisher
	consumers []UsageConsumer
}

func NewNoopUsageConsumer() UsageConsumer {
	return noopUsageConsumer{}
}

func PublishFailure(err error) error {
	var publishErr publishFailureError
	if errors.As(err, &publishErr) {
		return publishErr.err
	}
	return nil
}

func WithPublishFailureTimeout(consumer UsageConsumer, timeout time.Duration) UsageConsumer {
	if consumer == nil {
		return nil
	}
	if timeout <= 0 {
		panic("queue: publish failure timeout must be positive")
	}
	return publishFailureTimeoutConsumer{
		consumer: consumer,
		timeout:  timeout,
	}
}

func NewUsagePublisherWithConsumers(publisher UsagePublisher, consumers ...UsageConsumer) UsagePublisher {
	if publisher == nil {
		publisher = NewNoopUsagePublisher()
	}
	filtered := make([]UsageConsumer, 0, len(consumers))
	for _, consumer := range consumers {
		if consumer != nil {
			filtered = append(filtered, consumer)
		}
	}
	if len(filtered) == 0 {
		return publisher
	}
	return usageConsumingPublisher{
		publisher: publisher,
		consumers: filtered,
	}
}

func (f UsageConsumerFunc) Consume(ctx context.Context, event UsageEvent) error {
	return f(ctx, event)
}

func (noopUsageConsumer) Consume(context.Context, UsageEvent) error {
	return nil
}

func (e publishFailureError) Error() string {
	return e.err.Error()
}

func (e publishFailureError) Unwrap() error {
	return e.err
}

func (c publishFailureTimeoutConsumer) Consume(ctx context.Context, event UsageEvent) error {
	return c.consumeWithIndependentTimeout(ctx, event)
}

func (c publishFailureTimeoutConsumer) ConsumeAfterPublishFailure(ctx context.Context, event UsageEvent) error {
	return c.consumeWithIndependentTimeout(ctx, event)
}

func (c publishFailureTimeoutConsumer) consumeWithIndependentTimeout(ctx context.Context, event UsageEvent) error {
	fallbackCtx := context.Background()
	if ctx != nil {
		fallbackCtx = context.WithoutCancel(ctx)
	}
	var cancel context.CancelFunc
	fallbackCtx, cancel = context.WithTimeout(fallbackCtx, c.timeout)
	defer cancel()
	return c.consumer.Consume(fallbackCtx, event)
}

func (p usageConsumingPublisher) Publish(ctx context.Context, event UsageEvent) error {
	publishErr := p.publisher.Publish(ctx, event)

	var errs []error
	if publishErr != nil {
		errs = append(errs, publishFailureError{err: publishErr})
	}
	for _, consumer := range p.consumers {
		var err error
		if publishErr != nil {
			if fallbackConsumer, ok := consumer.(publishFailureConsumer); ok {
				err = fallbackConsumer.ConsumeAfterPublishFailure(ctx, event)
			} else {
				continue
			}
		} else {
			err = consumer.Consume(ctx, event)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
