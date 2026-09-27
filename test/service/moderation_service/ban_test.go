package moderation_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

// CA1: a moderator bans a lower-ranked member.
func TestBanMember_ModeratorBansLowerMember(t *testing.T) {
	f := newFixture()
	ban, err := f.ban(moderatorID, memberID, str("  spam  "))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ban.ServerID != f.server.ID || ban.UserID != memberID || ban.Reason == nil || *ban.Reason != "spam" ||
		ban.BannedBy == nil || *ban.BannedBy != moderatorID || ban.BannedAt.IsZero() {
		t.Errorf("ban = %+v", ban)
	}
	if f.repo.banCalls != 1 || f.repo.banned.UserID != memberID {
		t.Errorf("repo got %+v after %d calls", f.repo.banned, f.repo.banCalls)
	}
}

// The ban publishes member_left (for membership consumers) and member_banned,
// both through the same repository call (same transaction), and the reason
// stays out of the events.
func TestBanMember_PublishesMemberLeftAndMemberBanned(t *testing.T) {
	f := newFixture()
	if _, err := f.ban(moderatorID, memberID, str("private reason")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.repo.events) != 2 {
		t.Fatalf("got %d events, want 2", len(f.repo.events))
	}
	left, banned := f.repo.events[0], f.repo.events[1]
	if left.RoutingKey != membership.RoutingKeyMemberLeft || banned.RoutingKey != moderationevents.RoutingKeyMemberBanned {
		t.Errorf("routing keys = %q, %q", left.RoutingKey, banned.RoutingKey)
	}

	var leftBody membership.MemberLeft
	if err := json.Unmarshal(left.Payload, &leftBody); err != nil {
		t.Fatal(err)
	}
	if leftBody.ServerID != f.server.ID.String() || leftBody.UserID != memberID || leftBody.EventID == "" {
		t.Errorf("member_left = %+v", leftBody)
	}

	var bannedBody moderationevents.MemberBanned
	if err := json.Unmarshal(banned.Payload, &bannedBody); err != nil {
		t.Fatal(err)
	}
	if bannedBody.ServerID != f.server.ID.String() || bannedBody.UserID != memberID ||
		bannedBody.BannedBy != moderatorID || bannedBody.EventID == "" || bannedBody.EventID == leftBody.EventID {
		t.Errorf("member_banned = %+v", bannedBody)
	}
	if strings.Contains(string(banned.Payload), "private reason") {
		t.Error("the reason leaked into the member_banned event")
	}
}

func TestBanMember_OwnerAndAdministratorsCanBan(t *testing.T) {
	f := newFixture()
	if _, err := f.ban(ownerID, moderatorID, nil); err != nil {
		t.Errorf("owner: unexpected error %v", err)
	}

	f = newFixture()
	// ADMINISTRATOR implies BAN_MEMBERS, no need to hold it explicitly.
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	if _, err := f.ban("admin-1", moderatorID, nil); err != nil {
		t.Errorf("administrator: unexpected error %v", err)
	}
}

// CA3: without BAN_MEMBERS (nor admin) the ban is rejected.
func TestBanMember_WithoutPermission_Forbidden(t *testing.T) {
	cases := map[string]string{
		"member without roles": memberID,
		"not a member at all":  "stranger",
	}
	for name, actor := range cases {
		f := newFixture()
		f.standings.byUser["target"] = member(nil, 0)
		_, err := f.ban(actor, "target", nil)
		if !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", name, err)
		}
		if f.repo.banCalls != 0 {
			t.Errorf("%s: repo called", name)
		}
	}

	f := newFixture()
	f.standings.byUser["kicker"] = member(rank(1), role.PermKickMembers|role.PermManageRoles)
	if _, err := f.ban("kicker", memberID, nil); !errors.Is(err, model.ErrForbiddenMissingPermission) {
		t.Errorf("other permissions: err = %v, want ErrForbiddenMissingPermission", err)
	}
}

