package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/servers/internal/events"
)

// Integration test against a real RabbitMQ broker: binds a temporary queue to
// the discordia.events exchange, publishes through the real AMQPPublisher,
// and asserts the message arrives with the expected routing key and payload.
// Skipped unless RABBITMQ_URL is set -- run a local broker by hand (see
// README) to exercise it, same gate as the Postgres integration tests use
// DATABASE_HOST.
func TestAMQPPublisher_Publish_DeliversToRealBroker(t *testing.T) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL not set: skipping integration test")
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("conn.Channel: %v", err)
	}
	t.Cleanup(func() { ch.Close() })

	if err := ch.ExchangeDeclare("discordia.events", "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("ExchangeDeclare: %v", err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	if err := ch.QueueBind(q.Name, "servers.member_joined", "discordia.events", false, nil); err != nil {
		t.Fatalf("QueueBind: %v", err)
	}

	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publisher := events.NewAMQPPublisher(url)
	want := events.MembershipEvent{
		EventID:    "test-event-id",
		ServerID:   "11111111-1111-1111-1111-111111111111",
		UserID:     "user-integration",
		OccurredAt: time.Now().UTC().Truncate(time.Second),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publisher.Publish(ctx, events.RoutingKeyMemberJoined, want); err != nil {
		t.Fatalf("Publish() error = %v, want nil", err)
	}

	select {
	case msg := <-deliveries:
		if msg.RoutingKey != events.RoutingKeyMemberJoined {
			t.Errorf("routing key = %q, want %q", msg.RoutingKey, events.RoutingKeyMemberJoined)
		}
		var got events.MembershipEvent
		if err := json.Unmarshal(msg.Body, &got); err != nil {
			t.Fatalf("unmarshal delivered payload: %v", err)
		}
		if got != want {
			t.Errorf("delivered payload = %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the message to arrive on the bound queue")
	}
}

// CB: two concurrent join/leave requests each publish their own event --
// this repo doesn't dedupe or order them (that's the consumer's job), it
// just needs to not drop or corrupt either one under concurrent Publish
// calls against the same shared AMQPPublisher.
func TestAMQPPublisher_Publish_ConcurrentCallsAllDeliverIndependently(t *testing.T) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL not set: skipping integration test")
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("conn.Channel: %v", err)
	}
	t.Cleanup(func() { ch.Close() })

	if err := ch.ExchangeDeclare("discordia.events", "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("ExchangeDeclare: %v", err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	if err := ch.QueueBind(q.Name, "servers.member_joined", "discordia.events", false, nil); err != nil {
		t.Fatalf("QueueBind: %v", err)
	}

	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publisher := events.NewAMQPPublisher(url)

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			evt := events.MembershipEvent{
				EventID:    fmt.Sprintf("concurrent-%d", i),
				ServerID:   "11111111-1111-1111-1111-111111111111",
				UserID:     fmt.Sprintf("user-%d", i),
				OccurredAt: time.Now().UTC().Truncate(time.Second),
			}
			if err := publisher.Publish(ctx, events.RoutingKeyMemberJoined, evt); err != nil {
				t.Errorf("Publish() call %d error = %v, want nil", i, err)
			}
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for len(seen) < n {
		select {
		case msg := <-deliveries:
			var got events.MembershipEvent
			if err := json.Unmarshal(msg.Body, &got); err != nil {
				t.Fatalf("unmarshal delivered payload: %v", err)
			}
			if seen[got.EventID] {
				t.Fatalf("event_id %q delivered more than once", got.EventID)
			}
			seen[got.EventID] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for all %d messages, got %d so far", n, len(seen))
		}
	}
}
