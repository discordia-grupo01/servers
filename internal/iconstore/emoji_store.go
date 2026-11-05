package iconstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/discordia-grupo01/servers/internal/model"
)

const (
	emojiCacheControl = "max-age=31536000"
	emojiPageSize     = 100
)

func emojiFolder(serverID string) string {
	return "emojis/" + serverID
}

func emojiObjectKey(serverID, key string) string {
	return emojiFolder(serverID) + "/" + key
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

// DeleteServerEmojis removes every image of the server, including the ones of
// emojis that were already deleted.
func (s *SupabaseStore) DeleteServerEmojis(serverID string) error {
	folder := emojiFolder(serverID)
	names, err := s.listObjects(folder)
	if err != nil {
		return err
	}
	for start := 0; start < len(names); start += emojiPageSize {
		end := min(start+emojiPageSize, len(names))
		keys := make([]string, 0, end-start)
		for _, name := range names[start:end] {
			keys = append(keys, folder+"/"+name)
		}
		if err := s.deleteObjects(keys); err != nil {
			return err
		}
	}
	return nil
}

func (s *SupabaseStore) listObjects(folder string) ([]string, error) {
	var names []string
	for offset := 0; ; offset += emojiPageSize {
		page, err := s.listPage(folder, offset)
		if err != nil {
			return nil, err
		}
		for _, object := range page {
			if object.ID != nil {
				names = append(names, object.Name)
			}
		}
		if len(page) < emojiPageSize {
			return names, nil
		}
	}
}

type storageObject struct {
	ID   *string `json:"id"`
	Name string  `json:"name"`
}

func (s *SupabaseStore) listPage(folder string, offset int) ([]storageObject, error) {
	var page []storageObject
	err := s.storageJSON(http.MethodPost, fmt.Sprintf("%s/storage/v1/object/list/%s", s.BaseURL, s.Bucket), map[string]any{
		"prefix": folder,
		"limit":  emojiPageSize,
		"offset": offset,
		"sortBy": map[string]string{"column": "name", "order": "asc"},
	}, &page)
	return page, err
}

func (s *SupabaseStore) deleteObjects(keys []string) error {
	return s.storageJSON(http.MethodDelete, fmt.Sprintf("%s/storage/v1/object/%s", s.BaseURL, s.Bucket), map[string]any{
		"prefixes": keys,
	}, nil)
}

func (s *SupabaseStore) storageJSON(method, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("iconstore: cannot encode storage request: %w", err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("iconstore: cannot build storage request: %w", err)
	}
	s.authHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("iconstore: supabase storage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("iconstore: supabase storage %s failed (%d): %s", method, resp.StatusCode, detail)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("iconstore: cannot read supabase storage response: %w", err)
	}
	return nil
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

func (s *LocalStore) DeleteServerEmojis(serverID string) error {
	if serverID == "" || serverID == ".." || serverID != filepath.Base(serverID) {
		return fmt.Errorf("iconstore: invalid server id %q", serverID)
	}
	dir := filepath.Join(s.Dir, "emojis", serverID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("iconstore: cannot delete %s: %w", dir, err)
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
