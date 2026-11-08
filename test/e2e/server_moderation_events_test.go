package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	serverevents "github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channel_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/server_moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/suspension_repo"
	"github.com/discordia-grupo01/servers/internal/repository/user_profile_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

const moderationTables = `moderation_audit_entries, server_boosts, channel_permission_overrides, user_profiles, suspended_users, outbox_events, member_roles, roles, server_mutes, ownership_transfers, categories, server_bans, invitations, server_members, channels, servers`

// moderationEnv is the real service wired the way main.go does it -- the admin
// consumer with server moderation, the guard, the outbox poller and the router --
// against a real Postgres and a real RabbitMQ.
type moderationEnv struct {
	router *gin.Engine
	pool   *pgxpool.Pool
	admin  *amqp.Channel

	mu        sync.Mutex
	published []amqp.Delivery // servers.server_deleted and servers.member_left, as they arrive
}

func setupModeration(t *testing.T) *moderationEnv {
	t.Helper()
	rmqURL := os.Getenv("RABBITMQ_URL")
	if os.Getenv("DATABASE_HOST") == "" || rmqURL == "" {
		t.Skip("DATABASE_HOST and RABBITMQ_URL both required: skipping end-to-end test")
	}
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
	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS server_boosts, `+moderationTables+` CASCADE`)
	}
	t.Cleanup(drop)
	drop()
	for _, f := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql",
		"0003_roles.up.sql", "0004_member_roles.up.sql", "0005_default_role.up.sql",
		"0006_ownership_transfers.up.sql", "0007_categories.up.sql",
		"0008_ownership_transfer_cancelled_status.up.sql", "0009_server_banner.up.sql",
		"0010_native_categories.up.sql", "0011_outbox_events.up.sql", "0033_outbox_traceparent.up.sql",
		"0012_category_name_key.up.sql", "0013_unassigned_category.up.sql",
		"0014_drop_default_kind.up.sql", "0015_role_positions.up.sql",
		"0016_server_bans_banned_by.up.sql", "0017_user_profiles.up.sql",
		"0019_everyone_role.up.sql", "0020_channel_permission_overrides.up.sql",
		"0021_server_mutes.up.sql", "0027_suspended_users.up.sql", "0028_server_moderation.up.sql", "0032_moderation_audit.up.sql",
		"0031_server_boosts.up.sql",
	} {
		schema, err := os.ReadFile("../../migrations/" + f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", f, err)
		}
	}

	conn, err := amqp.Dial(rmqURL)
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
	// The consumer's queue is declared, bound and purged here first, so nothing
	// published by the test can be dropped for lack of a bound queue.
	if _, err := ch.QueueDeclare(admin_consumer.QueueName, true, false, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	for _, key := range []string{
		admin.RoutingKeyUserSuspended, admin.RoutingKeyUserReactivated,
		admin.RoutingKeyServerSuspended, admin.RoutingKeyServerReactivated, admin.RoutingKeyServerDeletionRequested,
	} {
		if err := ch.QueueBind(admin_consumer.QueueName, key, "discordia.events", false, nil); err != nil {
			t.Fatalf("QueueBind %s: %v", key, err)
		}
	}
	if _, err := ch.QueuePurge(admin_consumer.QueueName, false); err != nil {
		t.Fatalf("QueuePurge: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = ch.QueueDelete(admin_consumer.QueueName, false, false, false)
		ch.Close()
	})

	// A probe queue sees what the service publishes after a deletion.
	probe, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("QueueDeclare probe: %v", err)
	}
	for _, key := range []string{serverevents.RoutingKeyServerDeleted, membership.RoutingKeyMemberLeft} {
		if err := ch.QueueBind(probe.Name, key, "discordia.events", false, nil); err != nil {
			t.Fatalf("QueueBind probe %s: %v", key, err)
		}
	}
	deliveries, err := ch.Consume(probe.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("Consume probe: %v", err)
	}

	serverRepo := repository.NewServerRepository(pool)
	memberRepo := member_repo.NewMemberRepository(pool)
	categoryRepo := category_repo.NewCategoryRepository(pool)
	channelRepo := channel_repo.NewChannelRepository(pool)
	moderationRepo := moderation_repo.NewRepository(pool)
	channelPermRepo := channelperm_repo.NewRepository(pool)
	icons, err := iconstore.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("iconstore: %v", err)
	}
	svc := service.New(serverRepo, icons, memberRepo, channelPermRepo)
	moderationStore := server_moderation_repo.NewRepository(pool)
	guard := middleware.NewServerSuspensionGuard(moderationStore, time.Minute)

	go outbox.NewPoller(pool, events.NewAMQPPublisher(rmqURL)).Start(ctx)
	go admin_consumer.New(rmqURL, adminSecret, suspension_repo.NewSuspensionRepository(pool)).
		WithServerModeration(admin_consumer.ServerHandlers{Store: moderationStore, Deleter: svc, Cache: guard}).
		Start(ctx)

	gin.SetMode(gin.TestMode)
	router := handler.NewRouter(handler.Deps{
		Servers:               svc,
		Icons:                 icons,
		Membership:            membership_service.New(serverRepo, memberRepo),
		Invitations:           invitation.NewService(repository.NewInvitationRepository(pool), serverRepo, memberRepo),
		VoiceAccess:           channel_service.New(serverRepo, categoryRepo, channelRepo, memberRepo, channelPermRepo, moderationRepo, user_profile_repo.NewUserProfileRepository(pool)),
		Auth:                  middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		RejectSuspendedServer: guard.RejectSuspendedServer(),
		DB:                    pool,
	})

	m := &moderationEnv{router: router, pool: pool, admin: ch}
	go func() {
		for d := range deliveries {
			m.mu.Lock()
			m.published = append(m.published, d)
			m.mu.Unlock()
		}
	}()
	return m
}

func (m *moderationEnv) publishedFor(routingKey, serverID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, d := range m.published {
		if d.RoutingKey == routingKey && strings.Contains(string(d.Body), serverID) {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func serverSuspendedEvent(secret, serverID string, version int64) admin.ServerSuspended {
	e := admin.ServerSuspended{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

func serverReactivatedEvent(secret, serverID string, version int64) admin.ServerReactivated {
	e := admin.ServerReactivated{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

func deletionRequestedEvent(secret, serverID string, version int64) admin.ServerDeletionRequested {
	e := admin.ServerDeletionRequested{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

type createdServer struct {
	ID        string
	VoiceID   string
	TextID    string
	Owner     string
	Member    string
	InviteURL string
}

// newServerWithMember creates a server over HTTP and has a second user join it
// through an invitation.
func (m *moderationEnv) newServerWithMember(t *testing.T) createdServer {
	t.Helper()
	owner, member := "owner-"+uuid.NewString(), "member-"+uuid.NewString()
	w := postCreateServer(t, m.router, owner, "mod-"+uuid.NewString()[:8])
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d body = %s", w.Code, w.Body.String())
	}
	var srv struct {
		ID       string `json:"id"`
		Channels []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &srv); err != nil {
		t.Fatalf("decode server: %v", err)
	}
	cs := createdServer{ID: srv.ID, Owner: owner, Member: member}
	for _, c := range srv.Channels {
		if c.Kind == "voice" {
			cs.VoiceID = c.ID
		} else {
			cs.TextID = c.ID
		}
	}

	w = doJSONAsUser(t, m.router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create invite = %d body = %s", w.Code, w.Body.String())
	}
	var inv struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &inv); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	cs.InviteURL = "/v1/invites/" + inv.Code + "/join"
	if w := doAsUser(t, m.router, http.MethodPost, cs.InviteURL, member); w.Code != http.StatusCreated {
		t.Fatalf("join = %d body = %s", w.Code, w.Body.String())
	}
	return cs
}

func (m *moderationEnv) status(t *testing.T, method, path, user string) (int, string) {
	t.Helper()
	w := doAsUser(t, m.router, method, path, user)
	return w.Code, w.Body.String()
}

func (m *moderationEnv) serverExists(t *testing.T, id string) bool {
	t.Helper()
	var n int
	if err := m.pool.QueryRow(context.Background(), `SELECT count(*) FROM servers WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count servers: %v", err)
	}
	return n > 0
}

