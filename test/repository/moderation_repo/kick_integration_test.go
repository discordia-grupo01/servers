package moderation_repo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

func kickEvents() []outbox.Event {
	return []outbox.Event{
		{RoutingKey: "servers.member_left", Payload: []byte(`{"n":1}`)},
		{RoutingKey: "servers.member_kicked", Payload: []byte(`{"n":2}`)},
	}
}

func newKick(serverID uuid.UUID, userID string) moderation.Kick {
	return moderation.Kick{ServerID: serverID, UserID: userID, KickedBy: "mod-1", KickedAt: time.Now().UTC().Truncate(time.Millisecond)}
}

// #59 CA1: the member is removed with their roles, a pending transfer to them
// is cancelled and both events land in the outbox -- and, unlike a ban,
// nothing keeps them out.
func TestKickMember_RemovesMemberCancelsTransferAndWritesEvents(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target", "bystander")

	roleID := uuid.New()
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, position) VALUES ($1, $2, 'R', '#000000', 1)`, roleID, serverID)
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, 'target', $2)`, serverID, roleID)
	transferID := uuid.New()
	exec(ctx, t, pool, `INSERT INTO ownership_transfers (id, server_id, from_user_id, to_user_id) VALUES ($1, $2, $3, 'target')`, transferID, serverID, owner)

	if err := repo.KickMember(ctx, newKick(serverID, "target"), allowAll, kickEvents()...); err != nil {
		t.Fatalf("KickMember: %v", err)
	}

	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 0 {
		t.Error("the target is still a member")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM member_roles WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 0 {
		t.Error("the target kept their role assignments")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'bystander'`, serverID); n != 1 {
		t.Error("another member was removed")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans`); n != 0 {
		t.Error("a kick recorded a ban")
	}

	var status string
	var resolved *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, resolved_at FROM ownership_transfers WHERE id = $1`, transferID).Scan(&status, &resolved); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || resolved == nil {
		t.Errorf("transfer = %s, resolved_at %v; want cancelled with a timestamp", status, resolved)
	}

	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key IN ('servers.member_left', 'servers.member_kicked')`); n != 2 {
		t.Errorf("outbox has %d kick events, want 2", n)
	}
}

func TestKickMember_NotAMember_WritesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	err := repo.KickMember(ctx, newKick(serverID, "stranger"), allowAll, kickEvents()...)
	if !errors.Is(err, model.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Error("events were kept after the rollback")
	}
}

// The owner check runs under the server lock, so it holds even if ownership
// changed after the service looked at the server.
func TestKickMember_TargetIsTheOwner(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	exec(ctx, t, pool, `UPDATE servers SET owner_id = 'target' WHERE id = $1`, serverID)

	err := repo.KickMember(ctx, newKick(serverID, "target"), allowAll, kickEvents()...)
	if !errors.Is(err, model.ErrCannotKickOwner) {
		t.Fatalf("err = %v, want ErrCannotKickOwner", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE user_id = 'target'`); n != 1 {
		t.Error("the owner was removed")
	}
}

func TestKickMember_ServerNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	err := repo.KickMember(context.Background(), newKick(uuid.New(), "x"), allowAll, kickEvents()...)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

// Two moderators kicking the same member at once: exactly one wins, the rest
// get ErrMemberNotFound, and the events are written once.
func TestKickMember_ConcurrentKicks_OnlyOneWins(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = repo.KickMember(ctx, newKick(serverID, "target"), allowAll, kickEvents()...)
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, model.ErrMemberNotFound):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d kicks succeeded, want exactly 1", wins)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 2 {
		t.Errorf("outbox has %d events, want 2", n)
	}
}

// #59 CA2 at write time: the check gets both standings read under the server
// lock, including the owner resolved from the locked row.
func TestKickMember_CheckGetsTheStandingsReadUnderTheLock(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "mod-1", "target")
	seedRoleFor(ctx, t, pool, serverID, "mod-1", 1, role.PermKickMembers)
	seedRoleFor(ctx, t, pool, serverID, "target", 2, role.PermSendMessages)

	var gotActor, gotTarget role.Standing
	if err := repo.KickMember(ctx, newKick(serverID, "target"), func(actor, target role.Standing) error {
		gotActor, gotTarget = actor, target
		return nil
	}); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	if !gotActor.IsMember || gotActor.Permissions != role.PermKickMembers || gotActor.TopPosition == nil || *gotActor.TopPosition != 1 {
		t.Errorf("actor = %+v", gotActor)
	}
	if !gotTarget.IsMember || gotTarget.TopPosition == nil || *gotTarget.TopPosition != 2 {
		t.Errorf("target = %+v", gotTarget)
	}

	other := seedServer(ctx, t, pool, "target")
	byOwner := newKick(other, "target")
	byOwner.KickedBy = owner
	if err := repo.KickMember(ctx, byOwner, func(actor, _ role.Standing) error {
		gotActor = actor
		return nil
	}); err != nil {
		t.Fatalf("KickMember by owner: %v", err)
	}
	if !gotActor.IsOwner {
		t.Errorf("owner's standing = %+v, want IsOwner", gotActor)
	}
}

// If the check rejects the kick, its error comes back as-is and nothing is
// written.
func TestKickMember_CheckRejects_WritesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")

	err := repo.KickMember(ctx, newKick(serverID, "target"), func(_, _ role.Standing) error {
		return model.ErrTargetOutranksActor
	}, kickEvents()...)
	if !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Fatalf("err = %v, want ErrTargetOutranksActor", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 1 {
		t.Error("the member was removed")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Errorf("outbox has %d events", n)
	}
}

// A failing event insert rolls the whole kick back: the member is still
// there.
func TestKickMember_OutboxFailure_RollsBack(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")

	// outbox_events.payload is JSONB: an invalid payload makes the insert
	// fail after the membership was already deleted in the same transaction.
	bad := outbox.Event{RoutingKey: "servers.member_kicked", Payload: []byte(`not json`)}
	if err := repo.KickMember(ctx, newKick(serverID, "target"), allowAll, bad); err == nil {
		t.Fatal("KickMember succeeded with an invalid event")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 1 {
		t.Error("the member was removed although the event could not be written")
	}
}
