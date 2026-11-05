package boost_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/boost_service"
)

const (
	owner    = "owner-1"
	someone  = "someone"
	stranger = "stranger"
)

type fakes struct {
	server model.Server
	getErr error

	standing role.Standing

	vanityCalls int
	vanityCode  *string
	vanityErr   error
}

func (f *fakes) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, f.getErr
}

func (f *fakes) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	if userID == stranger {
		return role.Standing{}, nil
	}
	return f.standing, nil
}

func (f *fakes) SetVanity(_ context.Context, _ uuid.UUID, code *string) error {
	f.vanityCalls++
	f.vanityCode = code
	return f.vanityErr
}

func newService(server model.Server) (*boost_service.Service, *fakes) {
	server.OwnerID = owner
	f := &fakes{server: server, standing: role.Standing{IsMember: true}}
	return boost_service.New(f, f, f), f
}

func TestSummary_AnyMemberCanSeeIt(t *testing.T) {
	svc, _ := newService(model.Server{BoostCount: 7, BoostLevel: 1})
	got, err := svc.Summary(context.Background(), uuid.New(), someone)
	if err != nil {
		t.Fatal(err)
	}
	if got.BoostCount != 7 || got.Level != 1 || got.Next == nil || got.Next.BoostsMissing != 8 {
		t.Errorf("summary = %+v", got)
	}
}

func TestSummary_Errors(t *testing.T) {
	svc, f := newService(model.Server{})
	if _, err := svc.Summary(context.Background(), uuid.New(), ""); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no user: err = %v", err)
	}
	if _, err := svc.Summary(context.Background(), uuid.New(), stranger); !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("stranger: err = %v", err)
	}
	f.getErr = model.ErrServerNotFound
	if _, err := svc.Summary(context.Background(), uuid.New(), someone); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("missing server: err = %v", err)
	}
}

func TestSetVanity_NormalizesAndSaves(t *testing.T) {
	svc, f := newService(model.Server{BoostLevel: model.BoostLevelVanity})
	code := "  Mi-Servidor "
	got, err := svc.SetVanity(context.Background(), uuid.New(), owner, &code)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "mi-servidor" || f.vanityCode == nil || *f.vanityCode != "mi-servidor" {
		t.Errorf("got %v, saved %v", got, f.vanityCode)
	}
}

func TestSetVanity_NeedsLevelToSetButNotToClear(t *testing.T) {
	svc, f := newService(model.Server{BoostLevel: model.BoostLevelVanity - 1})
	code := "mi-servidor"

	_, err := svc.SetVanity(context.Background(), uuid.New(), owner, &code)
	var levelErr *model.BoostLevelRequiredError
	if !errors.As(err, &levelErr) || levelErr.Required != model.BoostLevelVanity {
		t.Fatalf("err = %v", err)
	}
	if f.vanityCalls != 0 {
		t.Error("saved without the level")
	}

	if got, err := svc.SetVanity(context.Background(), uuid.New(), owner, nil); err != nil || got != nil {
		t.Fatalf("clear: got=%v err=%v", got, err)
	}
	if f.vanityCalls != 1 || f.vanityCode != nil {
		t.Errorf("calls=%d code=%v", f.vanityCalls, f.vanityCode)
	}
}

func TestSetVanity_Rejections(t *testing.T) {
	svc, f := newService(model.Server{BoostLevel: model.BoostLevelVanity})
	code, invalid := "mi-servidor", "no"

	if _, err := svc.SetVanity(context.Background(), uuid.New(), "", &code); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no user: err = %v", err)
	}
	if _, err := svc.SetVanity(context.Background(), uuid.New(), someone, &code); !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("no permission: err = %v", err)
	}
	if _, err := svc.SetVanity(context.Background(), uuid.New(), owner, &invalid); !errors.Is(err, model.ErrVanityCodeInvalid) {
		t.Errorf("invalid: err = %v", err)
	}
	f.vanityErr = model.ErrVanityCodeTaken
	if _, err := svc.SetVanity(context.Background(), uuid.New(), owner, &code); !errors.Is(err, model.ErrVanityCodeTaken) {
		t.Errorf("taken: err = %v", err)
	}
}
