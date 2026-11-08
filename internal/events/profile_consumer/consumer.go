// Package profile_consumer keeps the user_profiles replica up to date by
// consuming identify's users.profile_updated events from discordia.events.
package profile_consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/discordia-grupo01/events-contract/broker"
	"github.com/discordia-grupo01/events-contract/users"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/servers/internal/events/deadletter"
	"github.com/discordia-grupo01/servers/internal/logging"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/telemetry"
)

// QueueName is owned by this consumer: no other service binds it. Another
// service that needs profiles declares its own queue on the same routing key.
const QueueName = "servers.user_profiles"

// ConnectionName shows up in the RabbitMQ management UI/API, which also lets
// tests find and force-close exactly this consumer's connection.
const ConnectionName = "discordia-servers-user-profiles"

// Store is where the replica is written. Upsert must apply p only if it is
// newer than what is stored, and report whether it did.
type Store interface {
	Upsert(ctx context.Context, p userprofile.Profile) (bool, error)
}

type Consumer struct {
	url   string
	store Store
}

func New(url string, store Store) *Consumer {
	return &Consumer{url: url, store: store}
}

// Start consumes until ctx is cancelled, reconnecting with exponential
// backoff (1s up to 30s) whenever the connection to RabbitMQ drops.
func (c *Consumer) Start(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for ctx.Err() == nil {
		if err := c.consumeOnce(ctx); err != nil {
			logging.Warnf("profile_consumer: RabbitMQ connection lost, retrying in %s: %v", backoff, err)
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
	if err := ch.QueueBind(queue.Name, users.RoutingKeyProfileUpdated, broker.ExchangeName, false, nil); err != nil {
		return err
	}

	// Manual ack: a message is only confirmed once processed, so a transient
	// database failure never loses an event.
	retrier, err := deadletter.New(conn, queue.Name, deadletter.DefaultPolicy())
	if err != nil {
		return err
	}
	defer retrier.Close()

	messages, err := ch.Consume(queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	logging.Infof("profile_consumer: consuming %s", QueueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-messages:
			if !ok {
				return errors.New("consume channel closed")
			}
			spanCtx, end := telemetry.StartConsumer(ctx, QueueName, delivery)
			requeue := ShouldRequeue(spanCtx, c.store, delivery.RoutingKey, delivery.Body)
			retrier.Settle(spanCtx, delivery, requeue)
			end(requeue)
		}
	}
}

// ShouldRequeue processes one message and reports whether it must be retried
// (true) or acknowledged (false). Messages that can never succeed (unknown
// routing key, malformed JSON, missing or non-UUID user_id, version < 1) are
// dropped with a log instead of being retried forever; only a failure to
// write the replica is retried. A stale or duplicate version is a no-op.
func ShouldRequeue(ctx context.Context, store Store, routingKey string, body []byte) bool {
	if routingKey != users.RoutingKeyProfileUpdated {
		logging.Warnf("profile_consumer: unknown routing key %q, dropping message", routingKey)
		return false
	}
	var evt users.ProfileUpdated
	if err := json.Unmarshal(body, &evt); err != nil {
		logging.Warnf("profile_consumer: malformed payload, dropping message: %v", err)
		return false
	}
	if _, err := uuid.Parse(evt.UserID); err != nil {
		logging.Warnf("profile_consumer: invalid user_id %q, event_id=%s, dropping message", evt.UserID, evt.EventID)
		return false
	}
	if evt.Version < 1 {
		logging.Warnf("profile_consumer: invalid version %d, event_id=%s user_id=%s, dropping message", evt.Version, evt.EventID, evt.UserID)
		return false
	}

	applied, err := store.Upsert(ctx, userprofile.Profile{
		UserID:      evt.UserID,
		Name:        evt.Name,
		AvatarURL:   evt.AvatarURL,
		Description: evt.Description,
		StatusText:  evt.StatusText,
		StatusEmoji: evt.StatusEmoji,
		Version:     evt.Version,
	})
	if err != nil {
		logging.Errorf("profile_consumer: failed to store profile, event_id=%s user_id=%s, retrying: %v", evt.EventID, evt.UserID, err)
		return true
	}
	if !applied {
		logging.Debugf("profile_consumer: ignored stale profile, event_id=%s user_id=%s version=%d", evt.EventID, evt.UserID, evt.Version)
		return false
	}
	logging.Infof("profile_consumer: stored profile, event_id=%s user_id=%s version=%d", evt.EventID, evt.UserID, evt.Version)
	return false
}
