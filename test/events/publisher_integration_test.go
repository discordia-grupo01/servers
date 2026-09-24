package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
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
	want := membership.MemberJoined{
		Meta:     envelope.Meta{EventID: "test-event-id", OccurredAt: time.Now().UTC().Truncate(time.Second)},
		ServerID: "11111111-1111-1111-1111-111111111111",
		UserID:   "user-integration",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publisher.Publish(ctx, membership.RoutingKeyMemberJoined, want); err != nil {
		t.Fatalf("Publish() error = %v, want nil", err)
	}

	select {
	case msg := <-deliveries:
		if msg.RoutingKey != membership.RoutingKeyMemberJoined {
			t.Errorf("routing key = %q, want %q", msg.RoutingKey, membership.RoutingKeyMemberJoined)
		}
		var got membership.MemberJoined
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
			evt := membership.MemberJoined{
				Meta:     envelope.Meta{EventID: fmt.Sprintf("concurrent-%d", i), OccurredAt: time.Now().UTC().Truncate(time.Second)},
				ServerID: "11111111-1111-1111-1111-111111111111",
				UserID:   fmt.Sprintf("user-%d", i),
			}
			if err := publisher.Publish(ctx, membership.RoutingKeyMemberJoined, evt); err != nil {
				t.Errorf("Publish() call %d error = %v, want nil", i, err)
			}
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for len(seen) < n {
		select {
		case msg := <-deliveries:
			var got membership.MemberJoined
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

// managementAPI derives the RabbitMQ management HTTP API base URL and
// credentials from an amqp:// URL (same host, management port 15672
// instead of the AMQP port).
func managementAPI(t *testing.T, amqpURL string) (base, user, pass string) {
	t.Helper()
	u, err := url.Parse(amqpURL)
	if err != nil {
		t.Fatalf("parse RABBITMQ_URL: %v", err)
	}
	user = u.User.Username()
	pass, _ = u.User.Password()
	return "http://" + u.Hostname() + ":15672", user, pass
}

// findManagedConnectionNames lists open AMQP connections via the management
// HTTP API and returns the (internal, not client-chosen) names of those
// whose client_properties.connection_name matches name.
func findManagedConnectionNames(t *testing.T, base, user, pass, name string) []string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+"/api/connections", nil)
	if err != nil {
		t.Fatalf("build list-connections request: %v", err)
	}
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list connections via management API: %v", err)
	}
	defer resp.Body.Close()

	var conns []struct {
		Name             string `json:"name"`
		ClientProperties struct {
			ConnectionName string `json:"connection_name"`
		} `json:"client_properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conns); err != nil {
		t.Fatalf("decode connections list: %v", err)
	}

	var names []string
	for _, c := range conns {
		if c.ClientProperties.ConnectionName == name {
			names = append(names, c.Name)
		}
	}
	return names
}

// closeManagedConnection force-closes every open AMQP connection whose
// client_properties.connection_name matches name, via the management HTTP
// API -- used to simulate "the broker drops our connection mid-flight"
// without needing to reach into AMQPPublisher's unexported state.
//
// The management plugin refreshes its stats on an internal interval (a few
// seconds), so a connection opened just before this call may not show up
// yet -- retry with backoff instead of a single lookup.
func closeManagedConnection(t *testing.T, base, user, pass, name string) {
	t.Helper()

	var names []string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		names = findManagedConnectionNames(t, base, user, pass, name)
		if len(names) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(names) == 0 {
		t.Fatalf("no open connection named %q found via the management API after 10s -- was it ever established?", name)
	}

	for _, connName := range names {
		delReq, err := http.NewRequest(http.MethodDelete, base+"/api/connections/"+url.PathEscape(connName), nil)
		if err != nil {
			t.Fatalf("build close-connection request: %v", err)
		}
		delReq.SetBasicAuth(user, pass)
		delReq.Header.Set("X-Reason", "forced by TestAMQPPublisher_Publish_ReconnectsAfterConnectionIsDropped")
		delResp, err := http.DefaultClient.Do(delReq)
		if err != nil {
			t.Fatalf("close connection %q: %v", connName, err)
		}
		delResp.Body.Close()
	}
}

// CB: reconexión tras un publish fallido estando ya conectado (the
// dropChannel path, publisher.go's Publish -> dropChannel -> connect ->
// retry). Forces a real disconnect via the management API between two
// Publish calls on the same *AMQPPublisher and verifies the second one
// still gets delivered -- proving the reconnect actually works, not just
// that the failure gets swallowed silently.
func TestAMQPPublisher_Publish_ReconnectsAfterConnectionIsDropped(t *testing.T) {
	url_ := os.Getenv("RABBITMQ_URL")
	if url_ == "" {
		t.Skip("RABBITMQ_URL not set: skipping integration test")
	}

	conn, err := amqp.Dial(url_)
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
	if err := ch.QueueBind(q.Name, membership.RoutingKeyMemberJoined, "discordia.events", false, nil); err != nil {
		t.Fatalf("QueueBind: %v", err)
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publisher := events.NewAMQPPublisher(url_)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	before := membership.MemberJoined{
		Meta:     envelope.Meta{EventID: "before-drop-" + uuid.NewString(), OccurredAt: time.Now().UTC().Truncate(time.Second)},
		ServerID: "11111111-1111-1111-1111-111111111111",
		UserID:   "user-before",
	}
	if err := publisher.Publish(ctx, membership.RoutingKeyMemberJoined, before); err != nil {
		t.Fatalf("Publish() (before drop) error = %v, want nil", err)
	}

	select {
	case msg := <-deliveries:
		var got membership.MemberJoined
		if err := json.Unmarshal(msg.Body, &got); err != nil {
			t.Fatalf("unmarshal delivered payload: %v", err)
		}
		if got.EventID != before.EventID {
			t.Fatalf("first delivery event_id = %q, want %q", got.EventID, before.EventID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the message published before the forced disconnect")
	}

	base, user, pass := managementAPI(t, url_)
	closeManagedConnection(t, base, user, pass, events.ConnectionName)
	time.Sleep(300 * time.Millisecond) // let the close reach the client side

	after := membership.MemberJoined{
		Meta:     envelope.Meta{EventID: "after-drop-" + uuid.NewString(), OccurredAt: time.Now().UTC().Truncate(time.Second)},
		ServerID: "11111111-1111-1111-1111-111111111111",
		UserID:   "user-after",
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel2()
	if err := publisher.Publish(ctx2, membership.RoutingKeyMemberJoined, after); err != nil {
		t.Fatalf("Publish() (after drop) error = %v, want nil", err)
	}

	select {
	case msg := <-deliveries:
		var got membership.MemberJoined
		if err := json.Unmarshal(msg.Body, &got); err != nil {
			t.Fatalf("unmarshal delivered payload: %v", err)
		}
		if got.EventID != after.EventID {
			t.Fatalf("second delivery event_id = %q, want %q -- the publisher likely did not reconnect after the forced disconnect", got.EventID, after.EventID)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for the message published after the forced disconnect -- the reconnect (dropChannel) path may be broken")
	}
}
