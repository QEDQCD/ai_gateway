package queue

import (
	"context"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type AMQPPublisher struct {
	url string

	mu   sync.Mutex
	conn amqpConnection
	dial func(string) (amqpConnection, error)
}

type amqpConnection interface {
	IsClosed() bool
	Channel() (amqpChannel, error)
	Close() error
}

type amqpChannel interface {
	QueueDeclare(name string, durable bool, autoDelete bool, exclusive bool, noWait bool, args amqp.Table) (amqp.Queue, error)
	PublishWithContext(context.Context, string, string, bool, bool, amqp.Publishing) error
	Close() error
}

func NewAMQPPublisher(url string) (*AMQPPublisher, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, amqp.ErrClosed
	}
	return &AMQPPublisher{
		url:  url,
		dial: dialAMQPConnection,
	}, nil
}

func (p *AMQPPublisher) Publish(ctx context.Context, exchange string, routingKey string, body []byte) error {
	conn, err := p.ensureConnection()
	if err != nil {
		return err
	}

	channel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() {
		_ = channel.Close()
	}()

	if exchange == "" && routingKey != "" {
		if _, err := channel.QueueDeclare(routingKey, true, false, false, false, nil); err != nil {
			return err
		}
	}

	return channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
	})
}

func (p *AMQPPublisher) ensureConnection() (amqpConnection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn == nil || p.conn.IsClosed() {
		conn, err := p.dial(p.url)
		if err != nil {
			return nil, err
		}
		p.conn = conn
	}
	return p.conn, nil
}

func (p *AMQPPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p == nil || p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	return err
}

type realAMQPConnection struct {
	conn *amqp.Connection
}

func dialAMQPConnection(url string) (amqpConnection, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	return &realAMQPConnection{conn: conn}, nil
}

func (c *realAMQPConnection) IsClosed() bool {
	return c.conn.IsClosed()
}

func (c *realAMQPConnection) Channel() (amqpChannel, error) {
	channel, err := c.conn.Channel()
	if err != nil {
		return nil, err
	}
	return &realAMQPChannel{channel: channel}, nil
}

func (c *realAMQPConnection) Close() error {
	return c.conn.Close()
}

type realAMQPChannel struct {
	channel *amqp.Channel
}

func (c *realAMQPChannel) QueueDeclare(name string, durable bool, autoDelete bool, exclusive bool, noWait bool, args amqp.Table) (amqp.Queue, error) {
	return c.channel.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args)
}

func (c *realAMQPChannel) PublishWithContext(ctx context.Context, exchange string, routingKey string, mandatory bool, immediate bool, publishing amqp.Publishing) error {
	return c.channel.PublishWithContext(ctx, exchange, routingKey, mandatory, immediate, publishing)
}

func (c *realAMQPChannel) Close() error {
	return c.channel.Close()
}
