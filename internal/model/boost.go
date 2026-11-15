package model

import (
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

const (
	BoostStatusActive    = "active"
	BoostStatusRejected  = "rejected"
	BoostStatusCancelled = "cancelled"
	BoostStatusRemoved   = "removed"
)

// The level each perk needs. Perks are cumulative.
const (
	BoostLevelEmojiSlots   = 1
	BoostLevelAnimatedIcon = 1
	BoostLevelBanner       = 2
	BoostLevelVanity       = 3
)

const (
	PerkExtraEmojiSlots   = "extra_emoji_slots"
	PerkAnimatedIcon      = "animated_icon"
	PerkCustomBanner      = "custom_banner"
	PerkLargerAttachments = "larger_attachments"
	PerkVanityURL         = "vanity_url"
	PerkBoostedBadge      = "boosted_badge"
)

// MaxBoostCount is the boosts of the last level. A server with that many active
// boosts takes no more until someone removes one.
const MaxBoostCount = 30

var boostLevelThresholds = []int{5, 15, MaxBoostCount}

var vanityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// Boost identifies one boost of the saga with billing.
type Boost struct {
	ID       uuid.UUID
	ServerID uuid.UUID
	UserID   string
}

// BoostSummary is what a member sees about a server's boosts.
type BoostSummary struct {
	BoostCount int
	Level      int
	Perks      []string
	Next       *NextBoostLevel
}

// NextBoostLevel is nil on the last level.
type NextBoostLevel struct {
	Level          int
	BoostsRequired int
	BoostsMissing  int
}

func BoostLevelFor(count int) int {
	level := 0
	for _, threshold := range boostLevelThresholds {
		if count >= threshold {
			level++
		}
	}
	return level
}

// BoostCapReached reports whether a server already has the boosts of the last
// level, so one more would add nothing.
func BoostCapReached(count int) bool {
	return count >= MaxBoostCount
}

func BoostPerks(level int) []string {
	perks := make([]string, 0, 6)
	if level >= 1 {
		perks = append(perks, PerkExtraEmojiSlots, PerkAnimatedIcon)
	}
	if level >= 2 {
		perks = append(perks, PerkCustomBanner, PerkLargerAttachments)
	}
	if level >= 3 {
		perks = append(perks, PerkVanityURL, PerkBoostedBadge)
	}
	return perks
}

func SummarizeBoosts(count int) BoostSummary {
	level := BoostLevelFor(count)
	summary := BoostSummary{BoostCount: count, Level: level, Perks: BoostPerks(level)}
	if level < len(boostLevelThresholds) {
		required := boostLevelThresholds[level]
		summary.Next = &NextBoostLevel{Level: level + 1, BoostsRequired: required, BoostsMissing: required - count}
	}
	return summary
}

func ValidateVanityCode(code string) error {
	if !vanityPattern.MatchString(code) {
		return ErrVanityCodeInvalid
	}
	return nil
}

// BoostLevelRequiredError means the server's level doesn't unlock that perk yet.
type BoostLevelRequiredError struct {
	Required int
}

func (e *BoostLevelRequiredError) Error() string {
	return fmt.Sprintf("this server needs boost level %d for that", e.Required)
}
