package moderation_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

const kickerID = "kicker-1"

// newKickFixture is newFixture plus kickerID, who holds KICK_MEMBERS (and
// nothing else) at rank 2.
func newKickFixture() *fixture {
	f := newFixture()
	f.standings.byUser[kickerID] = member(rank(2), role.PermKickMembers)
	return f
}

// CA1: a member with KICK_MEMBERS kicks a lower-ranked member.
func TestKickMember_KickerKicksLowerMember(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(kickerID, memberID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k := f.repo.kicked
	if f.repo.kickCalls != 1 || k.ServerID != f.server.ID || k.UserID != memberID || k.KickedBy != kickerID || k.KickedAt.IsZero() {
		t.Errorf("repo got %+v after %d calls", k, f.repo.kickCalls)
	}
}

// The kick publishes member_left (for membership consumers) and member_kicked,
// both through the same repository call (same transaction).
func TestKickMember_PublishesMemberLeftAndMemberKicked(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(kickerID, memberID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.repo.kickEvents) != 2 {
		t.Fatalf("got %d events, want 2", len(f.repo.kickEvents))
	}
	left, kicked := f.repo.kickEvents[0], f.repo.kickEvents[1]
	if left.RoutingKey != membership.RoutingKeyMemberLeft || kicked.RoutingKey != moderationevents.RoutingKeyMemberKicked {
		t.Errorf("routing keys = %q, %q", left.RoutingKey, kicked.RoutingKey)
	}

	var leftBody membership.MemberLeft
	if err := json.Unmarshal(left.Payload, &leftBody); err != nil {
		t.Fatal(err)
	}
	if leftBody.ServerID != f.server.ID.String() || leftBody.UserID != memberID || leftBody.EventID == "" {
		t.Errorf("member_left = %+v", leftBody)
	}

	var kickedBody moderationevents.MemberKicked
	if err := json.Unmarshal(kicked.Payload, &kickedBody); err != nil {
		t.Fatal(err)
	}
	if kickedBody.ServerID != f.server.ID.String() || kickedBody.UserID != memberID ||
		kickedBody.KickedBy != kickerID || kickedBody.EventID == "" || kickedBody.EventID == leftBody.EventID {
		t.Errorf("member_kicked = %+v", kickedBody)
	}
}

// CA3: the owner and administrators hold KICK_MEMBERS implicitly.
func TestKickMember_OwnerAndAdministratorsCanKick(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(ownerID, kickerID); err != nil {
		t.Errorf("owner: unexpected error %v", err)
	}

	f = newKickFixture()
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	if err := f.kick("admin-1", kickerID); err != nil {
		t.Errorf("administrator: unexpected error %v", err)
	}
}

// CA3: without KICK_MEMBERS (nor admin) the kick is rejected -- BAN_MEMBERS
// alone is not enough.
func TestKickMember_WithoutPermission_Forbidden(t *testing.T) {
	cases := map[string]string{
		"member without roles":         memberID,
		"not a member at all":          "stranger",
		"moderator with only BAN perm": moderatorID,
	}
	for name, actor := range cases {
		f := newKickFixture()
		f.standings.byUser["target"] = member(nil, 0)
		if err := f.kick(actor, "target"); !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", name, err)
		}
		if f.repo.kickCalls != 0 {
			t.Errorf("%s: repo called", name)
		}
	}
}

// CA2: nobody can kick a member with an equal or higher role, administrators
// included.
func TestKickMember_TargetWithEqualOrHigherRole_Forbidden(t *testing.T) {
	for name, p := range map[string]int{"equal rank": 2, "higher rank": 1} {
		f := newKickFixture()
		f.standings.byUser["target"] = member(rank(p), 0)
		if err := f.kick(kickerID, "target"); !errors.Is(err, model.ErrTargetOutranksActor) {
			t.Errorf("%s: err = %v, want ErrTargetOutranksActor", name, err)
		}
		if f.repo.kickCalls != 0 {
			t.Errorf("%s: repo called", name)
		}
	}

	f := newKickFixture()
	f.standings.byUser["admin-1"] = member(rank(3), role.PermAdministrator)
	if err := f.kick("admin-1", kickerID); !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Errorf("admin below target: err = %v, want ErrTargetOutranksActor", err)
	}
}

