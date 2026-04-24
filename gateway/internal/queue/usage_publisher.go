package queue

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type UsageEvent struct {
	RequestID            string    `json:"request_id"`
	TenantID             string    `json:"tenant_id"`
	PlatformAPIKeyID     string    `json:"platform_api_key_id"`
	PlatformAPIKeyName   string    `json:"platform_api_key_name"`
	ProviderCredentialID string    `json:"provider_credential_id"`
	RouteID              string    `json:"route_id"`
	Provider             string    `json:"provider"`
	Model                string    `json:"model"`
	Status               string    `json:"status"`
	UsageSource          string    `json:"usage_source"`
	PromptTokens         int       `json:"prompt_tokens"`
	CompletionTokens     int       `json:"completion_tokens"`
	TotalTokens          int       `json:"total_tokens"`
	Endpoint             string    `json:"endpoint"`
	StatusCode           int       `json:"status_code"`
	LatencyMS            int64     `json:"latency_ms"`
	OccurredAt           time.Time `json:"occurred_at"`
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
