package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func assertRejected(t *testing.T, name string, want error) {
	t.Helper()
	_, err, repo, icons := create(t, validParams(name))
	if !errors.Is(err, want) {
		t.Fatalf("name %q: err = %v, want %v", name, err, want)
	}
	if repo.calls != 0 {
		t.Errorf("name %q: repository was called", name)
	}
	if icons.savedID != "" {
		t.Errorf("name %q: icon store was called", name)
	}
}

func TestCreateServer_RejectsEmptyName(t *testing.T) {
	assertRejected(t, "", model.ErrNameRequired)
}

func TestCreateServer_RejectsWhitespaceOnlyName(t *testing.T) {
	assertRejected(t, "     ", model.ErrNameRequired)
}

func TestCreateServer_RejectsNameWithOneCharacter(t *testing.T) {
	assertRejected(t, "a", model.ErrNameTooShort)
}

func TestCreateServer_RejectsNameOneOverMaxLength(t *testing.T) {
	assertRejected(t, strings.Repeat("a", model.ServerNameMaxLen+1), model.ErrNameTooLong)
}

func TestCreateServer_RejectsVeryLongName(t *testing.T) {
	assertRejected(t, strings.Repeat("x", 5000), model.ErrNameTooLong)
}

func TestCreateServer_RejectsNameWithNullByte(t *testing.T) {
	assertRejected(t, "ab\x00cd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithNewline(t *testing.T) {
	assertRejected(t, "ab\ncd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithCarriageReturn(t *testing.T) {
	assertRejected(t, "ab\rcd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithTab(t *testing.T) {
	assertRejected(t, "ab\tcd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithVerticalTab(t *testing.T) {
	assertRejected(t, "ab\vcd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithFormFeed(t *testing.T) {
	assertRejected(t, "ab\fcd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithDelChar(t *testing.T) {
	assertRejected(t, "ab\x7fcd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameWithBellChar(t *testing.T) {
	assertRejected(t, "ab\acd", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsNameThatIsOnlyANullByte(t *testing.T) {
	assertRejected(t, "\x00", model.ErrNameInvalidChars)
}

func TestCreateServer_RejectsMissingOwner(t *testing.T) {
	_, err, repo, _ := create(t, service.CreateServerParams{Name: "Mi Comunidad", OwnerID: ""})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if repo.calls != 0 {
		t.Error("repository was called without an owner")
	}
}
