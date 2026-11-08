// Package billing_consumer is servers' side of the boost saga: it takes the signed
// events of billing from discordia.events. Accepting a boost changes a server's
// level, so every event is checked against the secret shared with billing first.
package billing_consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/boosts"
	"github.com/discordia-grupo01/events-contract/broker"
	"github.com/discordia-grupo01/servers/internal/events/deadletter"
	"github.com/discordia-grupo01/servers/internal/logging"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/telemetry"
)

const QueueName = "servers.billing_events"

const ConnectionName = "discordia-servers-billing-events"

// Store applies each step in one transaction. Repeating a step must be harmless.
type Store interface {
	Request(ctx context.Context, b model.Boost) error
	Cancel(ctx context.Context, b model.Boost) error
	Remove(ctx context.Context, b model.Boost) error
}

type Consumer struct {
	url    string
	secret string
	store  Store
}

func New(url, secret string, store Store) *Consumer {
	return &Consumer{url: url, secret: secret, store: store}
}

// Start consumes until ctx is cancelled, reconnecting with exponential backoff
// (1s up to 30s) whenever the connection to RabbitMQ drops.
func (c *Consumer) Start(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for ctx.Err() == nil {
		if err := c.consumeOnce(ctx); err != nil {
			logging.Warnf("billing_consumer: RabbitMQ connection lost, retrying in %s: %v", backoff, err)
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
	for _, key := range []string{boosts.RoutingKeyRequested, boosts.RoutingKeyCancelled, boosts.RoutingKeyRemoved} {
		if err := ch.QueueBind(queue.Name, key, broker.ExchangeName, false, nil); err != nil {
			return err
		}
	}

	retrier, err := deadletter.New(conn, queue.Name, deadletter.DefaultPolicy())
	if err != nil {
		return err
	}
	defer retrier.Close()

	messages, err := ch.Consume(queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	logging.Infof("billing_consumer: consuming %s", QueueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-messages:
			if !ok {
				return errors.New("consume channel closed")
			}
			spanCtx, end := telemetry.StartConsumer(ctx, QueueName, delivery)
			requeue := ShouldRequeue(spanCtx, c.store, c.secret, delivery.RoutingKey, delivery.Body)
			retrier.Settle(spanCtx, delivery, requeue)
			end(requeue)
		}
	}
}

// ShouldRequeue processes one message and reports whether it must be retried.
// A message that can never succeed (unknown routing key, malformed JSON, invalid
// signature or ids) is dropped with a log. Only a database failure is retried,
// so a technical problem never rejects a boost nor refunds its points.
func ShouldRequeue(ctx context.Context, store Store, secret, routingKey string, body []byte) bool {
	var apply func(context.Context, model.Boost) error
	switch routingKey {
	case boosts.RoutingKeyRequested:
		apply = store.Request
	case boosts.RoutingKeyCancelled:
		apply = store.Cancel
	case boosts.RoutingKeyRemoved:
		apply = store.Remove
	default:
		logging.Warnf("billing_consumer: unknown routing key %q, dropping message", routingKey)
		return false
	}

	var evt boosts.SagaEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		logging.Warnf("billing_consumer: malformed payload, dropping message: %v", err)
		return false
	}
	if !evt.Verify(routingKey, secret) {
		logging.Warnf("billing_consumer: WARN invalid or missing signature, routing_key=%s event_id=%s boost_id=%s, dropping message", routingKey, evt.EventID, evt.BoostID)
		return false
	}
	boostID, err := uuid.Parse(evt.BoostID)
	if err != nil {
		logging.Warnf("billing_consumer: invalid boost_id %q, event_id=%s, dropping message", evt.BoostID, evt.EventID)
		return false
	}
	serverID, err := uuid.Parse(evt.ServerID)
	if err != nil {
		logging.Warnf("billing_consumer: invalid server_id %q, event_id=%s, dropping message", evt.ServerID, evt.EventID)
		return false
	}
	if evt.UserID == "" {
		logging.Warnf("billing_consumer: empty user_id, event_id=%s, dropping message", evt.EventID)
		return false
	}

	if err := apply(ctx, model.Boost{ID: boostID, ServerID: serverID, UserID: evt.UserID}); err != nil {
		logging.Errorf("billing_consumer: failed to apply %s, event_id=%s boost_id=%s, retrying: %v", routingKey, evt.EventID, boostID, err)
		return true
	}
	logging.Infof("billing_consumer: applied %s, event_id=%s boost_id=%s", routingKey, evt.EventID, boostID)
	return false
}
