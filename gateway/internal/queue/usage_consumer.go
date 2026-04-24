package queue

import "context"

type UsageConsumer interface {
	Consume(ctx context.Context, event UsageEvent) error
}

type UsageConsumerFunc func(ctx context.Context, event UsageEvent) error

type noopUsageConsumer struct{}

type usageConsumingPublisher struct {
	publisher UsagePublisher
	consumers []UsageConsumer
}

func NewNoopUsageConsumer() UsageConsumer {
	return noopUsageConsumer{}
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

func (p usageConsumingPublisher) Publish(ctx context.Context, event UsageEvent) error {
	if err := p.publisher.Publish(ctx, event); err != nil {
		return err
	}
	for _, consumer := range p.consumers {
		if err := consumer.Consume(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
