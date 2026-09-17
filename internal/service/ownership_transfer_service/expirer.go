package ownership_transfer_service

import (
	"context"
	"log"
	"time"

	"github.com/discordia-grupo01/servers/internal/model"
)

// ExpirySweepInterval is how often Expirer looks for stale pending
// transfers. It only has to be small relative to model.OwnershipTransferTTL:
// Service already re-checks a transfer's age on every read that touches it
// (GetPendingTransfer, accept/reject, a new InitiateTransfer), so this sweep
// only matters for a transfer nobody -- neither the owner nor the recipient
// -- ever queries again.
const ExpirySweepInterval = 5 * time.Minute

// Expirer periodically rejects pending ownership transfers older than
// model.OwnershipTransferTTL, so a transfer expires on its own instead of
// depending on the recipient (or anyone else) reopening the server.
type Expirer struct {
	transfers Transfers
	now       func() time.Time
}

func NewExpirer(transfers Transfers) *Expirer {
	return &Expirer{transfers: transfers, now: func() time.Time { return time.Now().UTC() }}
}

// Start blocks, sweeping every ExpirySweepInterval until ctx is cancelled.
// Run it in its own goroutine.
func (e *Expirer) Start(ctx context.Context) {
	ticker := time.NewTicker(ExpirySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.sweepOnce(ctx)
		}
	}
}

func (e *Expirer) sweepOnce(ctx context.Context) {
	now := e.now()
	n, err := e.transfers.ExpireStalePending(ctx, now.Add(-model.OwnershipTransferTTL), now)
	if err != nil {
		log.Printf("ownership_transfer: expiry sweep failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("ownership_transfer: expired %d stale pending transfer(s)", n)
	}
}
