package membership_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

// CA1: a non-owner member leaves successfully.
func TestLeaveServer_Member_Success(t *testing.T) {
	srv := memberServer()
	members := &fakeMembers{removed: true}
	svc := newService(&fakeServers{server: srv}, members)

	if err := svc.LeaveServer(context.Background(), leaveParams(srv, testMember)); err != nil {
		t.Fatalf("LeaveServer() error = %v, want nil", err)
	}
	if members.removeCalls != 1 {
		t.Errorf("RemoveMember calls = %d, want 1", members.removeCalls)
	}
	if members.removedUser != testMember {
		t.Errorf("removed user = %q, want %q", members.removedUser, testMember)
	}
}

// CA2: the owner cannot leave directly.
func TestLeaveServer_Owner_Conflict(t *testing.T) {
	srv := memberServer()
	members := &fakeMembers{removed: true}
	svc := newService(&fakeServers{server: srv}, members)

	err := svc.LeaveServer(context.Background(), leaveParams(srv, testOwner))
	if !errors.Is(err, model.ErrOwnerCannotLeave) {
		t.Errorf("err = %v, want ErrOwnerCannotLeave", err)
	}
	if members.removeCalls != 0 {
		t.Error("RemoveMember must not be called when the owner tries to leave")
	}
}

func TestLeaveServer_NotSelf_Forbidden(t *testing.T) {
	srv := memberServer()
	members := &fakeMembers{removed: true}
	svc := newService(&fakeServers{server: srv}, members)

	p := leaveParams(srv, testMember)
	p.TargetUserID = "someone-else"

	err := svc.LeaveServer(context.Background(), p)
	if !errors.Is(err, model.ErrLeaveForbidden) {
		t.Errorf("err = %v, want ErrLeaveForbidden", err)
	}
	if members.removeCalls != 0 {
		t.Error("RemoveMember must not be called when target != actor")
	}
}

func TestLeaveServer_NotAMember_NotFound(t *testing.T) {
	srv := memberServer()
	svc := newService(&fakeServers{server: srv}, &fakeMembers{removed: false})

	err := svc.LeaveServer(context.Background(), leaveParams(srv, testMember))
	if !errors.Is(err, model.ErrMemberNotFound) {
		t.Errorf("err = %v, want ErrMemberNotFound", err)
	}
}

func TestLeaveServer_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeMembers{})

	p := membership_service.LeaveParams{
		ServerID: uuid.New(), ActorUserID: testMember, TargetUserID: testMember,
	}
	if err := svc.LeaveServer(context.Background(), p); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestLeaveServer_Unauthenticated(t *testing.T) {
	svc := newService(&fakeServers{server: memberServer()}, &fakeMembers{})

	p := membership_service.LeaveParams{ServerID: uuid.New()} // no actor
	if err := svc.LeaveServer(context.Background(), p); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestListMembers_Success(t *testing.T) {
	srv := memberServer()
	want := []model.Member{{UserID: testOwner, IsOwner: true}, {UserID: testMember}}
	members := &fakeMembers{isMember: true, listResult: want, listTotal: 2}
	svc := newService(&fakeServers{server: srv}, members)

	got, total, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{
		ServerID: srv.ID, ActorUserID: testMember, Limit: 10, Offset: 0,
	})
	if err != nil {
		t.Fatalf("ListMembers() error = %v, want nil", err)
	}
	if total != 2 || len(got) != 2 {
		t.Errorf("got %d members (total=%d), want 2 (total=2)", len(got), total)
	}
	if members.gotLimit != 10 || members.gotOffset != 0 {
		t.Errorf("repo called with limit=%d offset=%d, want 10, 0", members.gotLimit, members.gotOffset)
	}
}

func TestListMembers_ClampsLimit(t *testing.T) {
	srv := memberServer()
	members := &fakeMembers{isMember: true}
	svc := newService(&fakeServers{server: srv}, members)

	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{"zero uses default", 0, model.MemberListDefaultLimit},
		{"negative uses default", -5, model.MemberListDefaultLimit},
		{"over max is capped", 1000, model.MemberListMaxLimit},
		{"within range is kept", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{
				ServerID: srv.ID, ActorUserID: testMember, Limit: tt.limit,
			})
			if err != nil {
				t.Fatalf("ListMembers() error = %v, want nil", err)
			}
			if members.gotLimit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", members.gotLimit, tt.wantLimit)
			}
		})
	}
}

func TestListMembers_NegativeOffsetClampedToZero(t *testing.T) {
	srv := memberServer()
	members := &fakeMembers{isMember: true}
	svc := newService(&fakeServers{server: srv}, members)

	_, _, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{
		ServerID: srv.ID, ActorUserID: testMember, Offset: -1,
	})
	if err != nil {
		t.Fatalf("ListMembers() error = %v, want nil", err)
	}
	if members.gotOffset != 0 {
		t.Errorf("offset = %d, want 0", members.gotOffset)
	}
}

func TestListMembers_NotAMember_Forbidden(t *testing.T) {
	srv := memberServer()
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: false})

	_, _, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{
		ServerID: srv.ID, ActorUserID: "outsider",
	})
	if !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("err = %v, want ErrForbiddenNotMember", err)
	}
}

func TestListMembers_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeMembers{})

	_, _, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{
		ServerID: uuid.New(), ActorUserID: testMember,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestListMembers_Unauthenticated(t *testing.T) {
	svc := newService(&fakeServers{server: memberServer()}, &fakeMembers{})

	_, _, err := svc.ListMembers(context.Background(), membership_service.ListMembersParams{ServerID: uuid.New()})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}
