package moderation_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

func newMuteFixture() *fixture {
	f := newFixture()
	f.standings.byUser[moderatorID] = member(rank(2), role.PermManageMessages)
	return f
}

func (f *fixture) mute(actor, target string, until time.Time, reason *string) error {
	return f.svc.MuteMember(context.Background(), moderation_service.MuteParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target, UntilAt: until, Reason: reason,
	})
}

func (f *fixture) unmute(actor, target string) error {
	return f.svc.UnmuteMember(context.Background(), moderation_service.UnmuteParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target,
	})
}

func TestMuteMember_PersistsAndPublishesEvent(t *testing.T) {
	f := newMuteFixture()
	until := time.Now().UTC().Add(time.Hour)

	if err := f.mute(moderatorID, memberID, until, str("spam")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.repo.muteCalls != 1 || f.repo.muted.UserID != memberID || f.repo.muted.MutedBy != moderatorID {
		t.Errorf("mute = %+v, calls = %d", f.repo.muted, f.repo.muteCalls)
	}
	if len(f.repo.muteEvents) != 1 {
		t.Fatalf("events = %d, want 1", len(f.repo.muteEvents))
	}
	if f.repo.muteEvents[0].RoutingKey != moderationevents.RoutingKeyMemberMuted {
		t.Fatalf("routing key = %q", f.repo.muteEvents[0].RoutingKey)
	}
	var event moderationevents.MemberMuted
	if err := json.Unmarshal(f.repo.muteEvents[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.ServerID != f.server.ID.String() || event.UserID != memberID || event.MutedBy != moderatorID || event.UntilAt.IsZero() || event.Reason != "spam" {
		t.Errorf("event = %+v", event)
	}
}

func TestMuteMember_RequiresManageMessages(t *testing.T) {
	f := newFixture()
	if err := f.mute(moderatorID, memberID, time.Now().UTC().Add(time.Hour), nil); !errors.Is(err, model.ErrForbiddenMissingPermission) {
		t.Fatalf("err = %v, want missing permission", err)
	}
	if f.repo.muteCalls != 0 {
		t.Error("mute repository called without permission")
	}
}

func TestMuteMember_RejectsExpiredDeadline(t *testing.T) {
	f := newMuteFixture()
	if err := f.mute(moderatorID, memberID, time.Now().UTC().Add(-time.Minute), nil); !errors.Is(err, model.ErrMuteUntilInvalid) {
		t.Fatalf("err = %v, want invalid deadline", err)
	}
}

func TestMuteMember_UsesDedicatedValidationErrors(t *testing.T) {
	f := newMuteFixture()
	until := time.Now().UTC().Add(time.Hour)

	if err := f.mute(moderatorID, "   ", until, nil); !errors.Is(err, model.ErrMuteTargetRequired) {
		t.Errorf("blank target: err = %v, want ErrMuteTargetRequired", err)
	}
	if err := f.mute(moderatorID, moderatorID, until, nil); !errors.Is(err, model.ErrCannotMuteSelf) {
		t.Errorf("self target: err = %v, want ErrCannotMuteSelf", err)
	}
	f.standings.byUser["admin-1"] = member(rank(1), role.PermAdministrator)
	if err := f.mute("admin-1", ownerID, until, nil); !errors.Is(err, model.ErrCannotMuteOwner) {
		t.Errorf("owner target: err = %v, want ErrCannotMuteOwner", err)
	}
}

func TestUnmuteMember_PersistsAndPublishesManualEvent(t *testing.T) {
	f := newMuteFixture()
	if err := f.unmute(moderatorID, memberID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.repo.unmuteCalls != 1 || f.repo.unmuteServerID != f.server.ID || f.repo.unmuteUserID != memberID {
		t.Errorf("unmute server=%s user=%q calls=%d", f.repo.unmuteServerID, f.repo.unmuteUserID, f.repo.unmuteCalls)
	}
	if len(f.repo.unmuteEvents) != 1 || f.repo.unmuteEvents[0].RoutingKey != moderationevents.RoutingKeyMemberUnmuted {
		t.Fatalf("unmute events = %+v", f.repo.unmuteEvents)
	}
	var event moderationevents.MemberUnmuted
	if err := json.Unmarshal(f.repo.unmuteEvents[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.UserID != memberID || event.UnmutedBy != moderatorID || event.Reason != "manual" {
		t.Errorf("event = %+v", event)
	}
}

func TestUnmuteMember_UsesDedicatedTargetError(t *testing.T) {
	f := newMuteFixture()
	if err := f.unmute(moderatorID, "   "); !errors.Is(err, model.ErrUnmuteTargetRequired) {
		t.Fatalf("err = %v, want ErrUnmuteTargetRequired", err)
	}
}

// The first checks pass, but inside the transaction the target already
// outranks the moderator (or the moderator lost MANAGE_MESSAGES).
func TestMuteMember_RechecksInsideTheTransaction(t *testing.T) {
	until := time.Now().UTC().Add(time.Hour)

	f := newMuteFixture()
	promoted := member(rank(1), 0)
	f.repo.txTarget = &promoted
	if err := f.mute(moderatorID, memberID, until, nil); !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Errorf("target promoted: err = %v, want ErrTargetOutranksActor", err)
	}
	if f.repo.muted.UserID != "" {
		t.Error("target promoted: the mute went through")
	}

	f = newMuteFixture()
	demoted := member(rank(2), 0)
	f.repo.txActor = &demoted
	if err := f.mute(moderatorID, memberID, until, nil); !errors.Is(err, model.ErrForbiddenMissingPermission) {
		t.Errorf("moderator lost MANAGE_MESSAGES: err = %v, want ErrForbiddenMissingPermission", err)
	}
}
