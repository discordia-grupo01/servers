// Package userprofile holds the local, read-only replica of a user's public
// profile, kept up to date from identify's users.profile_updated events.
package userprofile

// Profile is a user's public profile as identify last published it. Version
// is identify's profile_version: a higher one always supersedes a lower one,
// whatever order the events arrive in.
type Profile struct {
	UserID      string
	Name        string
	AvatarURL   string
	Description string
	StatusText  string
	StatusEmoji string
	Version     int64
}
