package service_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
)

func genParams(serverID string) invitation.GenerateParams {
	return invitation.GenerateParams{ServerID: serverID, CreatedBy: inviterID}
}

func memberRepo() *fakeInviteRepo {
	return &fakeInviteRepo{exists: true, member: true}
}

func TestGenerate_CreatesAUniqueLink(t *testing.T) {
	repo := memberRepo()
	inv, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Code == "" {
		t.Fatal("no invitation code was generated")
	}
	if repo.created.Code != inv.Code {
		t.Errorf("persisted code %q != returned code %q", repo.created.Code, inv.Code)
	}
	if inv.ID == (uuid.UUID{}) {
		t.Error("invitation id was not generated")
	}
}

func TestGenerate_CodeIsURLSafe(t *testing.T) {
	inv, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(inv.Code) {
		t.Errorf("code %q is not URL-safe", inv.Code)
	}
}

func TestGenerate_TwoInvitationsGetDifferentCodes(t *testing.T) {
	svc := newInviteService(memberRepo())
	a, _ := svc.GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	b, _ := svc.GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if a.Code == b.Code {
		t.Errorf("both invitations share the code %q", a.Code)
	}
}

func TestGenerate_WithoutMaxUses_HasNoLimit(t *testing.T) {
	inv, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err != nil {
		t.Fatal(err)
	}
	if inv.MaxUses != nil {
		t.Errorf("max_uses = %v, want nil", *inv.MaxUses)
	}
	if inv.Uses != 0 {
		t.Errorf("uses = %d, want 0", inv.Uses)
	}
}

func TestGenerate_WithMaxUses(t *testing.T) {
	p := genParams(uuid.NewString())
	p.MaxUses = ptrInt(10)

	inv, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if inv.MaxUses == nil || *inv.MaxUses != 10 {
		t.Errorf("max_uses = %v, want 10", inv.MaxUses)
	}
}

func TestGenerate_ExpiresAfterTheConfiguredTTL(t *testing.T) {
	before := time.Now().UTC()
	inv, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExpiresAt == nil {
		t.Fatal("expires_at is nil, want a fixed TTL from creation")
	}
	want := before.Add(invitation.TTL)
	if d := inv.ExpiresAt.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("expires_at = %v, want ~%v (TTL %v)", inv.ExpiresAt, want, invitation.TTL)
	}
}

func TestGenerate_RetriesOnCodeCollision(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, collisions: 2}
	inv, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createCalls != 3 {
		t.Errorf("createCalls = %d, want 3", repo.createCalls)
	}
	if repo.created.Code != inv.Code {
		t.Error("the surviving code was not the persisted one")
	}
}

func TestGenerate_GivesUpAfterTooManyCollisions(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, collisions: 99}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInviteCodeUnavailable) {
		t.Fatalf("err = %v, want ErrInviteCodeUnavailable", err)
	}
}

func TestGenerate_RequiresIdentity(t *testing.T) {
	p := genParams(uuid.NewString())
	p.CreatedBy = ""
	_, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), p)
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestGenerate_UnknownServer(t *testing.T) {
	repo := &fakeInviteRepo{exists: false}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInviteServerNotFound) {
		t.Fatalf("err = %v, want ErrInviteServerNotFound", err)
	}
}

func TestGenerate_MalformedServerID(t *testing.T) {
	_, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), genParams("not-a-uuid"))
	if !errors.Is(err, model.ErrInviteServerNotFound) {
		t.Fatalf("err = %v, want ErrInviteServerNotFound", err)
	}
}

func TestGenerate_NonMemberCannotInvite(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: false}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.createCalls != 0 {
		t.Error("an invitation was persisted for a non-member")
	}
}

func TestGenerate_RejectsNonPositiveMaxUses(t *testing.T) {
	for _, n := range []int{0, -3} {
		p := genParams(uuid.NewString())
		p.MaxUses = ptrInt(n)
		_, err := newInviteService(memberRepo()).GenerateInvitation(context.Background(), p)
		if !errors.Is(err, model.ErrInviteMaxUsesInvalid) {
			t.Fatalf("max_uses=%d: err = %v, want ErrInviteMaxUsesInvalid", n, err)
		}
	}
}