// CA2: nobody can ban a member with an equal or higher role.
func TestBanMember_TargetWithEqualOrHigherRole_Forbidden(t *testing.T) {
	cases := map[string]int{"equal rank": 2, "higher rank": 1}
	for name, p := range cases {
		f := newFixture()
		f.standings.byUser["target"] = member(rank(p), 0)
		_, err := f.ban(moderatorID, "target", nil)
		if !errors.Is(err, model.ErrTargetOutranksActor) {
			t.Errorf("%s: err = %v, want ErrTargetOutranksActor", name, err)
		}
		if f.repo.banCalls != 0 {
			t.Errorf("%s: repo called", name)
		}
	}

	// Administrators are subject to the hierarchy too.
	f := newFixture()
	f.standings.byUser["admin-1"] = member(rank(3), role.PermAdministrator)
	if _, err := f.ban("admin-1", moderatorID, nil); !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Errorf("admin below target: err = %v, want ErrTargetOutranksActor", err)
	}
}

func TestBanMember_CannotBanTheOwner(t *testing.T) {
	f := newFixture()
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	_, err := f.ban("admin-1", ownerID, nil)
	if !errors.Is(err, model.ErrCannotBanOwner) {
		t.Fatalf("err = %v, want ErrCannotBanOwner", err)
	}
}

func TestBanMember_CannotBanYourself(t *testing.T) {
	f := newFixture()
	_, err := f.ban(moderatorID, moderatorID, nil)
	if !errors.Is(err, model.ErrCannotBanSelf) {
		t.Fatalf("err = %v, want ErrCannotBanSelf", err)
	}
}

func TestBanMember_InputValidation(t *testing.T) {
	f := newFixture()
	if _, err := f.ban(moderatorID, "   ", nil); !errors.Is(err, model.ErrBanTargetRequired) {
		t.Errorf("blank user_id: err = %v", err)
	}
	if _, err := f.ban(moderatorID, memberID, str(strings.Repeat("a", moderation.ReasonMaxLen+1))); !errors.Is(err, moderation.ErrReasonTooLong) {
		t.Errorf("long reason: err = %v", err)
	}
	if _, err := f.ban(moderatorID, memberID, str("bad\x00")); !errors.Is(err, moderation.ErrReasonInvalidChars) {
		t.Errorf("control char: err = %v", err)
	}
	if f.repo.banCalls != 0 {
		t.Error("repo called on invalid input")
	}
	if _, err := f.ban("", memberID, nil); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no identity: err = %v", err)
	}
}

func TestBanMember_BlankReasonIsStoredAsNone(t *testing.T) {
	f := newFixture()
	ban, err := f.ban(moderatorID, memberID, str("   "))
	if err != nil || ban.Reason != nil {
		t.Fatalf("ban.Reason = %v, err = %v; want nil, nil", ban.Reason, err)
	}
}

func TestBanMember_TargetIdIsTrimmed(t *testing.T) {
	f := newFixture()
	ban, err := f.ban(moderatorID, "  "+memberID+"  ", nil)
	if err != nil || ban.UserID != memberID {
		t.Fatalf("ban.UserID = %q, err = %v", ban.UserID, err)
	}
}

// The repository decides "already banned" (409) vs "not a member" (404) and
// the owner race; the service passes those through untouched.
func TestBanMember_RepoErrorsPassThrough(t *testing.T) {
	for _, want := range []error{model.ErrAlreadyBanned, model.ErrMemberNotFound, model.ErrCannotBanOwner, errors.New("db down")} {
		f := newFixture()
		f.repo.banErr = want
		if _, err := f.ban(moderatorID, memberID, nil); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	}
}

func TestBanMember_ServerNotFound(t *testing.T) {
	svc := moderation_service.New(&fakeServers{err: model.ErrServerNotFound}, &fakeStandings{}, &fakeRepo{})
	_, err := svc.BanMember(context.Background(), moderation_service.BanParams{
		ServerID: uuid.New(), ActorUserID: ownerID, TargetUserID: memberID,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

func TestBanMember_StandingErrorPropagates(t *testing.T) {
	f := newFixture()
	boom := errors.New("db down")
	f.standings.err = boom
	if _, err := f.ban(moderatorID, memberID, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
