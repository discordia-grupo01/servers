package emoji_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service/emoji_service"
)

const (
	ownerID    = "owner-1"
	managerID  = "manager-1"
	memberID   = "member-1"
	outsiderID = "outsider-1"
)

var (
	gifBytes = []byte("GIF89a0000")
	pngBytes = []byte("\x89PNG\r\n\x1a\n0000")
	errBoom  = errors.New("boom")
)

type fakeServers struct {
	server model.Server
	err    error
}

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, f.err
}

type fakeStandings struct{}

func (fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	switch userID {
	case managerID:
		return role.Standing{IsMember: true, Permissions: role.PermManageServer}, nil
	case memberID:
		return role.Standing{IsMember: true}, nil
	}
	return role.Standing{}, nil
}

type fakeRepo struct {
	created   []emoji.Emoji
	maxEmojis int
	event     outbox.Event
	createErr error
	listed    []emoji.Emoji
	listErr   error
	toDelete  emoji.Emoji
	deleteErr error
	deletedBy string
}

func (f *fakeRepo) Create(_ context.Context, e emoji.Emoji, maxEmojis int, evt outbox.Event) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, e)
	f.maxEmojis, f.event = maxEmojis, evt
	return nil
}

func (f *fakeRepo) List(context.Context, uuid.UUID) ([]emoji.Emoji, error) {
	return f.listed, f.listErr
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID, by string, _ time.Time, build func(emoji.Emoji) (outbox.Event, error)) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedBy = by
	evt, err := build(f.toDelete)
	f.event = evt
	return err
}

type fakeImages struct {
	key, contentType string
	saveErr          error
	deletedKeys      []string
}

func (f *fakeImages) SaveEmoji(_, key string, _ []byte, contentType string) (string, error) {
	f.key, f.contentType = key, contentType
	return "https://cdn.test/" + key, f.saveErr
}

func (f *fakeImages) DeleteEmoji(_, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

type fixture struct {
	svc      *emoji_service.Service
	repo     *fakeRepo
	images   *fakeImages
	servers  *fakeServers
	serverID uuid.UUID
}

func newFixture() *fixture {
	id := uuid.New()
	repo, images := &fakeRepo{}, &fakeImages{}
	servers := &fakeServers{server: model.Server{ID: id, OwnerID: ownerID}}
	return &fixture{svc: emoji_service.New(servers, fakeStandings{}, repo, images), repo: repo, images: images, servers: servers, serverID: id}
}

func TestCreatePublishesTheWholeEmoji(t *testing.T) {
	f := newFixture()
	got, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
		ServerID: f.serverID, ActorUserID: managerID, Name: "party", Image: gifBytes,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !got.Animated || got.CreatedBy != managerID || got.ImageURL != "https://cdn.test/"+got.ID.String()+".gif" {
		t.Errorf("got %+v", got)
	}
	if f.images.contentType != "image/gif" || f.repo.maxEmojis != emoji.BaseSlots {
		t.Errorf("contentType=%q max=%d", f.images.contentType, f.repo.maxEmojis)
	}
	if f.repo.event.RoutingKey != servers.RoutingKeyEmojiCreated {
		t.Errorf("routing key = %q", f.repo.event.RoutingKey)
	}
	var evt servers.EmojiCreated
	if err := json.Unmarshal(f.repo.event.Payload, &evt); err != nil {
		t.Fatal(err)
	}
	if evt.EventID == "" || evt.EmojiID != got.ID.String() || evt.ServerID != f.serverID.String() ||
		evt.Name != "party" || !evt.Animated || evt.ImageURL != got.ImageURL {
		t.Errorf("event = %+v", evt)
	}
}

func TestCreatePNGIsNotAnimated(t *testing.T) {
	f := newFixture()
	got, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
		ServerID: f.serverID, ActorUserID: ownerID, Name: "still", Image: pngBytes,
	})
	if err != nil || got.Animated {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestCreateRejectsBeforeUploading(t *testing.T) {
	cases := map[string]struct {
		actor, name string
		image       []byte
		want        error
	}{
		"anonymous":  {"", "party", gifBytes, model.ErrUnauthorized},
		"outsider":   {outsiderID, "party", gifBytes, model.ErrForbiddenNotAdmin},
		"member":     {memberID, "party", gifBytes, model.ErrForbiddenNotAdmin},
		"bad name":   {managerID, "x", gifBytes, model.ErrEmojiNameInvalid},
		"no image":   {managerID, "party", nil, model.ErrEmojiImageRequired},
		"bad format": {managerID, "party", []byte("not an image"), model.ErrEmojiImageUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
				ServerID: f.serverID, ActorUserID: tc.actor, Name: tc.name, Image: tc.image,
			})
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if f.images.key != "" {
				t.Error("uploaded an image for a rejected request")
			}
		})
	}
}

