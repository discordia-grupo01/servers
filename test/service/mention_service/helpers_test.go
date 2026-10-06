package mention_service_test

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
)

const (
	ownerID    = "owner-1"
	authorID   = "author-1" // plain member: default permissions, no MENTION_EVERYONE
	pingerID   = "pinger-1" // member whose role grants MENTION_EVERYONE
	adminID    = "admin-1"  // member whose role grants ADMINISTRATOR
	aliceID    = "alice"
	bobID      = "bob"
	carolID    = "carol" // member without VIEW_CHANNELS
	strangerID = "stranger"
)

type fakeServers struct {
	server model.Server
	err    error
}

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, f.err
}

type fakeChannels struct {
	byID map[uuid.UUID]model.Channel
}

func (f *fakeChannels) GetByID(_ context.Context, id uuid.UUID) (model.Channel, error) {
	ch, ok := f.byID[id]
	if !ok {
		return model.Channel{}, model.ErrChannelNotFound
	}
	return ch, nil
}

// fakeMembers models a server's members: their standing, the roles they hold
// and whether they can see the channels. The owner is not in standings (the
// service never asks: ResolveStanding short-circuits), but is a member who
// always sees, like in the real query.
type fakeMembers struct {
	ownerID   string
	standings map[string]role.Standing
	roles     map[uuid.UUID][]string // role id -> holders
	blind     map[string]bool        // members without VIEW_CHANNELS
	err       error

	calls []string
}

func (f *fakeMembers) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	return f.standings[userID], nil
}

func (f *fakeMembers) isMember(id string) bool {
	_, ok := f.standings[id]
	return ok || id == f.ownerID
}

func (f *fakeMembers) FilterMembers(_ context.Context, _ uuid.UUID, userIDs []string) ([]string, error) {
	f.calls = append(f.calls, "FilterMembers")
	if f.err != nil {
		return nil, f.err
	}
	var out []string
	for _, id := range userIDs {
		if f.isMember(id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeMembers) MemberIDsWithRoles(_ context.Context, _ uuid.UUID, roleIDs []uuid.UUID) ([]string, error) {
	f.calls = append(f.calls, "MemberIDsWithRoles")
	var out []string
	for _, id := range roleIDs {
		out = append(out, f.roles[id]...)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func (f *fakeMembers) ViewersAmong(_ context.Context, _ uuid.UUID, ownerID string, userIDs []string) ([]string, error) {
	f.calls = append(f.calls, "ViewersAmong")
	var out []string
	for _, id := range userIDs {
		if f.isMember(id) && (id == ownerID || !f.blind[id]) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeMembers) AllViewers(ctx context.Context, serverID uuid.UUID, ownerID string) ([]string, error) {
	f.calls = append(f.calls, "AllViewers")
	all := []string{ownerID}
	for id := range f.standings {
		all = append(all, id)
	}
	slices.Sort(all)
	viewers, _ := f.ViewersAmong(ctx, serverID, ownerID, all)
	f.calls = f.calls[:len(f.calls)-1]
	return viewers, nil
}

func (f *fakeMembers) called(name string) bool { return slices.Contains(f.calls, name) }

type fakeRoles struct {
	ofServer    map[uuid.UUID]bool
	everyoneID  uuid.UUID
	filterCalls int
}

func (f *fakeRoles) FilterMentionable(_ context.Context, _ uuid.UUID, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	f.filterCalls++
	var out []uuid.UUID
	for _, id := range roleIDs {
		if f.ofServer[id] && id != f.everyoneID {
			out = append(out, id)
		}
	}
	return out, nil
}

// fixture is a server owned by ownerID with a text channel, where:
//   - moderators = {alice, bob}, students = {bob, carol}
//   - carol can't see channels
//   - pingerID holds MENTION_EVERYONE, adminID holds ADMINISTRATOR, authorID
//     only the default permissions
type fixture struct {
	server     model.Server
	channelID  uuid.UUID
	moderators uuid.UUID
	students   uuid.UUID
	everyoneID uuid.UUID
	members    *fakeMembers
	roles      *fakeRoles
	svc        *mention_service.Service
}

func newFixture() *fixture {
	srv := model.Server{ID: uuid.New(), OwnerID: ownerID, Name: "Test"}
	channelID := uuid.New()
	moderators, students, everyone := uuid.New(), uuid.New(), uuid.New()

	plain := role.Standing{IsMember: true, Permissions: role.DefaultPermissions}
	members := &fakeMembers{
		ownerID: ownerID,
		standings: map[string]role.Standing{
			authorID: plain,
			pingerID: {IsMember: true, Permissions: role.DefaultPermissions | role.PermMentionEveryone},
			adminID:  {IsMember: true, Permissions: role.PermAdministrator},
			aliceID:  plain,
			bobID:    plain,
			carolID:  {IsMember: true},
		},
		roles: map[uuid.UUID][]string{
			moderators: {aliceID, bobID},
			students:   {bobID, carolID},
		},
		blind: map[string]bool{carolID: true},
	}
	roles := &fakeRoles{
		ofServer:   map[uuid.UUID]bool{moderators: true, students: true, everyone: true},
		everyoneID: everyone,
	}
	channels := &fakeChannels{byID: map[uuid.UUID]model.Channel{
		channelID: {ID: channelID, ServerID: srv.ID, Name: "general", Kind: model.ChannelText},
	}}

	return &fixture{
		server: srv, channelID: channelID,
		moderators: moderators, students: students, everyoneID: everyone,
		members: members, roles: roles,
		svc: mention_service.New(&fakeServers{server: srv}, channels, members, roles),
	}
}

func (f *fixture) resolve(actor string, users []string, roles []uuid.UUID, everyone bool) (mention.Resolution, error) {
	return f.svc.Resolve(context.Background(), mention_service.ResolveParams{
		ServerID: f.server.ID, ChannelID: f.channelID, ActorUserID: actor,
		UserIDs: users, RoleIDs: roles, Everyone: everyone,
	})
}
