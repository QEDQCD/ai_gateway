package queue

import (
	"context"
	"encoding/json"
	"sync"
)

type UsageEvent struct {
	TenantID             string `json:"tenant_id"`
	PlatformAPIKeyID     string `json:"platform_api_key_id"`
	ProviderCredentialID string `json:"provider_credential_id"`
	Endpoint             string `json:"endpoint"`
	StatusCode           int    `json:"status_code"`
	LatencyMS            int64  `json:"latency_ms"`
}

type UsagePublisher interface {
	Publish(ctx context.Context, event UsageEvent) error
}

type RabbitMQMessagePublisher interface {
	Publish(ctx context.Context, exchange string, routingKey string, body []byte) error
}

type noopUsagePublisher struct{}

type noopRabbitMQMessagePublisher struct{}

type RabbitMQUsagePublisher struct {
	publisher  RabbitMQMessagePublisher
	exchange   string
	routingKey string
}

type RecordingUsagePublisher struct {
	mu     sync.Mutex
	events []UsageEvent
}

func NewNoopUsagePublisher() UsagePublisher {
	return noopUsagePublisher{}
}

func NewRecordingUsagePublisher() *RecordingUsagePublisher {
	return &RecordingUsagePublisher{}
}

func NewNoopRabbitMQMessagePublisher() RabbitMQMessagePublisher {
	return noopRabbitMQMessagePublisher{}
}

func NewRabbitMQUsagePublisher(publisher RabbitMQMessagePublisher, exchange string, routingKey string) UsagePublisher {
	if publisher == nil {
		return noopUsagePublisher{}
	}
	return RabbitMQUsagePublisher{
		publisher:  publisher,
		exchange:   exchange,
		routingKey: routingKey,
	}
}

func (noopUsagePublisher) Publish(context.Context, UsageEvent) error {
	return nil
}

func (noopRabbitMQMessagePublisher) Publish(context.Context, string, string, []byte) error {
	return nil
}

func (p RabbitMQUsagePublisher) Publish(ctx context.Context, event UsageEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.publisher.Publish(ctx, p.exchange, p.routingKey, body)
}

func (p *RecordingUsagePublisher) Publish(_ context.Context, event UsageEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.events = append(p.events, event)
	return nil
}

func (p *RecordingUsagePublisher) Events() []UsageEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]UsageEvent(nil), p.events...)
}
