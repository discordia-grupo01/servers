package member_repo_test

import (
	"context"
	"testing"
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
