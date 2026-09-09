package service_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestCreateServer_MakesCallerTheOwner(t *testing.T) {
	srv, err, _, _ := create(t, validParams("Mi Comunidad"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.OwnerID != testOwner {
		t.Errorf("owner_id = %q, want %q", srv.OwnerID, testOwner)
	}
}

func TestCreateServer_GeneratesID(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	if srv.ID == (model.Server{}).ID {
		t.Error("ID was not generated")
	}
}

func TestCreateServer_SetsCreatedAt(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	if srv.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
}

func TestCreateServer_CreatesADefaultTextChannel(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	ch := findChannel(t, srv, model.ChannelText)
	if ch.Name != model.DefaultTextChannelName {
		t.Errorf("text channel name = %q, want %q", ch.Name, model.DefaultTextChannelName)
	}
	if ch.Position != 0 {
		t.Errorf("text channel position = %d, want 0", ch.Position)
	}
}

func TestCreateServer_CreatesADefaultVoiceChannel(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	ch := findChannel(t, srv, model.ChannelVoice)
	if ch.Name != model.DefaultVoiceChannelName {
		t.Errorf("voice channel name = %q, want %q", ch.Name, model.DefaultVoiceChannelName)
	}
}

func TestCreateServer_CreatesExactlyTwoChannels(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	if len(srv.Channels) != 2 {
		t.Fatalf("got %d channels, want 2: %+v", len(srv.Channels), srv.Channels)
	}
}

func TestCreateServer_ChannelsBelongToTheNewServer(t *testing.T) {
	srv, _, _, _ := create(t, validParams("Mi Comunidad"))
	for _, ch := range srv.Channels {
		if ch.ServerID != srv.ID {
			t.Errorf("channel %q has ServerID %s, want %s", ch.Name, ch.ServerID, srv.ID)
		}
	}
}

func TestCreateServer_PersistsThroughTheRepositoryOnce(t *testing.T) {
	_, _, repo, _ := create(t, validParams("Mi Comunidad"))
	if repo.calls != 1 {
		t.Fatalf("repo.CreateServer called %d times, want 1", repo.calls)
	}
	if repo.gotServer.Name != "Mi Comunidad" || len(repo.gotChans) != 2 {
		t.Errorf("repo got server=%q chans=%d", repo.gotServer.Name, len(repo.gotChans))
	}
}

func TestCreateServer_TrimsSurroundingWhitespace(t *testing.T) {
	srv, _, _, _ := create(t, validParams("   Mi Comunidad   "))
	if srv.Name != "Mi Comunidad" {
		t.Errorf("name = %q, want %q", srv.Name, "Mi Comunidad")
	}
}

func TestCreateServer_KeepsInternalPunctuationAndAccents(t *testing.T) {
	const in = "Café & Té — 2024"
	srv, err, _, _ := create(t, validParams(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.Name != in {
		t.Errorf("name = %q, want it stored verbatim %q", srv.Name, in)
	}
}

func TestCreateServer_AcceptsNameAtMinLength(t *testing.T) {
	_, err, _, _ := create(t, validParams(strings.Repeat("a", model.ServerNameMinLen)))
	if err != nil {
		t.Fatalf("min-length name rejected: %v", err)
	}
}

func TestCreateServer_AcceptsNameAtMaxLengthCountingRunesNotBytes(t *testing.T) {
	in := strings.Repeat("é", model.ServerNameMaxLen)
	if utf8.RuneCountInString(in) != 100 || len(in) != 200 {
		t.Fatalf("fixture: %d runes / %d bytes", utf8.RuneCountInString(in), len(in))
	}
	_, err, _, _ := create(t, validParams(in))
	if err != nil {
		t.Fatalf("max-length name rejected: %v", err)
	}
}

func TestCreateServer_AcceptsNonLatinName(t *testing.T) {
	_, err, _, _ := create(t, validParams("サーバー"))
	if err != nil {
		t.Fatalf("non-latin name rejected: %v", err)
	}
}

func TestCreateServer_AcceptsNameWithEmoji(t *testing.T) {
	_, err, _, _ := create(t, validParams("🎮 Gamers"))
	if err != nil {
		t.Fatalf("emoji name rejected: %v", err)
	}
}

func TestCreateServer_WithoutIcon_LeavesIconURLNil(t *testing.T) {
	srv, _, _, icons := create(t, validParams("Sin Icono"))
	if srv.IconURL != nil {
		t.Errorf("icon_url = %v, want nil", *srv.IconURL)
	}
	if icons.savedID != "" {
		t.Error("icon store should not have been called")
	}
}

func TestCreateServer_WithIcon_StoresBytesUnderServerID(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\n....")
	srv, err, _, icons := create(t, service.CreateServerParams{
		Name: "Con Icono", OwnerID: testOwner, Icon: data,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.savedID != srv.ID.String() {
		t.Errorf("icon saved under %q, want %q", icons.savedID, srv.ID.String())
	}
	if string(icons.savedData) != string(data) {
		t.Error("icon bytes passed to the store differ from the input")
	}
}

func TestCreateServer_WithIcon_SetsIconURL(t *testing.T) {
	srv, err, _, _ := create(t, iconParams("Con Icono"))
	if err != nil {
		t.Fatal(err)
	}
	if srv.IconURL == nil || *srv.IconURL != "/v1/servers/"+srv.ID.String()+"/icon" {
		t.Errorf("icon_url = %v", srv.IconURL)
	}
}

func findChannel(t *testing.T, s model.Server, kind model.ChannelKind) model.Channel {
	t.Helper()
	for _, ch := range s.Channels {
		if ch.Kind == kind {
			return ch
		}
	}
	t.Fatalf("no %s channel in %+v", kind, s.Channels)
	return model.Channel{}
}