func TestGenerate_PropagatesRepositoryError(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, createErr: errors.New("db down")}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if err == nil || errors.Is(err, model.ErrInviteCodeUnavailable) {
		t.Fatalf("err = %v, want the raw repository error", err)
	}
}

func listParams(serverID string) invitation.ListParams {
	return invitation.ListParams{ServerID: serverID, UserID: inviterID}
}

func TestList_ReturnsWhateverTheRepoGives(t *testing.T) {
	serverID := uuid.NewString()
	want := []model.Invitation{activeInvitation(uuid.New()), activeInvitation(uuid.New())}
	repo := &fakeInviteRepo{exists: true, member: true, listResult: want}

	got, err := newInviteService(repo).ListInvitations(context.Background(), listParams(serverID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d invitations, want %d", len(got), len(want))
	}
	if repo.listCalls != 1 {
		t.Errorf("ListByServer calls = %d, want 1", repo.listCalls)
	}
}

func TestList_RequiresIdentity(t *testing.T) {
	p := listParams(uuid.NewString())
	p.UserID = ""
	_, err := newInviteService(memberRepo()).ListInvitations(context.Background(), p)
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestList_UnknownServer(t *testing.T) {
	repo := &fakeInviteRepo{exists: false}
	_, err := newInviteService(repo).ListInvitations(context.Background(), listParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInviteServerNotFound) {
		t.Fatalf("err = %v, want ErrInviteServerNotFound", err)
	}
}

func TestList_MalformedServerID(t *testing.T) {
	_, err := newInviteService(memberRepo()).ListInvitations(context.Background(), listParams("not-a-uuid"))
	if !errors.Is(err, model.ErrInviteServerNotFound) {
		t.Fatalf("err = %v, want ErrInviteServerNotFound", err)
	}
}

func TestList_NonMemberCannotList(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: false}
	_, err := newInviteService(repo).ListInvitations(context.Background(), listParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.listCalls != 0 {
		t.Error("ListByServer must not be called for a non-member")
	}
}

func revokeSetup(inv model.Invitation, member bool) *fakeInviteRepo {
	return &fakeInviteRepo{
		member: member,
		byCode: map[string]model.Invitation{inv.Code: inv},
	}
}

func TestRevoke_DeactivatesActiveInvitation(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, true)

	before := time.Now().UTC()
	err := newInviteService(repo).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: inv.Code, UserID: inviterID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokedCode != inv.Code {
		t.Errorf("revoked code = %q, want %q", repo.revokedCode, inv.Code)
	}
	if repo.revokedAt.Before(before) {
		t.Errorf("revoked_at %v is before the call", repo.revokedAt)
	}
}

func TestRevoke_UnknownInvitation(t *testing.T) {
	repo := &fakeInviteRepo{member: true, byCode: map[string]model.Invitation{}}
	err := newInviteService(repo).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: "ghost", UserID: inviterID,
	})
	if !errors.Is(err, model.ErrInvitationNotFound) {
		t.Fatalf("err = %v, want ErrInvitationNotFound", err)
	}
}

func TestRevoke_NonMemberCannotRevoke(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, false)
	err := newInviteService(repo).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: inv.Code, UserID: "intruder",
	})
	if !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.revokeCalls != 0 {
		t.Error("revoke was persisted for a non-member")
	}
}

func TestRevoke_IsIdempotent(t *testing.T) {
	inv := activeInvitation(uuid.New())
	revokedAt := time.Now().UTC().Add(-time.Minute)
	inv.RevokedAt = &revokedAt
	repo := revokeSetup(inv, true)

	err := newInviteService(repo).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: inv.Code, UserID: inviterID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokeCalls != 0 {
		t.Error("an already-revoked invitation was revoked again")
	}
}

