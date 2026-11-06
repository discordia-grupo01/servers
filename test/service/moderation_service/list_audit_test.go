package moderation_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

func (f *fixture) listAudit(actor string, limit, offset int) (moderation.AuditPage, error) {
	return f.svc.ListAudit(context.Background(), moderation_service.ListAuditParams{
		ServerID: f.server.ID, ActorUserID: actor, Limit: limit, Offset: offset,
	})
}

func TestListAudit_ModeratorGetsThePage(t *testing.T) {
	f := newFixture()
	f.standings.byUser[moderatorID] = member(rank(2), role.ModeratorPermissions)
	f.repo.listAudit = []moderation.AuditEntry{{Action: moderation.AuditActionBan, ActorUserID: moderatorID, TargetUserID: memberID}}
	f.repo.auditTotal = 3

	page, err := f.listAudit(moderatorID, 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Entries) != 1 || page.Total != 3 || page.Limit != 2 || page.Offset != 1 {
		t.Errorf("page = %+v", page)
	}
	if f.repo.gotLimit != 2 || f.repo.gotOffset != 1 {
		t.Errorf("repo got limit=%d offset=%d", f.repo.gotLimit, f.repo.gotOffset)
	}
}

func TestListAudit_RequiresAnyModerationPermission(t *testing.T) {
	for _, actor := range []string{ownerID, moderatorID} {
		f := newFixture()
		f.standings.byUser[moderatorID] = member(rank(2), role.ModeratorPermissions)
		if _, err := f.listAudit(actor, 0, 0); err != nil {
			t.Errorf("%s: unexpected error: %v", actor, err)
		}
	}
	for _, actor := range []string{memberID, "stranger"} {
		f := newFixture()
		if _, err := f.listAudit(actor, 0, 0); !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", actor, err)
		}
	}
}

func TestListAudit_ClampsPagination(t *testing.T) {
	f := newFixture()
	page, err := f.listAudit(ownerID, 1000, -4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Limit != moderation.ListMaxLimit || page.Offset != 0 ||
		f.repo.gotLimit != moderation.ListMaxLimit || f.repo.gotOffset != 0 {
		t.Errorf("page=%+v repo=%d/%d", page, f.repo.gotLimit, f.repo.gotOffset)
	}
}

func TestListAudit_RepoErrorPropagates(t *testing.T) {
	f := newFixture()
	boom := errors.New("db down")
	f.repo.auditErr = boom
	if _, err := f.listAudit(ownerID, 0, 0); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
