package iconstore

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/discordia-grupo01/servers/internal/model"
)

const emojiCacheControl = "max-age=31536000"

func emojiObjectKey(serverID, key string) string {
	return "emojis/" + serverID + "/" + key
}

func (s *SupabaseStore) SaveEmoji(serverID, key string, data []byte, contentType string) (string, error) {
	objectKey := emojiObjectKey(serverID, key)
	req, err := http.NewRequest(http.MethodPut, s.objectURL(objectKey), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("iconstore: cannot build emoji upload request: %w", err)
	}
	s.authHeaders(req)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Cache-Control", emojiCacheControl)
	req.Header.Set("x-upsert", "false")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("iconstore: upload emoji to supabase storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return "", fmt.Errorf("iconstore: supabase storage emoji upload failed (%d): %s", resp.StatusCode, body)
	}
	return s.publicURL(objectKey), nil
}

func (s *SupabaseStore) DeleteEmoji(serverID, key string) error {
	return s.Delete(emojiObjectKey(serverID, key))
}

func (s *LocalStore) emojiPath(serverID, key string) (string, error) {
	if serverID != filepath.Base(serverID) || key != filepath.Base(key) || key == ".." || serverID == ".." {
		return "", model.ErrEmojiNotFound
	}
	return filepath.Join(s.Dir, "emojis", serverID, key), nil
}

func (s *LocalStore) SaveEmoji(serverID, key string, data []byte, _ string) (string, error) {
	path, err := s.emojiPath(serverID, key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("iconstore: cannot create emoji directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("iconstore: cannot write %s: %w", path, err)
	}
	return "/v1/servers/" + serverID + "/emojis/" + key + "/image", nil
}

func (s *LocalStore) DeleteEmoji(serverID, key string) error {
	path, err := s.emojiPath(serverID, key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("iconstore: cannot delete %s: %w", path, err)
	}
	return nil
}

func (s *LocalStore) GetEmoji(serverID, key string) ([]byte, string, error) {
	path, err := s.emojiPath(serverID, key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, "", model.ErrEmojiNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("iconstore: cannot read %s: %w", path, err)
	}
	return data, http.DetectContentType(data), nil
}
