package moderation_service

import (
	"context"
	"time"

	"github.com/discordia-grupo01/servers/internal/logging"
)

const MuteExpirySweepInterval = time.Minute

type MuteExpirer struct {
	mutes MuteRepository
	now   func() time.Time
}

func NewMuteExpirer(mutes MuteRepository) *MuteExpirer {
	return &MuteExpirer{mutes: mutes, now: func() time.Time { return time.Now().UTC() }}
}

func (e *MuteExpirer) Start(ctx context.Context) {
	ticker := time.NewTicker(MuteExpirySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.SweepOnce(ctx)
		}
	}
}

func (e *MuteExpirer) SweepOnce(ctx context.Context) {
	if count, err := e.mutes.ExpireMutes(ctx, e.now()); err != nil {
		logging.Errorf("member_mute: expiry sweep failed: %v", err)
	} else if count > 0 {
		logging.Infof("member_mute: expired %d mute(s)", count)
	}
}
