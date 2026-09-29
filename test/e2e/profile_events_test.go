package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/profile_consumer"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/user_profile_repo"
)

// setupProfileReplica starts the real profile consumer against the database
// setupE2E prepared, and returns a channel to publish on discordia.events plus
// the pool. The consumer's queue is declared, bound and purged here first, so
// nothing published by the test can be dropped for lack of a bound queue and
// nothing left over from an earlier run is consumed.
func setupProfileReplica(t *testing.T) (*amqp.Channel, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pool, err := repository.NewPool(ctx, "host="+os.Getenv("DATABASE_HOST")+
		" port="+env("DATABASE_PORT", "5432")+
		" dbname="+env("DATABASE_NAME", "discordia_servicio")+
		" user="+env("DATABASE_USER", "discordia")+
		" password="+env("DATABASE_PASSWORD", "discordia")+
		" sslmode=disable")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("conn.Channel: %v", err)
	}
	if err := ch.ExchangeDeclare("discordia.events", "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("ExchangeDeclare: %v", err)
	}
	if _, err := ch.QueueDeclare(profile_consumer.QueueName, true, false, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	if err := ch.QueueBind(profile_consumer.QueueName, profile_consumer.RoutingKeyProfileUpdated, "discordia.events", false, nil); err != nil {
		t.Fatalf("QueueBind: %v", err)
	}
	if _, err := ch.QueuePurge(profile_consumer.QueueName, false); err != nil {
		t.Fatalf("QueuePurge: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = ch.QueueDelete(profile_consumer.QueueName, false, false, false)
		ch.Close()
	})

	go profile_consumer.New(os.Getenv("RABBITMQ_URL"), user_profile_repo.NewUserProfileRepository(pool)).Start(ctx)
	return ch, pool
}

func publishProfile(t *testing.T, ch *amqp.Channel, userID string, version int64, name string) {
	t.Helper()
	body, err := json.Marshal(profile_consumer.ProfileUpdated{
		Meta:   envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()},
		UserID: userID, Version: version, Name: name,
		AvatarURL: "https://cdn/" + name + ".png", Description: "about " + name,
		StatusText: "status " + name, StatusEmoji: "🎮",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := ch.PublishWithContext(context.Background(), "discordia.events", profile_consumer.RoutingKeyProfileUpdated, false, false,
		amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// waitForMemberProfile polls GET /members until user's profile has the given
// name, as a client would see it.
func waitForMemberProfile(t *testing.T, r http.Handler, serverID, user, wantName string) model.MemberProfileResponse {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w := doAsUser(t, r, http.MethodGet, "/v1/servers/"+serverID+"/members", user)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /members status = %d body = %s", w.Code, w.Body.String())
		}
		var resp model.MemberListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		for _, m := range resp.Members {
			if m.UserID == user && m.Profile != nil && m.Profile.Name == wantName {
				return *m.Profile
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s's profile named %q in GET /members", user, wantName)
	return model.MemberProfileResponse{}
}

// waitForStoredVersion waits until user's replicated profile has version, by
// reading the table directly (used for a user that isn't in any server).
func waitForStoredVersion(t *testing.T, pool *pgxpool.Pool, user string, version int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var got int64
		err := pool.QueryRow(context.Background(), `SELECT version FROM user_profiles WHERE user_id = $1`, user).Scan(&got)
		if err == nil && got == version {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s's profile version %d", user, version)
}

func createServerAs(t *testing.T, r http.Handler, owner string) string {
	t.Helper()
	w := postCreateServer(t, r, owner, "profiles-"+uuid.NewString()[:8])
	if w.Code != http.StatusCreated {
		t.Fatalf("create server status = %d body = %s", w.Code, w.Body.String())
	}
	var srv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &srv); err != nil {
		t.Fatalf("decode server: %v", err)
	}
	return srv.ID
}

func TestProfileEvents_MembersListShowsReplicatedProfile(t *testing.T) {
	r, _ := setupE2E(t)
	ch, pool := setupProfileReplica(t)
	owner := uuid.NewString()
	serverID := createServerAs(t, r, owner)

	// Before any event the member is listed with a null profile.
	w := doAsUser(t, r, http.MethodGet, "/v1/servers/"+serverID+"/members", owner)
	var before model.MemberListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil || len(before.Members) != 1 || before.Members[0].Profile != nil {
		t.Fatalf("before any event: members = %+v (err %v), want one member with a null profile", before.Members, err)
	}

	publishProfile(t, ch, owner, 2, "manu")
	got := waitForMemberProfile(t, r, serverID, owner, "manu")
	want := model.MemberProfileResponse{Name: "manu", AvatarURL: "https://cdn/manu.png", Description: "about manu", StatusText: "status manu", StatusEmoji: "🎮"}
	if got != want {
		t.Errorf("profile = %+v, want %+v", got, want)
	}

	// A lower version arriving late doesn't overwrite it. The queue has a
	// single consumer, so once the sentinel published after it is stored the
	// stale event has been processed too.
	publishProfile(t, ch, owner, 1, "stale")
	sentinel := uuid.NewString()
	publishProfile(t, ch, sentinel, 1, "sentinel")
	waitForStoredVersion(t, pool, sentinel, 1)
	waitForMemberProfile(t, r, serverID, owner, "manu")
}

// The consumer reconnects after the broker drops its connection, and events
// published meanwhile are not lost (durable queue, manual ack).
func TestProfileEvents_ConsumerReconnectsAfterConnectionIsDropped(t *testing.T) {
	r, _ := setupE2E(t)
	ch, _ := setupProfileReplica(t)
	owner := uuid.NewString()
	serverID := createServerAs(t, r, owner)

	publishProfile(t, ch, owner, 1, "before")
	waitForMemberProfile(t, r, serverID, owner, "before")

	closeConsumerConnection(t, os.Getenv("RABBITMQ_URL"), profile_consumer.ConnectionName)
	publishProfile(t, ch, owner, 2, "after")
	waitForMemberProfile(t, r, serverID, owner, "after")
}

// closeConsumerConnection force-closes the AMQP connection whose
// connection_name is name, through the management API (port 15672), retrying
// while the management stats catch up with a freshly opened connection.
func closeConsumerConnection(t *testing.T, amqpURL, name string) {
	t.Helper()
	u, err := url.Parse(amqpURL)
	if err != nil {
		t.Fatalf("parse RABBITMQ_URL: %v", err)
	}
	base := "http://" + u.Hostname() + ":15672/api/connections"
	user := u.User.Username()
	pass, _ := u.User.Password()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, base, nil)
		req.SetBasicAuth(user, pass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list connections: %v", err)
		}
		var conns []struct {
			Name             string `json:"name"`
			ClientProperties struct {
				ConnectionName string `json:"connection_name"`
			} `json:"client_properties"`
		}
		err = json.NewDecoder(resp.Body).Decode(&conns)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode connections: %v", err)
		}
		closed := false
		for _, c := range conns {
			if c.ClientProperties.ConnectionName != name {
				continue
			}
			del, _ := http.NewRequest(http.MethodDelete, base+"/"+url.PathEscape(c.Name), nil)
			del.SetBasicAuth(user, pass)
			if dresp, err := http.DefaultClient.Do(del); err == nil {
				dresp.Body.Close()
				closed = true
			}
		}
		if closed {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no open connection named %q found via the management API", name)
}