func TestRevoke_RequiresIdentity(t *testing.T) {
	err := newInviteService(&fakeInviteRepo{}).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: "x", UserID: "",
	})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func joinRepo(inv model.Invitation) *fakeInviteRepo {
	return &fakeInviteRepo{
		byCode:       map[string]model.Invitation{inv.Code: inv},
		redeemServer: inv.ServerID,
	}
}

func join(repo *fakeInviteRepo, code string) (invitation.JoinResult, error) {
	return newInviteService(repo).JoinServer(context.Background(), invitation.JoinParams{
		Code: code, UserID: joinerID,
	})
}

func TestJoin_ValidInvitation_MakesTheUserAMember(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)

	res, err := join(repo, inv.Code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AlreadyMember {
		t.Error("AlreadyMember = true, want false for a fresh join")
	}
	if res.ServerID != inv.ServerID {
		t.Errorf("server = %s, want %s", res.ServerID, inv.ServerID)
	}
	if repo.redeemCalls != 1 {
		t.Errorf("redeemCalls = %d, want 1", repo.redeemCalls)
	}
}

func TestJoin_UnknownInvitation(t *testing.T) {
	repo := &fakeInviteRepo{byCode: map[string]model.Invitation{}}
	_, err := join(repo, "ghost")
	if !errors.Is(err, model.ErrInvitationNotFound) {
		t.Fatalf("err = %v, want ErrInvitationNotFound", err)
	}
}

func TestJoin_RevokedInvitation(t *testing.T) {
	inv := activeInvitation(uuid.New())
	revokedAt := time.Now().UTC().Add(-time.Minute)
	inv.RevokedAt = &revokedAt
	repo := joinRepo(inv)

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrInvitationRevoked) {
		t.Fatalf("err = %v, want ErrInvitationRevoked", err)
	}
	if repo.redeemCalls != 0 {
		t.Error("a revoked invitation was redeemed")
	}
}

func TestJoin_ExpiredInvitation(t *testing.T) {
	inv := activeInvitation(uuid.New())
	inv.ExpiresAt = past()
	repo := joinRepo(inv)

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrInvitationExpired) {
		t.Fatalf("err = %v, want ErrInvitationExpired", err)
	}
	if repo.redeemCalls != 0 {
		t.Error("an expired invitation was redeemed")
	}
}

func TestJoin_UsesLimitReached(t *testing.T) {
	inv := activeInvitation(uuid.New())
	inv.MaxUses = ptrInt(2)
	inv.Uses = 2
	repo := joinRepo(inv)

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted", err)
	}
	if repo.redeemCalls != 0 {
		t.Error("an exhausted invitation was redeemed")
	}
}

func TestJoin_LastUseStillWorks(t *testing.T) {
	inv := activeInvitation(uuid.New())
	inv.MaxUses = ptrInt(3)
	inv.Uses = 2
	repo := joinRepo(inv)

	res, err := join(repo, inv.Code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AlreadyMember || res.ServerID != inv.ServerID {
		t.Errorf("res = %+v", res)
	}
}

func TestJoin_AlreadyMember_DoesNotDuplicateNorConsumeUse(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.member = true

	res, err := join(repo, inv.Code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.AlreadyMember {
		t.Error("AlreadyMember = false, want true")
	}
	if res.ServerID != inv.ServerID {
		t.Errorf("server = %s, want %s", res.ServerID, inv.ServerID)
	}
	if repo.redeemCalls != 0 {
		t.Error("redeem was called for an existing member")
	}
}

func TestJoin_AlreadyMember_EvenIfInvitationIsExpired(t *testing.T) {
	inv := activeInvitation(uuid.New())
	inv.ExpiresAt = past()
	repo := joinRepo(inv)
	repo.member = true

	res, err := join(repo, inv.Code)
	if err != nil {
		t.Fatalf("a member should still be routed in: %v", err)
	}
	if !res.AlreadyMember {
		t.Error("AlreadyMember = false, want true")
	}
}

func TestJoin_BannedUser_IsRejected(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.banned = true

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrUserBanned) {
		t.Fatalf("err = %v, want ErrUserBanned", err)
	}
	if repo.redeemCalls != 0 {
		t.Error("a banned user redeemed an invitation")
	}
}

func TestJoin_BanTakesPrecedenceOverMembership(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.banned = true
	repo.member = true

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrUserBanned) {
		t.Fatalf("err = %v, want ErrUserBanned", err)
	}
}

