package mention_service_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
)

func assertIDs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// CA1: mentioning a member notifies them.
func TestResolve_UserMention_NotifiesTheMentionedMember(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{aliceID}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "user_ids", res.UserIDs, []string{aliceID})
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID})
}

// CA1 doesn't need MENTION_EVERYONE: any member can mention a user.
func TestResolve_UserMention_NeedsNoSpecialPermission(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{bobID, aliceID}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID, bobID})
}

func TestResolve_UserMention_NonMemberIsIgnored(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{strangerID, aliceID}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "user_ids", res.UserIDs, []string{aliceID})
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID})
}

// A member who can't see the channel is still a valid mention (the text
// names them) but is not notified about a message they can't read.
func TestResolve_UserMention_MemberWhoCantSeeTheChannelIsNotNotified(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{carolID}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "user_ids", res.UserIDs, []string{carolID})
	if len(res.RecipientIDs) != 0 {
		t.Errorf("recipients = %v, want none", res.RecipientIDs)
	}
}

func TestResolve_DuplicatesAndBlanksCountOnce(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, []string{aliceID, " alice ", "", aliceID}, []uuid.UUID{f.moderators, f.moderators}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "user_ids", res.UserIDs, []string{aliceID})
	if len(res.RoleIDs) != 1 {
		t.Errorf("role_ids = %v, want one", res.RoleIDs)
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID, bobID})
}

// D9: the author is never notified of their own message.
func TestResolve_AuthorIsNeverARecipient(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(aliceID, []string{aliceID, bobID}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "user_ids", res.UserIDs, []string{aliceID, bobID})
	assertIDs(t, "recipients", res.RecipientIDs, []string{bobID})
}

// CA2: with MENTION_EVERYONE, a role mention reaches every holder who can see
// the channel.
func TestResolve_RoleMention_WithPermission_NotifiesItsHolders(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, nil, []uuid.UUID{f.students}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !slices.Equal(res.RoleIDs, []uuid.UUID{f.students}) {
		t.Errorf("role_ids = %v, want [%v]", res.RoleIDs, f.students)
	}
	// carol holds the role but can't see channels.
	assertIDs(t, "recipients", res.RecipientIDs, []string{bobID})
}

func TestResolve_RoleAndUserMentions_RecipientsAreTheUnion(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, []string{ownerID}, []uuid.UUID{f.moderators, f.students}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID, bobID, ownerID})
}

// CA4 for roles: without MENTION_EVERYONE the role mention is dropped (plain
// text) instead of failing, and the servers' roles aren't even looked up.
func TestResolve_RoleMention_WithoutPermission_IsDroppedNotRejected(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{aliceID}, []uuid.UUID{f.students}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.RoleIDs) != 0 {
		t.Errorf("role_ids = %v, want none", res.RoleIDs)
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID})
	if f.roles.filterCalls != 0 || f.members.called("MemberIDsWithRoles") {
		t.Error("roles should not be expanded without MENTION_EVERYONE")
	}
}

func TestResolve_RoleMention_ForeignRoleAndEveryoneRoleIdAreIgnored(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, nil, []uuid.UUID{uuid.New(), f.everyoneID}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.RoleIDs) != 0 || res.Everyone || len(res.RecipientIDs) != 0 {
		t.Errorf("got %+v, want an empty resolution", res)
	}
}

// CA3: with MENTION_EVERYONE, @everyone reaches every member who can see the
// channel (the owner included), except the author.
func TestResolve_Everyone_WithPermission_NotifiesEveryViewer(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, nil, nil, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Everyone {
		t.Error("everyone = false, want true")
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{adminID, aliceID, authorID, bobID, ownerID})
	if f.members.called("ViewersAmong") {
		t.Error("@everyone already reaches everybody: no need to expand the other mentions")
	}
}

// CA3 holds for the owner and administrators without the explicit grant.
func TestResolve_Everyone_OwnerAndAdministratorHoldThePermissionImplicitly(t *testing.T) {
	for _, actor := range []string{ownerID, adminID} {
		t.Run(actor, func(t *testing.T) {
			f := newFixture()
			res, err := f.resolve(actor, nil, []uuid.UUID{f.moderators}, true)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !res.Everyone || len(res.RoleIDs) != 1 {
				t.Errorf("got %+v, want everyone and the role to hold", res)
			}
			if slices.Contains(res.RecipientIDs, actor) {
				t.Errorf("recipients %v include the author", res.RecipientIDs)
			}
		})
	}
}

