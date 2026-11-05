package emoji

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

const (
	BaseSlots           = 50
	BoostSlots          = 20
	MaxImageBytes int64 = 5 << 20
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,32}$`)

var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// Slots is how many emojis a server of that boost level can have. Dropping a
// level keeps the emojis it already has: it just can't add new ones.
func Slots(boostLevel int) int {
	if boostLevel >= model.BoostLevelEmojiSlots {
		return BaseSlots + BoostSlots
	}
	return BaseSlots
}

type Emoji struct {
	ID        uuid.UUID
	ServerID  uuid.UUID
	Name      string
	Animated  bool
	ImageURL  string
	CreatedBy string
	CreatedAt time.Time
	DeletedAt *time.Time
	DeletedBy *string
}

type Image struct {
	Extension   string
	ContentType string
	Animated    bool
}

func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return model.ErrEmojiNameInvalid
	}
	return nil
}

// InspectImage trusts the file's magic bytes, never its name or declared type.
func InspectImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, model.ErrEmojiImageRequired
	}
	if int64(len(data)) > MaxImageBytes {
		return Image{}, model.ErrEmojiImageTooLarge
	}
	contentType := http.DetectContentType(data)
	extension, ok := imageExtensions[contentType]
	if !ok {
		return Image{}, fmt.Errorf("%w: detected %q", model.ErrEmojiImageUnsupported, contentType)
	}
	return Image{Extension: extension, ContentType: contentType, Animated: contentType == "image/gif"}, nil
}

type Page struct {
	Emojis    []Emoji
	MaxEmojis int
}

type Response struct {
	ID        string    `json:"id" example:"2f4e6a8c-1b3d-4f5a-9c7e-8d6b4a2c0e1f"`
	ServerID  string    `json:"server_id" example:"0b6f1c8e-7a2d-4e3f-9c1b-5d4a3e2f1a0b"`
	Name      string    `json:"name" example:"party"`
	Animated  bool      `json:"animated" example:"true"`
	ImageURL  string    `json:"image_url" example:"https://storage.example.com/emojis/2f4e6a8c.gif"`
	CreatedBy string    `json:"created_by" example:"a3f1c0de-1111-4222-8333-444455556666"`
	CreatedAt time.Time `json:"created_at"`
}

type ListResponse struct {
	Emojis    []Response `json:"emojis"`
	MaxEmojis int        `json:"max_emojis" example:"50"`
}

func ToResponse(e Emoji) Response {
	return Response{
		ID:        e.ID.String(),
		ServerID:  e.ServerID.String(),
		Name:      e.Name,
		Animated:  e.Animated,
		ImageURL:  e.ImageURL,
		CreatedBy: e.CreatedBy,
		CreatedAt: e.CreatedAt,
	}
}

func ToListResponse(p Page) ListResponse {
	emojis := make([]Response, 0, len(p.Emojis))
	for _, e := range p.Emojis {
		emojis = append(emojis, ToResponse(e))
	}
	return ListResponse{Emojis: emojis, MaxEmojis: p.MaxEmojis}
}
