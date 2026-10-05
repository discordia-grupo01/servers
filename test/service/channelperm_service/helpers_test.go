package channelperm_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
)

const (
	ownerID   = "owner-1"
	managerID = "manager-1" // MANAGE_ROLES at rank 2
	memberID  = "member-1"  // no roles beyond @everyone
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

type fakeRoles struct {
	byID map[uuid.UUID]role.Role
}

func (f *fakeRoles) GetByID(_ context.Context, id uuid.UUID) (role.Role, error) {
	rl, ok := f.byID[id]
	if !ok {
		return role.Role{}, model.ErrRoleNotFound
	}
	return rl, nil
}

// fakeMembers answers MemberStanding per user; users not in the map are
// non-members.
type fakeMembers struct {
	byUser map[string]role.Standing
	err    error
}

func (f *fakeMembers) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	if f.err != nil {
		return role.Standing{}, f.err
	}
	return f.byUser[userID], nil
}

func (f *fakeMembers) IsMember(_ context.Context, _ uuid.UUID, userID string) (bool, error) {
	return f.byUser[userID].IsMember, f.err
}

type fakeRepo struct {
	members *fakeMembers
	roles   *fakeRoles
	ownerID string
	// txActor, if set, replaces the actor's standing inside the transaction,
	// to simulate a rank or a permission that changed after the first check.
	txActor *role.Standing

	upsertErr   error
	upsertCalls int
	upserted    channelperm.Override

	deleteErr   error
	deleteCalls int
	deleteFound bool // what Delete reports as deleted

	list    []channelperm.Override
	listErr error

	everyone     channelperm.Override
	roleOvs      []channelperm.Override
	forMemberErr error
	forCalls     int
}

func (f *fakeRepo) check(actorID string, roleID uuid.UUID, check func(role.Standing, role.Role) error) error {
	actor := f.members.byUser[actorID]
	if actorID == f.ownerID {
		actor = role.Standing{IsMember: true, IsOwner: true}
	}
	if f.txActor != nil {
		actor = *f.txActor
	}
	return check(actor, f.roles.byID[roleID])
}

func (f *fakeRepo) Upsert(_ context.Context, _ uuid.UUID, actorID string, o channelperm.Override, check func(role.Standing, role.Role) error) (channelperm.Override, error) {
	f.upsertCalls++
	if f.upsertErr != nil {
		return channelperm.Override{}, f.upsertErr
	}
	if err := f.check(actorID, o.RoleID, check); err != nil {
		return channelperm.Override{}, err
	}
	f.upserted = o
	return o, nil
}

func (f *fakeRepo) Delete(_ context.Context, _ uuid.UUID, actorID string, _, roleID uuid.UUID, check func(role.Standing, role.Role) error) (bool, error) {
	f.deleteCalls++
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	if err := f.check(actorID, roleID, check); err != nil {
		return false, err
	}
	return f.deleteFound, nil
}

func (f *fakeRepo) ListByChannel(context.Context, uuid.UUID) ([]channelperm.Override, error) {
	return f.list, f.listErr
}

func (f *fakeRepo) ForMember(context.Context, uuid.UUID, uuid.UUID, string) (channelperm.Override, []channelperm.Override, error) {
	f.forCalls++
	return f.everyone, f.roleOvs, f.forMemberErr
}

func rank(p int) *int { return &p }

func member(p *int, perms role.Permission) role.Standing {
	return role.Standing{IsMember: true, Permissions: perms, TopPosition: p}
}

// fixture: a server owned by ownerID with one channel and three roles --
// @everyone (position 0), "Mod" above the manager's role and "Low" below it.
type fixture struct {
	server             model.Server
	channel            model.Channel
	everyone, mod, low role.Role
	members            *fakeMembers
	repo               *fakeRepo
	svc                *channelperm_service.Service
	otherServerRole    role.Role
	otherServerChannel model.Channel
}

func newFixture() *fixture {
	srv := model.Server{ID: uuid.New(), OwnerID: ownerID, Name: "Test"}
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID, Name: "anuncios", Kind: model.ChannelText}
	otherCh := model.Channel{ID: uuid.New(), ServerID: uuid.New(), Name: "other", Kind: model.ChannelText}
	everyone := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "@everyone", Position: 0, IsEveryone: true, Permissions: role.PermViewChannels | role.PermSendMessages}
	mod := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Mod", Position: 1}
	low := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Low", Position: 3}
	other := role.Role{ID: uuid.New(), ServerID: uuid.New(), Name: "Other", Position: 1}

	members := &fakeMembers{byUser: map[string]role.Standing{
		managerID: member(rank(2), role.PermManageRoles|role.PermViewChannels|role.PermSendMessages),
		memberID:  member(nil, role.PermViewChannels|role.PermSendMessages),
	}}
	roles := &fakeRoles{byID: map[uuid.UUID]role.Role{everyone.ID: everyone, mod.ID: mod, low.ID: low, other.ID: other}}
	repo := &fakeRepo{members: members, roles: roles, ownerID: ownerID}
	channels := &fakeChannels{byID: map[uuid.UUID]model.Channel{ch.ID: ch, otherCh.ID: otherCh}}

	return &fixture{
		server: srv, channel: ch, everyone: everyone, mod: mod, low: low,
		members: members, repo: repo,
		svc:                channelperm_service.New(&fakeServers{server: srv}, channels, roles, members, repo),
		otherServerRole:    other,
		otherServerChannel: otherCh,
	}
}

func (f *fixture) set(actor string, roleID uuid.UUID, allow, deny []string) (channelperm.Override, error) {
	return f.svc.SetOverride(context.Background(), channelperm_service.SetOverrideParams{
		ChannelID: f.channel.ID, RoleID: roleID, ActorUserID: actor, Allow: allow, Deny: deny,
	})
}

func (f *fixture) del(actor string, roleID uuid.UUID) error {
	return f.svc.DeleteOverride(context.Background(), channelperm_service.DeleteOverrideParams{
		ChannelID: f.channel.ID, RoleID: roleID, ActorUserID: actor,
	})
}

func (f *fixture) effective(actor, target string) ([]string, error) {
	return f.svc.EffectivePermissions(context.Background(), channelperm_service.EffectivePermissionsParams{
		ChannelID: f.channel.ID, ActorUserID: actor, TargetUserID: target,
	})
}
