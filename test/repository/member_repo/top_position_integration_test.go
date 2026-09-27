package member_repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

// MemberStanding reports the position of the member's highest role, or nil
// when they hold no role (the lowest rank).
func TestMemberRepo_MemberStanding_TopPosition(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	st, err := members.MemberStanding(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	if st.TopPosition != nil {
		t.Errorf("without roles TopPosition = %d, want nil", *st.TopPosition)
	}

	_ = seedRole(ctx, t, roles, srv.ID, "Top")    // position 1, not assigned
	mid := seedRole(ctx, t, roles, srv.ID, "Mid") // position 2
	low := seedRole(ctx, t, roles, srv.ID, "Low") // position 3
	if err := members.AssignRole(ctx, srv.ID, member, low.ID); err != nil {
		t.Fatal(err)
	}
	if err := members.AssignRole(ctx, srv.ID, member, mid.ID); err != nil {
		t.Fatal(err)
	}

	st, err = members.MemberStanding(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	if st.TopPosition == nil || *st.TopPosition != 2 {
		t.Errorf("TopPosition = %v, want 2 (their highest role, Mid)", st.TopPosition)
	}
}

// #56 CA1: the new order applies right away. The rank that the ban's
// hierarchy check reads (TopPosition) follows the reordered positions, with
// no extra step: moving a member's role to the top makes them outrank members
// they were below before.
func TestMemberRepo_MemberStanding_FollowsReorder(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	top := seedRole(ctx, t, roles, srv.ID, "Top") // position 1
	low := seedRole(ctx, t, roles, srv.ID, "Low") // position 2
	if err := members.AssignRole(ctx, srv.ID, member, low.ID); err != nil {
		t.Fatal(err)
	}
	other := "other-member"
	if _, err := pool.Exec(ctx, `INSERT INTO server_members (server_id, user_id) VALUES ($1, $2)`, srv.ID, other); err != nil {
		t.Fatal(err)
	}
	if err := members.AssignRole(ctx, srv.ID, other, top.ID); err != nil {
		t.Fatal(err)
	}

	before, _ := members.MemberStanding(ctx, srv.ID, member)
	otherSt, _ := members.MemberStanding(ctx, srv.ID, other)
	if before.Outranks(otherSt) || !otherSt.Outranks(before) {
		t.Fatalf("before reorder: member %v vs other %v", before.TopPosition, otherSt.TopPosition)
	}

	noCheck := func([]role.Role) error { return nil }
	if _, err := roles.Reorder(ctx, srv.ID, []uuid.UUID{low.ID, top.ID}, noCheck); err != nil {
		t.Fatalf("Reorder: %v", err)
	}

	after, _ := members.MemberStanding(ctx, srv.ID, member)
	otherSt, _ = members.MemberStanding(ctx, srv.ID, other)
	if after.TopPosition == nil || *after.TopPosition != 1 {
		t.Errorf("after reorder TopPosition = %v, want 1", after.TopPosition)
	}
	if !after.Outranks(otherSt) {
		t.Error("after moving their role to the top, the member should outrank the other member")
	}
}
