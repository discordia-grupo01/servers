package voicebackfill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const dialTimeout = 3 * time.Second
const eventsExchange = "discordia.events"

type AMQPPublisher struct {
	queue string
	mu    sync.Mutex
	conn  *amqp.Connection
	ch    *amqp.Channel
}

func NewAMQPPublisher(url, queue string) (*AMQPPublisher, error) {
	p := &AMQPPublisher{queue: queue}
	if err := p.connect(url); err != nil {
		return nil, fmt.Errorf("connect RabbitMQ: %w", err)
	}
	return p, nil
}

func (p *AMQPPublisher) connect(url string) error {
	props := amqp.NewConnectionProperties()
	props.SetClientConnectionName("discordia-servers-voice-backfill")
	conn, err := amqp.DialConfig(url, amqp.Config{Dial: amqp.DefaultDial(dialTimeout), Properties: props})
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if _, err := ch.QueueDeclarePassive(p.queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("queue %q is not available: %w", p.queue, err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("enable publisher confirms: %w", err)
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *AMQPPublisher) Publish(ctx context.Context, routingKey string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal backfill event: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, eventsExchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
	if err != nil {
		return fmt.Errorf("publish with routing key %q: %w", routingKey, err)
	}
	if confirmation == nil || !confirmation.Wait() {
		return fmt.Errorf("RabbitMQ did not confirm event with routing key %q", routingKey)
	}
	return nil
}

func (p *AMQPPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.ch = nil
	p.conn = nil
}
