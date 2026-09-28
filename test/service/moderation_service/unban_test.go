package moderation_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

// #26 CA1: a moderator revokes a ban; the repository gets the ban to delete
// and a single servers.member_unbanned event in the same call.
func TestUnbanMember_ModeratorRevokesBan(t *testing.T) {
	f := newFixture()
	if err := f.unban(moderatorID, "  banned-1  "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.repo.unbanCalls != 1 || f.repo.unbanServerID != f.server.ID || f.repo.unbanUserID != "banned-1" {
		t.Fatalf("repo got server=%s user=%q after %d calls", f.repo.unbanServerID, f.repo.unbanUserID, f.repo.unbanCalls)
	}
	if len(f.repo.unbanEvents) != 1 || f.repo.unbanEvents[0].RoutingKey != moderationevents.RoutingKeyMemberUnbanned {
		t.Fatalf("events = %+v", f.repo.unbanEvents)
	}
	var body moderationevents.MemberUnbanned
	if err := json.Unmarshal(f.repo.unbanEvents[0].Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.ServerID != f.server.ID.String() || body.UserID != "banned-1" || body.UnbannedBy != moderatorID ||
		body.EventID == "" || body.OccurredAt.IsZero() {
		t.Errorf("member_unbanned = %+v", body)
	}
}

func TestUnbanMember_OwnerAndAdministratorsCanUnban(t *testing.T) {
	f := newFixture()
	if err := f.unban(ownerID, "banned-1"); err != nil {
		t.Errorf("owner: unexpected error %v", err)
	}

	f = newFixture()
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	if err := f.unban("admin-1", "banned-1"); err != nil {
		t.Errorf("administrator: unexpected error %v", err)
	}
}

// Unlike banning there is no hierarchy check: a low-ranked moderator can
// revoke a ban placed by someone above them.
func TestUnbanMember_NoHierarchyCheck(t *testing.T) {
	f := newFixture()
	f.standings.byUser["low-mod"] = member(rank(9), role.PermBanMembers)
	if err := f.unban("low-mod", "banned-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// #26 CA2: without BAN_MEMBERS (nor admin) the unban is rejected.
func TestUnbanMember_WithoutPermission_Forbidden(t *testing.T) {
	f := newFixture()
	f.standings.byUser["kicker"] = member(rank(1), role.PermKickMembers|role.PermManageRoles)
	for _, actor := range []string{memberID, "stranger", "kicker"} {
		if err := f.unban(actor, "banned-1"); !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", actor, err)
		}
	}
	if f.repo.unbanCalls != 0 {
		t.Error("repo called")
	}
}

func TestUnbanMember_InputValidation(t *testing.T) {
	f := newFixture()
	if err := f.unban("", "banned-1"); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no identity: err = %v", err)
	}
	if err := f.unban(moderatorID, "   "); !errors.Is(err, model.ErrBanTargetRequired) {
		t.Errorf("blank target: err = %v", err)
	}
	if f.repo.unbanCalls != 0 {
		t.Error("repo called on invalid input")
	}
}

// "not banned" (404) is decided by the repository; the service passes it
// through untouched.
func TestUnbanMember_RepoErrorsPassThrough(t *testing.T) {
	for _, want := range []error{model.ErrBanNotFound, errors.New("db down")} {
		f := newFixture()
		f.repo.unbanErr = want
		if err := f.unban(moderatorID, "banned-1"); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	}
}

func TestUnbanMember_ServerNotFound(t *testing.T) {
	repo := &fakeRepo{}
	svc := moderation_service.New(&fakeServers{err: model.ErrServerNotFound}, &fakeStandings{}, repo)
	err := svc.UnbanMember(context.Background(), moderation_service.UnbanParams{
		ServerID: uuid.New(), ActorUserID: ownerID, TargetUserID: "banned-1",
	})
	if !errors.Is(err, model.ErrServerNotFound) || repo.unbanCalls != 0 {
		t.Fatalf("err = %v, calls = %d", err, repo.unbanCalls)
	}
}

func TestUnbanMember_StandingErrorPropagates(t *testing.T) {
	f := newFixture()
	boom := errors.New("db down")
	f.standings.err = boom
	if err := f.unban(moderatorID, "banned-1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
