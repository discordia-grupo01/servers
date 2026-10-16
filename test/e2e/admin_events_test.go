package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channel_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/suspension_repo"
	"github.com/discordia-grupo01/servers/internal/repository/user_profile_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

const adminSecret = "e2e-admin-secret"

// setupAdminReplica starts the real admin consumer against Postgres and
// RabbitMQ, and returns a channel to publish on discordia.events plus the
// pool. The consumer's queue is declared, bound and purged here first, so
// nothing published by the test can be dropped for lack of a bound queue and
// nothing left over from an earlier run is consumed.
func setupAdminReplica(t *testing.T, onApplied admin_consumer.Invalidator) (*amqp.Channel, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("DATABASE_HOST") == "" || os.Getenv("RABBITMQ_URL") == "" {
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
	drop := func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS suspended_users`) }
	t.Cleanup(drop)
	drop()
	schema, err := os.ReadFile("../../migrations/0027_suspended_users.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

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
	if _, err := ch.QueueDeclare(admin_consumer.QueueName, true, false, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	for _, key := range []string{admin.RoutingKeyUserSuspended, admin.RoutingKeyUserReactivated} {
		if err := ch.QueueBind(admin_consumer.QueueName, key, "discordia.events", false, nil); err != nil {
			t.Fatalf("QueueBind: %v", err)
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

	var store admin_consumer.Store = suspension_repo.NewSuspensionRepository(pool)
	if onApplied != nil {
		store = admin_consumer.WithInvalidation(store, onApplied)
	}
	go admin_consumer.New(os.Getenv("RABBITMQ_URL"), adminSecret, store).Start(ctx)
	return ch, pool
}

func publishAdmin(t *testing.T, ch *amqp.Channel, routingKey string, event any) {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := ch.PublishWithContext(context.Background(), "discordia.events", routingKey, false, false,
		amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func suspend(secret, user string, version int64) admin.UserSuspended {
	e := admin.UserSuspended{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, UserID: user, Version: version}
	e.Sign(secret)
	return e
}

func reactivate(secret, user string, version int64) admin.UserReactivated {
	e := admin.UserReactivated{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, UserID: user, Version: version}
	e.Sign(secret)
	return e
}

// waitForSuspension waits until user's replicated state is (suspended, version).
func waitForSuspension(t *testing.T, pool *pgxpool.Pool, user string, suspended bool, version int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var gotSuspended bool
		var gotVersion int64
		err := pool.QueryRow(context.Background(), `SELECT suspended, version FROM suspended_users WHERE user_id = $1`, user).Scan(&gotSuspended, &gotVersion)
		if err == nil && gotSuspended == suspended && gotVersion == version {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to be suspended=%v at version %d", user, suspended, version)
}

func hasSuspensionRow(t *testing.T, pool *pgxpool.Pool, user string) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM suspended_users WHERE user_id = $1`, user).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n > 0
}

func TestAdminEvents_SuspendAndReactivateAreReplicated(t *testing.T) {
	ch, pool := setupAdminReplica(t, nil)
	user := uuid.NewString() // servers has never seen this user: the row is created anyway

	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, user, 1))
	waitForSuspension(t, pool, user, true, 1)

	publishAdmin(t, ch, admin.RoutingKeyUserReactivated, reactivate(adminSecret, user, 2))
	waitForSuspension(t, pool, user, false, 2)

	// A late duplicate of the suspension (older version) doesn't re-suspend.
	// The probe event published after it is handled in order by the same
	// consumer, so once the probe lands the stale one has been processed.
	probe := uuid.NewString()
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, user, 1))
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, probe, 1))
	waitForSuspension(t, pool, probe, true, 1)
	waitForSuspension(t, pool, user, false, 2)
}

// Events with a missing, forged or foreign-secret signature, or published under
// another routing key than the one signed, change nothing. The valid event at
// the end is processed after them (same queue, same consumer), which proves the
// invalid ones were already handled and rejected.
func TestAdminEvents_UnverifiableEventsChangeNothing(t *testing.T) {
	ch, pool := setupAdminReplica(t, nil)
	victim := uuid.NewString()
	probe := uuid.NewString()

	unsigned := suspend(adminSecret, victim, 1)
	unsigned.Signature = ""
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, unsigned)
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend("not-the-shared-secret", victim, 2))
	forged := suspend(adminSecret, victim, 3)
	forged.Signature = "00" + forged.Signature[2:]
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, forged)
	// A valid reactivation signature can't pass as a suspension and vice versa.
	r := reactivate(adminSecret, victim, 4)
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, admin.UserSuspended{Meta: r.Meta, UserID: r.UserID, Version: r.Version, Signature: r.Signature})
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, probe, 1))

	waitForSuspension(t, pool, probe, true, 1)
	if hasSuspensionRow(t, pool, victim) {
		t.Errorf("victim has a suspended_users row after only unverifiable events")
	}
}

// lateInvalidator forwards to a guard that is built after the consumer starts
// (the guard needs the pool setupAdminReplica creates).
type lateInvalidator struct {
	guard atomic.Pointer[middleware.SuspensionGuard]
}

func (l *lateInvalidator) Invalidate(userID string) {
	if g := l.guard.Load(); g != nil {
		g.Invalidate(userID)
	}
}