func TestCreateRemovesTheImageWhenTheRowFails(t *testing.T) {
	for _, repoErr := range []error{model.ErrEmojiNameTaken, model.ErrEmojiLimitReached, errBoom} {
		f := newFixture()
		f.repo.createErr = repoErr
		_, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
			ServerID: f.serverID, ActorUserID: managerID, Name: "party", Image: gifBytes,
		})
		if !errors.Is(err, repoErr) {
			t.Errorf("error = %v, want %v", err, repoErr)
		}
		if len(f.images.deletedKeys) != 1 || f.images.deletedKeys[0] != f.images.key {
			t.Errorf("deleted %v, want [%s]", f.images.deletedKeys, f.images.key)
		}
	}
}

func TestCreateUploadFailureWritesNoRow(t *testing.T) {
	f := newFixture()
	f.images.saveErr = errBoom
	_, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
		ServerID: f.serverID, ActorUserID: managerID, Name: "party", Image: gifBytes,
	})
	if !errors.Is(err, errBoom) {
		t.Errorf("error = %v", err)
	}
	if len(f.repo.created) != 0 {
		t.Error("wrote a row after a failed upload")
	}
}

func TestListIsForMembers(t *testing.T) {
	f := newFixture()
	f.repo.listed = []emoji.Emoji{{Name: "party"}}

	page, err := f.svc.List(context.Background(), emoji_service.ListParams{ServerID: f.serverID, ActorUserID: memberID})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Emojis) != 1 || page.MaxEmojis != emoji.BaseSlots {
		t.Errorf("page = %+v", page)
	}

	_, err = f.svc.List(context.Background(), emoji_service.ListParams{ServerID: f.serverID, ActorUserID: outsiderID})
	if !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("outsider error = %v", err)
	}
	_, err = f.svc.List(context.Background(), emoji_service.ListParams{ServerID: f.serverID})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("anonymous error = %v", err)
	}
}

func TestDeletePublishesTheWholeEmoji(t *testing.T) {
	f := newFixture()
	id := uuid.New()
	f.repo.toDelete = emoji.Emoji{ID: id, ServerID: f.serverID, Name: "party", Animated: true, ImageURL: "https://cdn.test/a.gif"}

	err := f.svc.Delete(context.Background(), emoji_service.DeleteParams{ServerID: f.serverID, EmojiID: id, ActorUserID: managerID})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if f.repo.deletedBy != managerID || f.repo.event.RoutingKey != servers.RoutingKeyEmojiDeleted {
		t.Errorf("by=%q key=%q", f.repo.deletedBy, f.repo.event.RoutingKey)
	}
	var evt servers.EmojiDeleted
	if err := json.Unmarshal(f.repo.event.Payload, &evt); err != nil {
		t.Fatal(err)
	}
	if evt.EmojiID != id.String() || evt.Name != "party" || !evt.Animated || evt.ImageURL != "https://cdn.test/a.gif" {
		t.Errorf("event = %+v", evt)
	}
	if len(f.images.deletedKeys) != 0 {
		t.Error("deleting an emoji must keep its image")
	}
}

func TestDeleteErrors(t *testing.T) {
	f := newFixture()
	err := f.svc.Delete(context.Background(), emoji_service.DeleteParams{ServerID: f.serverID, EmojiID: uuid.New(), ActorUserID: memberID})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("member error = %v", err)
	}
	f.repo.deleteErr = model.ErrEmojiNotFound
	err = f.svc.Delete(context.Background(), emoji_service.DeleteParams{ServerID: f.serverID, EmojiID: uuid.New(), ActorUserID: ownerID})
	if !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("not found error = %v", err)
	}
}

func TestServerNotFound(t *testing.T) {
	repo, images := &fakeRepo{}, &fakeImages{}
	svc := emoji_service.New(&fakeServers{err: model.ErrServerNotFound}, fakeStandings{}, repo, images)
	if _, err := svc.Create(context.Background(), emoji_service.CreateParams{ServerID: uuid.New(), ActorUserID: ownerID, Name: "party", Image: gifBytes}); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("Create error = %v", err)
	}
	if _, err := svc.List(context.Background(), emoji_service.ListParams{ServerID: uuid.New(), ActorUserID: ownerID}); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("List error = %v", err)
	}
}

func TestList_MaxEmojisFollowsTheBoostLevel(t *testing.T) {
	f := newFixture()
	f.servers.server.BoostLevel = 1
	page, err := f.svc.List(context.Background(), emoji_service.ListParams{ServerID: f.serverID, ActorUserID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if page.MaxEmojis != 70 {
		t.Errorf("max_emojis = %d, want 70", page.MaxEmojis)
	}
}

func TestCreate_UsesTheSlotsOfTheBoostLevel(t *testing.T) {
	f := newFixture()
	f.servers.server.BoostLevel = 1
	if _, err := f.svc.Create(context.Background(), emoji_service.CreateParams{
		ServerID: f.serverID, ActorUserID: managerID, Name: "party", Image: gifBytes,
	}); err != nil {
		t.Fatal(err)
	}
	if f.repo.maxEmojis != 70 {
		t.Errorf("maxEmojis = %d, want 70", f.repo.maxEmojis)
	}
}