func TestJoin_RequiresIdentity(t *testing.T) {
	_, err := newInviteService(&fakeInviteRepo{}).JoinServer(context.Background(), invitation.JoinParams{
		Code: "x", UserID: "",
	})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestJoin_RedeemRaceReportsAlreadyMember(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.redeemErr = model.ErrAlreadyMember

	res, err := join(repo, inv.Code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.AlreadyMember || res.ServerID != inv.ServerID {
		t.Errorf("res = %+v", res)
	}
}

func TestJoin_RedeemRaceReportsExhausted(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.redeemErr = model.ErrInvitationExhausted

	_, err := join(repo, inv.Code)
	if !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted", err)
	}
}

// RF1: a successful new join publishes servers.member_joined with the right
// payload shape.
func TestJoin_Success_PublishesMemberJoined(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	pub := &fakePublisher{}

	res, err := newInviteServiceWithPublisher(repo, pub).JoinServer(context.Background(), invitation.JoinParams{
		Code: inv.Code, UserID: joinerID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("published events = %d, want 1", len(pub.published))
	}
	got := pub.published[0]
	if got.routingKey != events.RoutingKeyMemberJoined {
		t.Errorf("routing key = %q, want %q", got.routingKey, events.RoutingKeyMemberJoined)
	}
	evt, ok := got.payload.(events.MembershipEvent)
	if !ok {
		t.Fatalf("payload type = %T, want events.MembershipEvent", got.payload)
	}
	if evt.EventID == "" {
		t.Error("event_id must not be empty")
	}
	if evt.ServerID != res.ServerID.String() {
		t.Errorf("server_id = %q, want %q", evt.ServerID, res.ServerID.String())
	}
	if evt.UserID != joinerID {
		t.Errorf("user_id = %q, want %q", evt.UserID, joinerID)
	}
	if evt.OccurredAt.IsZero() {
		t.Error("occurred_at must not be zero")
	}
}

// CB: joining fails at the DB step -> nothing gets published.
func TestJoin_RedeemFails_DoesNotPublish(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.redeemErr = errors.New("db down")
	pub := &fakePublisher{}

	_, err := newInviteServiceWithPublisher(repo, pub).JoinServer(context.Background(), invitation.JoinParams{
		Code: inv.Code, UserID: joinerID,
	})
	if err == nil {
		t.Fatal("expected an error from RedeemInvitation")
	}
	if len(pub.published) != 0 {
		t.Errorf("published = %d events, want 0 when the DB write fails", len(pub.published))
	}
}

// CB: the user was already a member -> no new join happened, no event.
func TestJoin_AlreadyMember_DoesNotPublish(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.member = true
	pub := &fakePublisher{}

	_, err := newInviteServiceWithPublisher(repo, pub).JoinServer(context.Background(), invitation.JoinParams{
		Code: inv.Code, UserID: joinerID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pub.published) != 0 {
		t.Errorf("published = %d events, want 0 for an already-member join", len(pub.published))
	}
}

// CB: a concurrent redeem race that reports already-member must not publish
// either -- same reasoning, no new membership was created.
func TestJoin_RedeemRaceAlreadyMember_DoesNotPublish(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := joinRepo(inv)
	repo.redeemErr = model.ErrAlreadyMember
	pub := &fakePublisher{}

	_, err := newInviteServiceWithPublisher(repo, pub).JoinServer(context.Background(), invitation.JoinParams{
		Code: inv.Code, UserID: joinerID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pub.published) != 0 {
		t.Errorf("published = %d events, want 0", len(pub.published))
	}
}
