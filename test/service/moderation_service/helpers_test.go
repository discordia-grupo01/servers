package moderation_service_test

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

const (
	ownerID     = "owner-1"
	moderatorID = "mod-1"
	memberID    = "member-1"
)

type fakeServers struct {
	server model.Server
	err    error
}

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, f.err
}

// fakeStandings answers MemberStanding per user; users not in the map are
// non-members.
type fakeStandings struct {
	byUser map[string]role.Standing
	err    error
}

func (f *fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	if f.err != nil {
		return role.Standing{}, f.err
	}
	return f.byUser[userID], nil
}

type fakeRepo struct {
	// standings and ownerID let BanMember hand the check the standings the
	// real repository re-reads under the server lock; wired by newFixture.
	standings *fakeStandings
	ownerID   string
	// txActor/txTarget, if set, replace those standings to simulate a rank
	// that changed after the service's first check.
	txActor, txTarget *role.Standing

	banErr   error
	banCalls int
	banned   moderation.Ban
	events   []outbox.Event

	kickErr    error
	kickCalls  int
	kicked     moderation.Kick
	kickEvents []outbox.Event

	unbanErr      error
	unbanCalls    int
	unbanServerID uuid.UUID
	unbanUserID   string
	unbanEvents   []outbox.Event

	listBans  []moderation.Ban
	listTotal int
	listErr   error
	gotLimit  int
	gotOffset int

	muteErr    error
	muteCalls  int
	muted      moderation.Mute
	muteEvents []outbox.Event

	unmuteMuteErr  error
	unmuteCalls    int
	unmuteServerID uuid.UUID
	unmuteUserID   string
	unmuteEvents   []outbox.Event

	isMuted     bool
	isMutedErr  error
	expireCount int
	expireErr   error
}

func (f *fakeRepo) BanMember(_ context.Context, ban moderation.Ban, check func(actor, target role.Standing) error, events ...outbox.Event) error {
	f.banCalls++
	if f.banErr != nil {
		return f.banErr
	}
	if f.standings != nil {
		actor, target := f.standingOf(*ban.BannedBy), f.standingOf(ban.UserID)
		if f.txActor != nil {
			actor = *f.txActor
		}
		if f.txTarget != nil {
			target = *f.txTarget
		}
		if err := check(actor, target); err != nil {
			return err
		}
	}
	f.banned, f.events = ban, events
	return nil
}

func (f *fakeRepo) KickMember(_ context.Context, kick moderation.Kick, check func(actor, target role.Standing) error, events ...outbox.Event) error {
	f.kickCalls++
	if f.kickErr != nil {
		return f.kickErr
	}
	if f.standings != nil {
		actor, target := f.standingOf(kick.KickedBy), f.standingOf(kick.UserID)
		if f.txActor != nil {
			actor = *f.txActor
		}
		if f.txTarget != nil {
			target = *f.txTarget
		}
		if err := check(actor, target); err != nil {
			return err
		}
	}
	f.kicked, f.kickEvents = kick, events
	return nil
}

func (f *fakeRepo) UnbanMember(_ context.Context, serverID uuid.UUID, userID string, events ...outbox.Event) error {
	f.unbanCalls++
	if f.unbanErr != nil {
		return f.unbanErr
	}
	f.unbanServerID, f.unbanUserID, f.unbanEvents = serverID, userID, events
	return nil
}

func (f *fakeRepo) ListBans(_ context.Context, _ uuid.UUID, limit, offset int) ([]moderation.Ban, int, error) {
	f.gotLimit, f.gotOffset = limit, offset
	return f.listBans, f.listTotal, f.listErr
}

func (f *fakeRepo) MuteMember(_ context.Context, mute moderation.Mute, check func(actor, target role.Standing) error, events ...outbox.Event) error {
	f.muteCalls++
	if f.muteErr != nil {
		return f.muteErr
	}
	if f.standings != nil {
		actor, target := f.standingOf(mute.MutedBy), f.standingOf(mute.UserID)
		if f.txActor != nil {
			actor = *f.txActor
		}
		if f.txTarget != nil {
			target = *f.txTarget
		}
		if err := check(actor, target); err != nil {
			return err
		}
	}
	f.muted, f.muteEvents = mute, events
	return nil
}

func (f *fakeRepo) UnmuteMember(_ context.Context, serverID uuid.UUID, userID string, events ...outbox.Event) error {
	f.unmuteCalls++
	if f.unmuteMuteErr != nil {
		return f.unmuteMuteErr
	}
	f.unmuteServerID, f.unmuteUserID, f.unmuteEvents = serverID, userID, events
	return nil
}

func (f *fakeRepo) IsMuted(context.Context, uuid.UUID, string, time.Time) (bool, error) {
	return f.isMuted, f.isMutedErr
}

func (f *fakeRepo) ExpireMutes(context.Context, time.Time) (int, error) {
	return f.expireCount, f.expireErr
}

func (f *fakeRepo) standingOf(userID string) role.Standing {
	if userID == f.ownerID {
		return role.Standing{IsMember: true, IsOwner: true}
	}
	return f.standings.byUser[userID]
}

func rank(p int) *int { return &p }

// member ranks a member at position p (nil = no roles) with perms.
func member(p *int, perms role.Permission) role.Standing {
	return role.Standing{IsMember: true, Permissions: perms, TopPosition: p}
}

// fixture is a server owned by ownerID where moderatorID holds BAN_MEMBERS at
// rank 2 and memberID has no roles.
type fixture struct {
	server    model.Server
	standings *fakeStandings
	repo      *fakeRepo
	svc       *moderation_service.Service
}

func newFixture() *fixture {
	srv := model.Server{ID: uuid.New(), OwnerID: ownerID, Name: "Test"}
	st := &fakeStandings{byUser: map[string]role.Standing{
		moderatorID: member(rank(2), role.PermBanMembers),
		memberID:    member(nil, 0),
	}}
	repo := &fakeRepo{standings: st, ownerID: ownerID}
	return &fixture{
		server:    srv,
		standings: st,
		repo:      repo,
		svc:       moderation_service.New(&fakeServers{server: srv}, st, repo, repo),
	}
}

func (f *fixture) ban(actor, target string, reason *string) (moderation.Ban, error) {
	return f.svc.BanMember(context.Background(), moderation_service.BanParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target, Reason: reason,
	})
}

func (f *fixture) kick(actor, target string) error {
	return f.svc.KickMember(context.Background(), moderation_service.KickParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target,
	})
}

func (f *fixture) unban(actor, target string) error {
	return f.svc.UnbanMember(context.Background(), moderation_service.UnbanParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target,
	})
}

func str(s string) *string { return &s }
