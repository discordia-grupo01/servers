package moderation_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

func (f *fixture) list(actor string, limit, offset int) (moderation.BanPage, error) {
	return f.svc.ListBans(context.Background(), moderation_service.ListBansParams{
		ServerID: f.server.ID, ActorUserID: actor, Limit: limit, Offset: offset,
	})
}

func TestListBans_ModeratorGetsThePage(t *testing.T) {
	f := newFixture()
	f.repo.listBans = []moderation.Ban{{UserID: "a"}, {UserID: "b"}}
	f.repo.listTotal = 7

	page, err := f.list(moderatorID, 2, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Bans) != 2 || page.Total != 7 || page.Limit != 2 || page.Offset != 4 {
		t.Errorf("page = %+v", page)
	}
	if f.repo.gotLimit != 2 || f.repo.gotOffset != 4 {
		t.Errorf("repo got limit=%d offset=%d", f.repo.gotLimit, f.repo.gotOffset)
	}
}

func TestListBans_ClampsPagination(t *testing.T) {
	cases := []struct {
		limit, offset         int
		wantLimit, wantOffset int
	}{
		{0, 0, moderation.ListDefaultLimit, 0},
		{-5, -3, moderation.ListDefaultLimit, 0},
		{1000, 10, moderation.ListMaxLimit, 10},
	}
	for _, tc := range cases {
		f := newFixture()
		page, err := f.list(ownerID, tc.limit, tc.offset)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if page.Limit != tc.wantLimit || page.Offset != tc.wantOffset || f.repo.gotLimit != tc.wantLimit {
			t.Errorf("limit=%d offset=%d → page %d/%d, repo %d; want %d/%d",
				tc.limit, tc.offset, page.Limit, page.Offset, f.repo.gotLimit, tc.wantLimit, tc.wantOffset)
		}
	}
}

func TestListBans_WithoutPermission_Forbidden(t *testing.T) {
	for _, actor := range []string{memberID, "stranger"} {
		f := newFixture()
		if _, err := f.list(actor, 0, 0); !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", actor, err)
		}
	}
}

func TestListBans_RepoErrorPropagates(t *testing.T) {
	f := newFixture()
	boom := errors.New("db down")
	f.repo.listErr = boom
	if _, err := f.list(ownerID, 0, 0); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
