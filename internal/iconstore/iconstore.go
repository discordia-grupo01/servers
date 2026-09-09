// Package iconstore valida y persiste los iconos de los servidores en disco.
package iconstore

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

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
	Delete(id string) error
	FilePath(id string) (path string, ok bool)
}

type LocalStore struct {
	Dir      string
	MaxBytes int64
}

func NewLocalStore(dir string, maxBytes int64) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("iconstore: cannot create directory %s: %w", dir, err)
	}
	// Falla al arrancar si el directorio no es escribible (ej. volumen Docker
	// montado como root), en vez de devolver 500 en cada upload.
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return nil, fmt.Errorf("iconstore: %s is not writable: %w", dir, err)
	}
	_ = os.Remove(probe)
	return &LocalStore{Dir: dir, MaxBytes: maxBytes}, nil
}

func URLPath(id string) string { return "/v1/servers/" + id + "/icon" }

func (s *LocalStore) Save(id string, data []byte) (string, error) {
	ext, _, err := ValidateIcon(data, s.MaxBytes)
	if err != nil {
		return "", err
	}
	if err := s.Delete(id); err != nil {
		return "", err
	}
	dst := filepath.Join(s.Dir, id+ext)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("iconstore: cannot write %s: %w", dst, err)
	}
	return URLPath(id), nil
}

func (s *LocalStore) Delete(id string) error {
	for _, ext := range contentTypeExt {
		p := filepath.Join(s.Dir, id+ext)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("iconstore: cannot delete %s: %w", p, err)
		}
	}
	return nil
}

func (s *LocalStore) FilePath(id string) (string, bool) {
	for _, ext := range contentTypeExt {
		p := filepath.Join(s.Dir, id+ext)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
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
