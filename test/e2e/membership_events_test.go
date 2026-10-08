// Package e2e_test exercises the criterios de aceptación from
// PLANNING_RABBITMQ.md literally: a real HTTP request against the real
// router, backed by a real Postgres and a real RabbitMQ, must result in the
// right event landing on a queue bound to the discordia.events exchange.
// Everything here lives inside this one repo (server creation, invite,
// join, leave) -- discordia-identify is not needed to prove the publisher
// side end to end.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// setupE2E wires the real router (server creation, invites, membership)
// against a real Postgres and a real RabbitMQ, and returns a channel
// delivering every message published to discordia.events under
// servers.member_joined / servers.member_left / servers.member_banned / servers.member_muted.
// Skipped unless both
// DATABASE_HOST and RABBITMQ_URL are set, same gate style as the rest of
// this repo's integration tests.
func setupE2E(t *testing.T) (*gin.Engine, <-chan amqp.Delivery) {
	t.Helper()

	dbHost := os.Getenv("DATABASE_HOST")
	rmqURL := os.Getenv("RABBITMQ_URL")
	if dbHost == "" || rmqURL == "" {
		t.Skip("DATABASE_HOST and RABBITMQ_URL both required: skipping end-to-end test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dsn := "host=" + dbHost +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	dropTables := func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS user_profiles, outbox_events, member_roles, roles, server_mutes, ownership_transfers, categories, server_bans, invitations, server_members, channels, servers CASCADE`)
	}
	t.Cleanup(dropTables)
	dropTables()

	for _, f := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql",
		"0003_roles.up.sql", "0004_member_roles.up.sql", "0005_default_role.up.sql",
		"0006_ownership_transfers.up.sql", "0007_categories.up.sql",
		"0008_ownership_transfer_cancelled_status.up.sql", "0009_server_banner.up.sql",
		"0010_native_categories.up.sql", "0011_outbox_events.up.sql",
		"0012_category_name_key.up.sql",
		"0013_unassigned_category.up.sql",
		"0014_drop_default_kind.up.sql",
		"0015_role_positions.up.sql", "0016_server_bans_banned_by.up.sql",
		"0017_user_profiles.up.sql", "0019_everyone_role.up.sql", "0021_server_mutes.up.sql",
	} {
		schema, err := os.ReadFile("../../migrations/" + f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", f, err)
		}
	}

	publisher := events.NewAMQPPublisher(rmqURL)
	serverRepo := repository.NewServerRepository(pool)
	memberRepo := member_repo.NewMemberRepository(pool)
	moderationRepo := moderation_repo.NewRepository(pool)
	go outbox.NewPoller(pool, publisher).Start(ctx)

	icons, err := iconstore.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("iconstore: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := handler.NewRouter(handler.Deps{
		Servers:     service.New(serverRepo, icons, memberRepo, channelperm_repo.NewRepository(pool)),
		Icons:       icons,
		Membership:  membership_service.New(serverRepo, memberRepo),
		Invitations: invitation.NewService(repository.NewInvitationRepository(pool), serverRepo, memberRepo),
		Moderation:  moderation_service.New(serverRepo, memberRepo, moderationRepo, moderationRepo),
		Auth:        middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:          pool,
	})

	conn, err := amqp.Dial(rmqURL)
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
	for _, key := range []string{membership.RoutingKeyMemberJoined, membership.RoutingKeyMemberLeft, moderationevents.RoutingKeyMemberBanned, moderationevents.RoutingKeyMemberUnbanned, moderationevents.RoutingKeyMemberKicked, moderationevents.RoutingKeyMemberMuted, moderationevents.RoutingKeyMemberUnmuted} {
		if err := ch.QueueBind(q.Name, key, "discordia.events", false, nil); err != nil {
			t.Fatalf("QueueBind %s: %v", key, err)
		}
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	return router, deliveries
}

// membership.MemberJoined and MemberLeft share the same wire shape, so
// decoding into MemberJoined here is fine regardless of wantRoutingKey --
// the routing key check above is what actually verifies which event fired.
func waitForEvent(t *testing.T, deliveries <-chan amqp.Delivery, wantRoutingKey string) membership.MemberJoined {
	t.Helper()
	select {
	case msg := <-deliveries:
		if msg.RoutingKey != wantRoutingKey {
			t.Fatalf("routing key = %q, want %q", msg.RoutingKey, wantRoutingKey)
		}
		var evt membership.MemberJoined
		if err := json.Unmarshal(msg.Body, &evt); err != nil {
			t.Fatalf("unmarshal delivered payload: %v", err)
		}
		return evt
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for a %q event", wantRoutingKey)
		return membership.MemberJoined{}
	}
}

func postCreateServer(t *testing.T, r http.Handler, owner, name string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormField("name")
	_, _ = fw.Write([]byte(name))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/servers", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User-Id", owner)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doAsUser(t *testing.T, r http.Handler, method, path, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-User-Id", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// CA: "un join real (POST /v1/invites/{code}/join) publica
// servers.member_joined con el payload correcto" and "un leave real (DELETE
// /v1/servers/{id}/members/{userId}) publica servers.member_left con el
// payload correcto" -- both literally, end to end, over the real HTTP API.
func TestMembershipEvents_EndToEnd(t *testing.T) {
	router, deliveries := setupE2E(t)

	owner := "owner-" + uuid.NewString()
	joiner := "joiner-" + uuid.NewString()

	// Creating a server makes the owner a member too -- this is the same
	// bug fixed in server_service.go, verified here over the real HTTP path.
	w := postCreateServer(t, router, owner, "E2E Test Server")
	if w.Code != http.StatusCreated {
		t.Fatalf("create server: code=%d body=%s", w.Code, w.Body.String())
	}
	var srv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &srv); err != nil {
		t.Fatalf("decode server: %v", err)
	}

	ownerEvt := waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined)
	if ownerEvt.ServerID != srv.ID || ownerEvt.UserID != owner {
		t.Fatalf("owner join event = %+v, want server_id=%s user_id=%s", ownerEvt, srv.ID, owner)
	}

	w = doAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("generate invite: code=%d body=%s", w.Code, w.Body.String())
	}
	var inv struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &inv); err != nil {
		t.Fatalf("decode invite: %v", err)
	}

	w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+inv.Code+"/join", joiner)
	if w.Code != http.StatusCreated {
		t.Fatalf("join: code=%d body=%s", w.Code, w.Body.String())
	}

	joinEvt := waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined)
	if joinEvt.ServerID != srv.ID || joinEvt.UserID != joiner {
		t.Fatalf("join event = %+v, want server_id=%s user_id=%s", joinEvt, srv.ID, joiner)
	}
	if joinEvt.EventID == ownerEvt.EventID {
		t.Error("join event reused the owner's event_id")
	}

	w = doAsUser(t, router, http.MethodDelete, "/v1/servers/"+srv.ID+"/members/"+joiner, joiner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("leave: code=%d body=%s", w.Code, w.Body.String())
	}

	leaveEvt := waitForEvent(t, deliveries, membership.RoutingKeyMemberLeft)
	if leaveEvt.ServerID != srv.ID || leaveEvt.UserID != joiner {
		t.Fatalf("leave event = %+v, want server_id=%s user_id=%s", leaveEvt, srv.ID, joiner)
	}
}
