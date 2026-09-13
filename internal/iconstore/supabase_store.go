package iconstore

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/discordia-grupo01/servers/internal/model"
)

type SupabaseStore struct {
	BaseURL    string
	Bucket     string
	ServiceKey string
	MaxBytes   int64
	Client     *http.Client
}

func NewSupabaseStore(baseURL, bucket, serviceKey string, maxBytes int64) *SupabaseStore {
	return &SupabaseStore{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Bucket:     bucket,
		ServiceKey: serviceKey,
		MaxBytes:   maxBytes,
		Client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SupabaseStore) objectURL(id string) string {
	return fmt.Sprintf("%s/storage/v1/object/%s/%s", s.BaseURL, s.Bucket, id)
}

func (s *SupabaseStore) publicURL(id string) string {
	return fmt.Sprintf("%s/storage/v1/object/public/%s/%s", s.BaseURL, s.Bucket, id)
}

func (s *SupabaseStore) authHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+s.ServiceKey)
	req.Header.Set("apikey", s.ServiceKey)
}

func (s *SupabaseStore) Save(id string, data []byte) (string, error) {
	_, contentType, err := ValidateIcon(data, s.MaxBytes)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPut, s.objectURL(id), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("iconstore: cannot build upload request: %w", err)
	}
	s.authHeaders(req)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("iconstore: upload to supabase storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return "", fmt.Errorf("iconstore: supabase storage upload failed (%d): %s", resp.StatusCode, body)
	}

	return s.publicURL(id), nil
}

func (s *SupabaseStore) Delete(id string) error {
	req, err := http.NewRequest(http.MethodDelete, s.objectURL(id), nil)
	if err != nil {
		return fmt.Errorf("iconstore: cannot build delete request: %w", err)
	}
	s.authHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("iconstore: delete from supabase storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("iconstore: supabase storage delete failed (%d): %s", resp.StatusCode, body)
	}
	return nil
}

func (s *SupabaseStore) Get(id string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, s.objectURL(id), nil)
	if err != nil {
		return nil, "", fmt.Errorf("iconstore: cannot build download request: %w", err)
	}
	s.authHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("iconstore: download from supabase storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", model.ErrIconNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return nil, "", fmt.Errorf("iconstore: supabase storage download failed (%d): %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("iconstore: cannot read supabase storage response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

func (s *SupabaseStore) GenerateDefault() ([]byte, error) {
	return GenerateDefaultIcon()
}