// Two members with no real role don't outrank each other, even if one of them
// got KICK_MEMBERS through @everyone.
func TestKickMember_NeitherHasARole_Forbidden(t *testing.T) {
	f := newKickFixture()
	f.standings.byUser["everyone-kicker"] = member(nil, role.PermKickMembers)
	if err := f.kick("everyone-kicker", memberID); !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Fatalf("err = %v, want ErrTargetOutranksActor", err)
	}
}

func TestKickMember_CannotKickTheOwner(t *testing.T) {
	f := newKickFixture()
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	if err := f.kick("admin-1", ownerID); !errors.Is(err, model.ErrCannotKickOwner) {
		t.Fatalf("err = %v, want ErrCannotKickOwner", err)
	}
}

func TestKickMember_CannotKickYourself(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(kickerID, kickerID); !errors.Is(err, model.ErrCannotKickSelf) {
		t.Fatalf("err = %v, want ErrCannotKickSelf", err)
	}
}

func TestKickMember_InputValidation(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(kickerID, "   "); !errors.Is(err, model.ErrBanTargetRequired) {
		t.Errorf("blank user_id: err = %v", err)
	}
	if err := f.kick("", memberID); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no identity: err = %v", err)
	}
	if f.repo.kickCalls != 0 {
		t.Error("repo called on invalid input")
	}
}

func TestKickMember_TargetIdIsTrimmed(t *testing.T) {
	f := newKickFixture()
	if err := f.kick(kickerID, "  "+memberID+"  "); err != nil || f.repo.kicked.UserID != memberID {
		t.Fatalf("kicked %q, err = %v", f.repo.kicked.UserID, err)
	}
}

// The repository decides "not a member" (404) and the owner race; the service
// passes those through untouched.
func TestKickMember_RepoErrorsPassThrough(t *testing.T) {
	for _, want := range []error{model.ErrMemberNotFound, model.ErrCannotKickOwner, errors.New("db down")} {
		f := newKickFixture()
		f.repo.kickErr = want
		if err := f.kick(kickerID, memberID); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	}
}

func TestKickMember_ServerNotFound(t *testing.T) {
	svc := moderation_service.New(&fakeServers{err: model.ErrServerNotFound}, &fakeStandings{}, &fakeRepo{})
	err := svc.KickMember(context.Background(), moderation_service.KickParams{
		ServerID: uuid.New(), ActorUserID: ownerID, TargetUserID: memberID,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

func TestKickMember_StandingErrorPropagates(t *testing.T) {
	f := newKickFixture()
	boom := errors.New("db down")
	f.standings.err = boom
	if err := f.kick(kickerID, memberID); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// CA2 at write time: the first checks pass, but inside the transaction the
// target already outranks the kicker (or the kicker lost KICK_MEMBERS).
func TestKickMember_RechecksInsideTheTransaction(t *testing.T) {
	f := newKickFixture()
	promoted := member(rank(1), 0)
	f.repo.txTarget = &promoted
	if err := f.kick(kickerID, memberID); !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Errorf("target promoted: err = %v, want ErrTargetOutranksActor", err)
	}
	if f.repo.kicked.UserID != "" {
		t.Error("target promoted: the kick went through")
	}

	f = newKickFixture()
	demoted := member(rank(2), 0)
	f.repo.txActor = &demoted
	if err := f.kick(kickerID, memberID); !errors.Is(err, model.ErrForbiddenMissingPermission) {
		t.Errorf("kicker lost KICK_MEMBERS: err = %v, want ErrForbiddenMissingPermission", err)
	}
}
