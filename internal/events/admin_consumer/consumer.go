// Package admin_consumer keeps the suspended_users replica up to date by
// consuming admin's admin.user_suspended and admin.user_reactivated events from
// discordia.events. These events lock people out, so every one is checked
// against the HMAC secret shared with admin before it changes anything.
package admin_consumer

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/broker"
	"github.com/discordia-grupo01/events-contract/envelope"
	amqp "github.com/rabbitmq/amqp091-go"
)

// QueueName is owned by this consumer: no other service binds it. Another
// service that needs the suspensions declares its own queue on the same
// routing keys.
const QueueName = "servers.admin_events"

// ConnectionName shows up in the RabbitMQ management UI/API, which also lets
// tests find and force-close exactly this consumer's connection.
const ConnectionName = "discordia-servers-admin-events"

// Store is where the replica is written. Apply must store the state only if
// version is newer than the one stored, and report whether it did.
type Store interface {
	Apply(ctx context.Context, userID string, suspended bool, version int64) (bool, error)
}

type Consumer struct {
	url    string
	secret string
	store  Store
}

func New(url, secret string, store Store) *Consumer {
	return &Consumer{url: url, secret: secret, store: store}
}

// Start consumes until ctx is cancelled, reconnecting with exponential
// backoff (1s up to 30s) whenever the connection to RabbitMQ drops.
func (c *Consumer) Start(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for ctx.Err() == nil {
		if err := c.consumeOnce(ctx); err != nil {
			log.Printf("admin_consumer: RabbitMQ connection lost, retrying in %s: %v", backoff, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = time.Second
	}
}

func (c *Consumer) consumeOnce(ctx context.Context) error {
	props := amqp.NewConnectionProperties()
	props.SetClientConnectionName(ConnectionName)
	conn, err := amqp.DialConfig(c.url, amqp.Config{Properties: props})
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(broker.ExchangeName, broker.ExchangeType, true, false, false, false, nil); err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		return err
	}
	for _, key := range []string{admin.RoutingKeyUserSuspended, admin.RoutingKeyUserReactivated} {
		if err := ch.QueueBind(queue.Name, key, broker.ExchangeName, false, nil); err != nil {
			return err
		}
	}

	// Manual ack: a message is only confirmed once processed, so a transient
	// database failure never loses an event.
	messages, err := ch.Consume(queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	log.Printf("admin_consumer: consuming %s", QueueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-messages:
			if !ok {
				return errors.New("consume channel closed")
			}
			if ShouldRequeue(ctx, c.store, c.secret, delivery.RoutingKey, delivery.Body) {
				_ = delivery.Nack(false, true)
			} else {
				_ = delivery.Ack(false)
			}
		}
	}
}

// ShouldRequeue processes one message and reports whether it must be retried
// (true) or acknowledged (false). Messages that can never succeed (unknown
// routing key, malformed JSON, missing or invalid signature, empty user_id,
// version < 1) are dropped with a log instead of being retried forever and
// leave the state untouched; only a failure to write the replica is retried.
// A stale or duplicate version is a no-op.
func ShouldRequeue(ctx context.Context, store Store, secret, routingKey string, body []byte) bool {
	var (
		meta      envelope.Meta
		userID    string
		version   int64
		verified  bool
		suspended bool
	)
	switch routingKey {
	case admin.RoutingKeyUserSuspended:
		var evt admin.UserSuspended
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("admin_consumer: malformed payload, dropping message: %v", err)
			return false
		}
		meta, userID, version, verified, suspended = evt.Meta, evt.UserID, evt.Version, evt.Verify(secret), true
	case admin.RoutingKeyUserReactivated:
		var evt admin.UserReactivated
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("admin_consumer: malformed payload, dropping message: %v", err)
			return false
		}
		meta, userID, version, verified, suspended = evt.Meta, evt.UserID, evt.Version, evt.Verify(secret), false
	default:
		log.Printf("admin_consumer: unknown routing key %q, dropping message", routingKey)
		return false
	}

	if !verified {
		log.Printf("admin_consumer: WARN invalid or missing signature, routing_key=%s event_id=%s user_id=%s, dropping message", routingKey, meta.EventID, userID)
		return false
	}
	if userID == "" {
		log.Printf("admin_consumer: empty user_id, event_id=%s, dropping message", meta.EventID)
		return false
	}
	if version < 1 {
		log.Printf("admin_consumer: invalid version %d, event_id=%s user_id=%s, dropping message", version, meta.EventID, userID)
		return false
	}

	applied, err := store.Apply(ctx, userID, suspended, version)
	if err != nil {
		log.Printf("admin_consumer: failed to store suspension, event_id=%s user_id=%s, retrying: %v", meta.EventID, userID, err)
		return true
	}
	if !applied {
		log.Printf("admin_consumer: ignored stale suspension event, event_id=%s user_id=%s version=%d", meta.EventID, userID, version)
		return false
	}
	log.Printf("admin_consumer: applied %s, event_id=%s user_id=%s version=%d", routingKey, meta.EventID, userID, version)
	return false
}
