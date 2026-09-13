package events

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

const exchangeName = "discordia.events"

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
		log.Printf("events: RABBITMQ_URL not set, publisher disabled")
		return p
	}
	if err := p.connect(); err != nil {
		log.Printf("events: warning: initial RabbitMQ connection failed, will retry on next publish: %v", err)
	}
	return p
}

func (p *AMQPPublisher) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	if err := ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil); err != nil {
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
	if p.url == "" {
		return nil
	}

	ch := p.currentChannel()
	if ch == nil {
		if err := p.connect(); err != nil {
			log.Printf("events: warning: could not publish %q, RabbitMQ unreachable: %v", routingKey, err)
			return nil
		}
		ch = p.currentChannel()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("events: warning: could not marshal payload for %q: %v", routingKey, err)
		return nil
	}

	msg := amqp.Publishing{ContentType: "application/json", Body: body}
	if err := ch.PublishWithContext(ctx, exchangeName, routingKey, false, false, msg); err != nil {
		log.Printf("events: warning: publish %q failed, reconnecting: %v", routingKey, err)
		p.dropChannel()
		if err := p.connect(); err != nil {
			log.Printf("events: warning: reconnect after failed publish %q also failed: %v", routingKey, err)
			return nil
		}
		if err := p.currentChannel().PublishWithContext(ctx, exchangeName, routingKey, false, false, msg); err != nil {
			log.Printf("events: warning: publish %q failed again after reconnect: %v", routingKey, err)
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
