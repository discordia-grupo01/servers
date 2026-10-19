package iconstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/discordia-grupo01/servers/internal/model"
)

var contentTypeExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

const SupportedFormats = "PNG, JPEG, WEBP"

func ValidateIcon(data []byte, maxBytes int64) (ext string, contentType string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("%w: file is empty", model.ErrIconUnsupportedType)
	}
	if int64(len(data)) > maxBytes {
		return "", "", fmt.Errorf(
			"%w (max %s, got %s)",
			model.ErrIconTooLarge, humanBytes(maxBytes), humanBytes(int64(len(data))),
		)
	}

	contentType = http.DetectContentType(data)
	e, ok := contentTypeExt[contentType]
	if !ok {
		return "", "", fmt.Errorf(
			"%w: detected %q, only %s are accepted",
			model.ErrIconUnsupportedType, contentType, SupportedFormats,
		)
	}
	return e, contentType, nil
}

type Store interface {
	Save(id string, data []byte) (url string, err error)
	Delete(key string) error
	Get(key string) (data []byte, contentType string, err error)
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func StorageKey(id, hash string) string {
	return id + "-" + hash
}

func withVersion(url, hash string) string {
	return fmt.Sprintf("%s?v=%s", url, hash)
}
func HashFromURL(url string) (hash string, ok bool) {
	_, v, found := strings.Cut(url, "?v=")
	if !found || v == "" {
		return "", false
	}
	return v, true
}

type LocalStore struct {
	Dir      string
	MaxBytes int64
}

func NewLocalStore(dir string, maxBytes int64) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("iconstore: cannot create directory %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return nil, fmt.Errorf("iconstore: %s is not writable: %w", dir, err)
	}
	_ = os.Remove(probe)
	return &LocalStore{Dir: dir, MaxBytes: maxBytes}, nil
}

const bannerIDSuffix = "-banner"

func URLPath(id string) string {
	if serverID, ok := strings.CutSuffix(id, bannerIDSuffix); ok {
		return "/v1/servers/" + serverID + "/banner"
	}
	return "/v1/servers/" + id + "/icon"
}

func (s *LocalStore) Save(id string, data []byte) (string, error) {
	ext, _, err := ValidateIcon(data, s.MaxBytes)
	if err != nil {
		return "", err
	}
	hash := contentHash(data)
	dst := filepath.Join(s.Dir, StorageKey(id, hash)+ext)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("iconstore: cannot write %s: %w", dst, err)
	}
	return withVersion(URLPath(id), hash), nil
}

func (s *LocalStore) Delete(key string) error {
	for _, ext := range contentTypeExt {
		p := filepath.Join(s.Dir, key+ext)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("iconstore: cannot delete %s: %w", p, err)
		}
	}
	return nil
}

func (s *LocalStore) Get(key string) ([]byte, string, error) {
	for contentType, ext := range contentTypeExt {
		p := filepath.Join(s.Dir, key+ext)
		data, err := os.ReadFile(p)
		if err == nil {
			return data, contentType, nil
		}
		if !os.IsNotExist(err) {
			return nil, "", fmt.Errorf("iconstore: cannot read %s: %w", p, err)
		}
	}
	return nil, "", model.ErrIconNotFound
}

func humanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