// waitUntilForbidden polls a route until it answers 403 SERVER_SUSPENDED (or stops doing so).
func (m *moderationEnv) waitForSuspension(t *testing.T, path, user string, suspended bool) {
	t.Helper()
	eventually(t, "the server to be suspended="+map[bool]string{true: "true", false: "false"}[suspended], func() bool {
		code, body := m.status(t, http.MethodGet, path, user)
		if suspended {
			return code == http.StatusForbidden && strings.Contains(body, `"error_code":"SERVER_SUSPENDED"`)
		}
		return code != http.StatusForbidden
	})
}

// HU #69, end to end: a signed admin.server_suspended over RabbitMQ blocks the
// server for everyone -- owner included -- including the LiveKit token; the
// server stays in the list of my servers, marked; a member can still leave; and
// admin.server_reactivated brings everything back.
func TestServerModeration_SuspendAndReactivateEndToEnd(t *testing.T) {
	m := setupModeration(t)
	s := m.newServerWithMember(t)
	voicePath := "/v1/channels/" + s.VoiceID + "/voice-access"
	serverPath := "/v1/servers/" + s.ID

	// Active: everything works (and the guard caches "not suspended").
	for _, user := range []string{s.Owner, s.Member} {
		if code, body := m.status(t, http.MethodGet, voicePath, user); code != http.StatusOK {
			t.Fatalf("voice-access before the suspension for %s = %d body = %s", user, code, body)
		}
	}

	publishAdmin(t, m.admin, admin.RoutingKeyServerSuspended, serverSuspendedEvent(adminSecret, s.ID, 1))
	m.waitForSuspension(t, voicePath, s.Member, true)

	// Nobody acts on it: not the member, not the owner, not on the server itself,
	// nor on its channels, nor by joining with the invitation.
	for _, user := range []string{s.Owner, s.Member} {
		for _, p := range []string{voicePath, serverPath, serverPath + "/members", "/v1/channels/" + s.TextID + "/permissions"} {
			if code, body := m.status(t, http.MethodGet, p, user); code != http.StatusForbidden || !strings.Contains(body, "SERVER_SUSPENDED") {
				t.Errorf("GET %s as %s = %d body = %s, want 403 SERVER_SUSPENDED", p, user, code, body)
			}
		}
	}
	if code, _ := m.status(t, http.MethodDelete, serverPath, s.Owner); code != http.StatusForbidden {
		t.Errorf("the owner deleting a suspended server = %d, want 403", code)
	}
	stranger := "stranger-" + uuid.NewString()
	if code, body := m.status(t, http.MethodPost, s.InviteURL, stranger); code != http.StatusForbidden || !strings.Contains(body, "SERVER_SUSPENDED") {
		t.Errorf("joining a suspended server = %d body = %s, want 403 SERVER_SUSPENDED", code, body)
	}
	var uses int
	if err := m.pool.QueryRow(context.Background(), `SELECT sum(uses) FROM invitations WHERE server_id = $1`, s.ID).Scan(&uses); err != nil || uses != 1 {
		t.Errorf("invitation uses = %d (err %v), want 1: the blocked join must not consume it", uses, err)
	}

	// Still in "my servers", marked and without content.
	w := doAsUser(t, m.router, http.MethodGet, "/v1/servers", s.Member)
	var list []struct {
		ID        string `json:"id"`
		Suspended bool   `json:"suspended"`
		Channels  []any  `json:"channels"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list) != 1 {
		t.Fatalf("list = %d body = %s, want the suspended server", w.Code, w.Body.String())
	}
	if list[0].ID != s.ID || !list[0].Suspended || len(list[0].Channels) != 0 {
		t.Errorf("listed server = %+v, want it suspended and without channels", list[0])
	}

	// Leaving is allowed; removing somebody else is not.
	if code, _ := m.status(t, http.MethodDelete, serverPath+"/members/"+s.Owner, s.Member); code != http.StatusForbidden {
		t.Errorf("removing the owner = %d, want 403", code)
	}
	if code, body := m.status(t, http.MethodDelete, serverPath+"/members/"+s.Member, s.Member); code != http.StatusNoContent {
		t.Errorf("leaving a suspended server = %d body = %s, want 204", code, body)
	}

	// Reactivation lifts everything for those who remain; an old suspension
	// (lower version) arriving late doesn't suspend it again.
	publishAdmin(t, m.admin, admin.RoutingKeyServerReactivated, serverReactivatedEvent(adminSecret, s.ID, 2))
	m.waitForSuspension(t, voicePath, s.Owner, false)
	publishAdmin(t, m.admin, admin.RoutingKeyServerSuspended, serverSuspendedEvent(adminSecret, s.ID, 1))
	other := m.newServerWithMember(t)
	publishAdmin(t, m.admin, admin.RoutingKeyServerSuspended, serverSuspendedEvent(adminSecret, other.ID, 1))
	m.waitForSuspension(t, "/v1/channels/"+other.VoiceID+"/voice-access", other.Owner, true) // the probe: queue is in order
	if code, body := m.status(t, http.MethodGet, voicePath, s.Owner); code != http.StatusOK {
		t.Errorf("voice-access after reactivating, with a stale suspension after it = %d body = %s, want 200", code, body)
	}
}

// HU #68, end to end: a signed admin.server_deletion_requested runs the same
// cascade the owner's deletion does and publishes servers.server_deleted once.
// A repeated request does nothing more, and one that isn't verifiable never
// deletes anything.
func TestServerModeration_DeletionRequestEndToEnd(t *testing.T) {
	m := setupModeration(t)
	s := m.newServerWithMember(t)
	victim := m.newServerWithMember(t) // gets only forged requests
	probe := m.newServerWithMember(t)  // proves the forged ones were already handled

	// Forged requests first: unsigned, signed with another secret, a suspension
	// replayed as a deletion, and a tampered server id. None deletes anything.
	unsigned := deletionRequestedEvent(adminSecret, victim.ID, 1)
	unsigned.Signature = ""
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, unsigned)
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent("not-the-shared-secret", victim.ID, 2))
	susp := serverSuspendedEvent(adminSecret, victim.ID, 3)
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, admin.ServerDeletionRequested{Meta: susp.Meta, ServerID: susp.ServerID, Version: susp.Version, Signature: susp.Signature})
	tampered := deletionRequestedEvent(adminSecret, probe.ID, 4)
	tampered.ServerID = victim.ID
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, tampered)

	// A valid request for a server that doesn't exist, and then the real one.
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent(adminSecret, uuid.NewString(), 1))
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent(adminSecret, s.ID, 1))

	eventually(t, "the server to be deleted", func() bool { return !m.serverExists(t, s.ID) })
	if !m.serverExists(t, victim.ID) || !m.serverExists(t, probe.ID) {
		t.Fatal("a forged deletion request deleted a server")
	}
	if code, _ := m.status(t, http.MethodGet, "/v1/servers/"+s.ID, s.Owner); code != http.StatusNotFound {
		t.Errorf("GET of the deleted server = %d, want 404", code)
	}
	for _, table := range []string{"channels", "server_members", "roles", "invitations", "categories"} {
		var n int
		if err := m.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE server_id = $1`, s.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s rows left for the deleted server = %d (err %v), want 0", table, n, err)
		}
	}

	// The cascade published what the other services depend on: one server_deleted
	// and one member_left per member.
	eventually(t, "servers.server_deleted", func() bool { return m.publishedFor(serverevents.RoutingKeyServerDeleted, s.ID) == 1 })
	eventually(t, "both member_left", func() bool { return m.publishedFor(membership.RoutingKeyMemberLeft, s.ID) == 2 })

	// A duplicate of the request, queued behind a deletion of another server,
	// has been handled by the time that one is done -- and published nothing.
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent(adminSecret, s.ID, 1))
	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent(adminSecret, probe.ID, 5))
	eventually(t, "the probe server to be deleted", func() bool { return !m.serverExists(t, probe.ID) })
	eventually(t, "the probe's server_deleted", func() bool { return m.publishedFor(serverevents.RoutingKeyServerDeleted, probe.ID) == 1 })
	if n := m.publishedFor(serverevents.RoutingKeyServerDeleted, s.ID); n != 1 {
		t.Errorf("servers.server_deleted published %d times for the repeated request, want exactly 1", n)
	}
	if !m.serverExists(t, victim.ID) {
		t.Error("the victim of the forged requests was deleted")
	}
}

// A suspended server is the natural thing for staff to delete next.
func TestServerModeration_ASuspendedServerCanBeDeleted(t *testing.T) {
	m := setupModeration(t)
	s := m.newServerWithMember(t)

	publishAdmin(t, m.admin, admin.RoutingKeyServerSuspended, serverSuspendedEvent(adminSecret, s.ID, 1))
	m.waitForSuspension(t, "/v1/servers/"+s.ID, s.Owner, true)

	publishAdmin(t, m.admin, admin.RoutingKeyServerDeletionRequested, deletionRequestedEvent(adminSecret, s.ID, 2))
	eventually(t, "the suspended server to be deleted", func() bool { return !m.serverExists(t, s.ID) })
	eventually(t, "servers.server_deleted", func() bool { return m.publishedFor(serverevents.RoutingKeyServerDeleted, s.ID) == 1 })
}
