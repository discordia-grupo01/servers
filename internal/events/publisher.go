package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/discordia-grupo01/events-contract/broker"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/servers/internal/logging"
	"github.com/discordia-grupo01/servers/internal/telemetry"
)

// dialTimeout bounds how long a connection attempt can take. Publish() may
// call connect() synchronously from an HTTP handler's goroutine, so this
// needs to be short: amqp.Dial's own default (no Config.Dial) falls back to
// a 30s net.DialTimeout, which would be a very noticeable stall on a
// request whenever RabbitMQ is unreachable in a way that doesn't fail fast
// (network partition, dropped packets) rather than actively refusing the
// connection.
const dialTimeout = 3 * time.Second

// ConnectionName is set as the client_properties.connection_name on every
// AMQP connection this publisher opens -- shows up in the RabbitMQ
// management UI/API, which also lets tests find and force-close exactly
// this publisher's connection without touching anyone else's.
const ConnectionName = "discordia-servers-events-publisher"

type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

type AMQPPublisher struct {
	url string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewAMQPPublisher(url string) *AMQPPublisher {
	p := &AMQPPublisher{url: url}
	if url == "" {
		logging.Warnf("events: RABBITMQ_URL not set, publisher disabled")
		return p
	}
	if err := p.connect(); err != nil {
		logging.Warnf("events: warning: initial RabbitMQ connection failed, will retry on next publish: %v", err)
	}
	return p
}

func (p *AMQPPublisher) connect() error {
	props := amqp.NewConnectionProperties()
	props.SetClientConnectionName(ConnectionName)
	conn, err := amqp.DialConfig(p.url, amqp.Config{Dial: amqp.DefaultDial(dialTimeout), Properties: props})
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	if err := ch.ExchangeDeclare(broker.ExchangeName, broker.ExchangeType, true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	p.mu.Lock()
	p.conn = conn
	p.ch = ch
	p.mu.Unlock()
	return nil
}

func (p *AMQPPublisher) Publish(ctx context.Context, routingKey string, payload any) error {
	if err := p.publish(ctx, routingKey, payload); err != nil {
		logging.Warnf("events: warning: could not publish %q: %v", routingKey, err)
	}
	return nil
}

func (p *AMQPPublisher) PublishOrError(ctx context.Context, routingKey string, payload any) error {
	return p.publish(ctx, routingKey, payload)
}

func (p *AMQPPublisher) publish(ctx context.Context, routingKey string, payload any) error {
	if p.url == "" {
		return nil
	}

	ch := p.currentChannel()
	if ch == nil {
		if err := p.connect(); err != nil {
			return fmt.Errorf("rabbitmq unreachable: %w", err)
		}
		ch = p.currentChannel()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	msg := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Headers:      telemetry.InjectAMQP(ctx, nil),
	}
	if err := ch.PublishWithContext(ctx, broker.ExchangeName, routingKey, false, false, msg); err != nil {
		p.dropChannel()
		if err := p.connect(); err != nil {
			return fmt.Errorf("reconnect after failed publish: %w", err)
		}
		if err := p.currentChannel().PublishWithContext(ctx, broker.ExchangeName, routingKey, false, false, msg); err != nil {
			return fmt.Errorf("publish failed again after reconnect: %w", err)
		}
	}
	return nil
}

func (p *AMQPPublisher) currentChannel() *amqp.Channel {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ch
}

func (p *AMQPPublisher) dropChannel() {
	p.mu.Lock()
	p.ch = nil
	p.mu.Unlock()
}
