package moderation_service_test

import (
	"context"

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
	banErr   error
	banCalls int
	banned   moderation.Ban
	events   []outbox.Event

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
}

func (f *fakeRepo) BanMember(_ context.Context, ban moderation.Ban, events ...outbox.Event) error {
	f.banCalls++
	if f.banErr != nil {
		return f.banErr
	}
	f.banned, f.events = ban, events
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
	repo := &fakeRepo{}
	return &fixture{
		server:    srv,
		standings: st,
		repo:      repo,
		svc:       moderation_service.New(&fakeServers{server: srv}, st, repo),
	}
}

func (f *fixture) ban(actor, target string, reason *string) (moderation.Ban, error) {
	return f.svc.BanMember(context.Background(), moderation_service.BanParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target, Reason: reason,
	})
}

func (f *fixture) unban(actor, target string) error {
	return f.svc.UnbanMember(context.Background(), moderation_service.UnbanParams{
		ServerID: f.server.ID, ActorUserID: actor, TargetUserID: target,
	})
}

func str(s string) *string { return &s }