// Full path: signed admin.user_suspended over RabbitMQ -> consumer -> Postgres
// -> RejectSuspended -> 403 on a real route and on voice-access, which is what
// the voice service uses to authorize the LiveKit token. A later
// admin.user_reactivated lifts it.
func TestAdminEvents_SuspendedUserIsRejectedAndVoiceAccessDenied(t *testing.T) {
	late := &lateInvalidator{}
	ch, pool := setupAdminReplica(t, late)
	ctx := context.Background()

	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS channel_permission_overrides, user_profiles, outbox_events, member_roles, roles, server_mutes, ownership_transfers, categories, server_bans, invitations, server_members, channels, servers CASCADE`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS channel_permission_overrides, user_profiles, outbox_events, member_roles, roles, server_mutes, ownership_transfers, categories, server_bans, invitations, server_members, channels, servers CASCADE`)
	})
	for _, f := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql",
		"0003_roles.up.sql", "0004_member_roles.up.sql", "0005_default_role.up.sql",
		"0006_ownership_transfers.up.sql", "0007_categories.up.sql",
		"0008_ownership_transfer_cancelled_status.up.sql", "0009_server_banner.up.sql",
		"0010_native_categories.up.sql", "0011_outbox_events.up.sql",
		"0012_category_name_key.up.sql", "0013_unassigned_category.up.sql",
		"0014_drop_default_kind.up.sql", "0015_role_positions.up.sql",
		"0016_server_bans_banned_by.up.sql", "0017_user_profiles.up.sql",
		"0019_everyone_role.up.sql", "0020_channel_permission_overrides.up.sql",
		"0021_server_mutes.up.sql",
	} {
		schema, err := os.ReadFile("../../migrations/" + f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", f, err)
		}
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
	guard := middleware.NewSuspensionGuard(suspension_repo.NewSuspensionRepository(pool), time.Minute)
	late.guard.Store(guard)

	gin.SetMode(gin.TestMode)
	router := handler.NewRouter(handler.Deps{
		Servers:         service.New(serverRepo, icons, memberRepo, channelPermRepo),
		Icons:           icons,
		Membership:      membership_service.New(serverRepo, memberRepo),
		VoiceAccess:     channel_service.New(serverRepo, categoryRepo, channelRepo, memberRepo, channelPermRepo, moderationRepo, user_profile_repo.NewUserProfileRepository(pool)),
		Auth:            middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		RejectSuspended: guard.RejectSuspended(),
		DB:              pool,
	})

	owner := uuid.NewString()
	w := postCreateServer(t, router, owner, "suspend-"+uuid.NewString()[:8])
	if w.Code != http.StatusCreated {
		t.Fatalf("create server status = %d body = %s", w.Code, w.Body.String())
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
	voiceID := ""
	for _, c := range srv.Channels {
		if c.Kind == "voice" {
			voiceID = c.ID
		}
	}
	if voiceID == "" {
		t.Fatalf("no voice channel in %s", w.Body.String())
	}
	voicePath := "/v1/channels/" + voiceID + "/voice-access"

	// Active: voice-access authorizes the owner (this also caches "not suspended").
	if w := doAsUser(t, router, http.MethodGet, voicePath, owner); w.Code != http.StatusOK {
		t.Fatalf("voice-access before suspension = %d body = %s", w.Code, w.Body.String())
	}

	assertSuspended := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			w := doAsUser(t, router, http.MethodGet, voicePath, owner)
			if (w.Code == http.StatusForbidden) == want {
				if want {
					var e struct {
						Error struct {
							Details map[string]any `json:"details"`
						} `json:"error"`
					}
					_ = json.Unmarshal(w.Body.Bytes(), &e)
					if e.Error.Details["error_code"] != "ACCOUNT_SUSPENDED" {
						t.Fatalf("403 body = %s, want details.error_code ACCOUNT_SUSPENDED", w.Body.String())
					}
				}
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for voice-access suspended=%v", want)
	}

	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, owner, 1))
	assertSuspended(true)
	// Any other action is rejected too, e.g. creating a server.
	if w := postCreateServer(t, router, owner, "again-"+uuid.NewString()[:8]); w.Code != http.StatusForbidden {
		t.Errorf("create server while suspended = %d, want 403", w.Code)
	}

	publishAdmin(t, ch, admin.RoutingKeyUserReactivated, reactivate(adminSecret, owner, 2))
	assertSuspended(false)

	// A suspended member stays in GET /members but without their own profile.
	other := uuid.NewString()
	for _, q := range []string{
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ('` + srv.ID + `', '` + other + `', FALSE, now())`,
		`INSERT INTO user_profiles (user_id, name, version) VALUES ('` + other + `', 'Other', 1), ('` + owner + `', 'Owner', 1)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	profileOf := func() map[string]*model.MemberProfileResponse {
		w := doAsUser(t, router, http.MethodGet, "/v1/servers/"+srv.ID+"/members", owner)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /members = %d body = %s", w.Code, w.Body.String())
		}
		var resp model.MemberListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		out := map[string]*model.MemberProfileResponse{}
		for _, m := range resp.Members {
			out[m.UserID] = m.Profile
		}
		return out
	}
	if p := profileOf(); p[other] == nil || p[other].Name != "Other" {
		t.Fatalf("before suspension profiles = %+v, want Other's profile", p)
	}
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, other, 1))
	waitForSuspension(t, pool, other, true, 1)
	p := profileOf()
	if _, listed := p[other]; !listed || p[other] != nil {
		t.Errorf("suspended member: listed=%v profile=%+v, want listed with a null profile", listed, p[other])
	}
	if p[owner] == nil || p[owner].Name != "Owner" {
		t.Errorf("owner profile = %+v, want it untouched", p[owner])
	}
}
