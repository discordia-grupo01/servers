package iconstore_test

import (
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/model"
)

func png(n int) []byte  { return pad([]byte("\x89PNG\r\n\x1a\n"), n) }
func jpeg(n int) []byte { return pad([]byte("\xff\xd8\xff\xe0"), n) }
func webp(n int) []byte { return pad([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), n) }
func gif(n int) []byte  { return pad([]byte("GIF87a"), n) }
func bmp(n int) []byte  { return pad([]byte("BM\x00\x00"), n) }

func pad(head []byte, total int) []byte {
	if total <= len(head) {
		return head
	}
	return append(head, make([]byte, total-len(head))...)
}

const max = 4096

func TestValidateIcon_AcceptsPNG(t *testing.T) {
	ext, _, err := iconstore.ValidateIcon(png(64), max)
	if err != nil || ext != ".png" {
		t.Fatalf("ext=%q err=%v", ext, err)
	}
}

func TestValidateIcon_AcceptsJPEG(t *testing.T) {
	ext, _, err := iconstore.ValidateIcon(jpeg(64), max)
	if err != nil || ext != ".jpg" {
		t.Fatalf("ext=%q err=%v", ext, err)
	}
}

func TestValidateIcon_AcceptsWEBP(t *testing.T) {
	ext, _, err := iconstore.ValidateIcon(webp(64), max)
	if err != nil || ext != ".webp" {
		t.Fatalf("ext=%q err=%v", ext, err)
	}
}

func TestValidateIcon_RejectsEmptyData(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(nil, max); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateIcon_AcceptsGIF(t *testing.T) {
	ext, contentType, err := iconstore.ValidateIcon(gif(64), max)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ext != ".gif" || contentType != "image/gif" {
		t.Errorf("ext=%q contentType=%q", ext, contentType)
	}
	if !iconstore.IsGIF(gif(64)) {
		t.Error("IsGIF = false for a GIF")
	}
}

func TestValidateIcon_RejectsOtherTypes(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(pad([]byte("BM"), 64), max); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateIcon_RejectsBMP(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(bmp(64), max); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateIcon_RejectsPlainText(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon([]byte("just some text, not an image at all"), max); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateIcon_AcceptsExactlyMaxBytes(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(png(max), max); err != nil {
		t.Fatalf("data of exactly max bytes rejected: %v", err)
	}
}

func TestValidateIcon_RejectsOneByteOverMax(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(png(max+1), max); !errors.Is(err, model.ErrIconTooLarge) {
		t.Fatalf("err = %v, want ErrIconTooLarge", err)
	}
}

func TestValidateIcon_SizeIsCheckedBeforeType(t *testing.T) {
	if _, _, err := iconstore.ValidateIcon(gif(max+1), max); !errors.Is(err, model.ErrIconTooLarge) {
		t.Fatalf("err = %v, want ErrIconTooLarge", err)
	}
}