// CA4: without MENTION_EVERYONE, @everyone is plain text: no error, no
// notification, but the user mentions of the same message still work.
func TestResolve_Everyone_WithoutPermission_IsPlainText(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(authorID, []string{bobID}, nil, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Everyone {
		t.Error("everyone = true, want false without MENTION_EVERYONE")
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{bobID})
	if f.members.called("AllViewers") {
		t.Error("@everyone without permission must not expand to every member")
	}
}

func TestResolve_NoMentions_DoesNoLookups(t *testing.T) {
	f := newFixture()

	res, err := f.resolve(pingerID, nil, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.UserIDs)+len(res.RoleIDs)+len(res.RecipientIDs) != 0 || res.Everyone {
		t.Errorf("got %+v, want an empty resolution", res)
	}
	if len(f.members.calls) != 0 || f.roles.filterCalls != 0 {
		t.Errorf("unexpected lookups: %v, %d role filters", f.members.calls, f.roles.filterCalls)
	}
}

func TestResolve_ActorNotAMember_IsForbidden(t *testing.T) {
	f := newFixture()

	_, err := f.resolve(strangerID, []string{aliceID}, nil, false)
	if !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("err = %v, want ErrForbiddenNotMember", err)
	}
}

func TestResolve_NoActor_IsUnauthorized(t *testing.T) {
	f := newFixture()

	_, err := f.resolve("", []string{aliceID}, nil, false)
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestResolve_UnknownChannel_IsNotFound(t *testing.T) {
	f := newFixture()

	_, err := f.svc.Resolve(context.Background(), mention_service.ResolveParams{
		ServerID: f.server.ID, ChannelID: uuid.New(), ActorUserID: aliceID, UserIDs: []string{bobID},
	})
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestResolve_ChannelOfAnotherServer_IsRejected(t *testing.T) {
	f := newFixture()
	other := uuid.New()
	channels := &fakeChannels{byID: map[uuid.UUID]model.Channel{
		f.channelID: {ID: f.channelID, ServerID: other},
	}}
	svc := mention_service.New(&fakeServers{server: f.server}, channels, f.members, f.roles)

	_, err := svc.Resolve(context.Background(), mention_service.ResolveParams{
		ServerID: f.server.ID, ChannelID: f.channelID, ActorUserID: aliceID, UserIDs: []string{bobID},
	})
	if !errors.Is(err, model.ErrChannelServerMismatch) {
		t.Errorf("err = %v, want ErrChannelServerMismatch", err)
	}
}

func TestResolve_UnknownServer_IsNotFound(t *testing.T) {
	f := newFixture()
	svc := mention_service.New(&fakeServers{err: model.ErrServerNotFound}, &fakeChannels{}, f.members, f.roles)

	_, err := svc.Resolve(context.Background(), mention_service.ResolveParams{
		ServerID: uuid.New(), ChannelID: f.channelID, ActorUserID: aliceID,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestResolve_TooManyMentions_AreRejectedBeforeAnyLookup(t *testing.T) {
	f := newFixture()

	users := make([]string, mention.MaxUserIDs+1)
	for i := range users {
		users[i] = fmt.Sprintf("user-%d", i)
	}
	if _, err := f.resolve(pingerID, users, nil, false); !errors.Is(err, mention.ErrTooManyUsers) {
		t.Errorf("users: err = %v, want ErrTooManyUsers", err)
	}

	roles := make([]uuid.UUID, mention.MaxRoleIDs+1)
	for i := range roles {
		roles[i] = uuid.New()
	}
	if _, err := f.resolve(pingerID, nil, roles, false); !errors.Is(err, mention.ErrTooManyRoles) {
		t.Errorf("roles: err = %v, want ErrTooManyRoles", err)
	}

	if len(f.members.calls) != 0 {
		t.Errorf("unexpected lookups: %v", f.members.calls)
	}
}

// The cap is on distinct mentions: repeating the same user many times is fine.
func TestResolve_RepeatedMentionsDontCountTowardsTheCap(t *testing.T) {
	f := newFixture()

	users := make([]string, mention.MaxUserIDs*2)
	for i := range users {
		users[i] = aliceID
	}
	res, err := f.resolve(authorID, users, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertIDs(t, "recipients", res.RecipientIDs, []string{aliceID})
}

func TestResolve_RepositoryError_IsReturned(t *testing.T) {
	f := newFixture()
	boom := errors.New("db down")
	f.members.err = boom

	if _, err := f.resolve(authorID, []string{aliceID}, nil, false); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

// Exactly the cap is still allowed; one more is what gets rejected.
func TestResolve_MentionsAtTheCap_AreAllowed(t *testing.T) {
	f := newFixture()

	users := make([]string, mention.MaxUserIDs)
	for i := range users {
		users[i] = fmt.Sprintf("user-%d", i)
	}
	if _, err := f.resolve(pingerID, users, nil, false); err != nil {
		t.Errorf("users: %d mentions must be allowed: %v", mention.MaxUserIDs, err)
	}

	roles := make([]uuid.UUID, mention.MaxRoleIDs)
	for i := range roles {
		roles[i] = uuid.New()
	}
	if _, err := f.resolve(pingerID, nil, roles, false); err != nil {
		t.Errorf("roles: %d mentions must be allowed: %v", mention.MaxRoleIDs, err)
	}
}
