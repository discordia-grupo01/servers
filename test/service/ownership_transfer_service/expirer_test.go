package ownership_transfer_service_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
)

func TestNewExpirer_ReturnsUsableInstance(t *testing.T) {
	expirer := ownership_transfer_service.NewExpirer(&fakeTransfers{})
	if expirer == nil {
		t.Fatal("NewExpirer returned nil")
	}
}

func TestExpirer_SweepOnce_CallsExpireStalePendingOnce(t *testing.T) {
	transfers := &fakeTransfers{expireStaleN: 2}
	expirer := ownership_transfer_service.NewExpirer(transfers)

	expirer.SweepOnce(context.Background())

	if transfers.expireStaleCalls != 1 {
		t.Errorf("expireStaleCalls = %d, want 1", transfers.expireStaleCalls)
	}
}

// TestExpirer_SweepOnce_CutoffMatchesTTL asserts the cutoff SweepOnce passes
// to the repo is "now - OwnershipTransferTTL": within a small tolerance,
// since NewExpirer's clock isn't injectable and the test itself takes some
// (tiny) wall-clock time to run.
func TestExpirer_SweepOnce_CutoffMatchesTTL(t *testing.T) {
	transfers := &fakeCapturingTransfers{}
	expirer := ownership_transfer_service.NewExpirer(transfers)

	before := time.Now().UTC()
	expirer.SweepOnce(context.Background())
	after := time.Now().UTC()

	if transfers.resolvedAt.Before(before) || transfers.resolvedAt.After(after) {
		t.Errorf("resolvedAt = %v, want between %v and %v", transfers.resolvedAt, before, after)
	}

	gotTTL := transfers.resolvedAt.Sub(transfers.cutoff)
	if math.Abs(float64(gotTTL-model.OwnershipTransferTTL)) > float64(time.Second) {
		t.Errorf("resolvedAt - cutoff = %v, want ~%v", gotTTL, model.OwnershipTransferTTL)
	}
}

// TestExpirer_SweepOnce_RepoError_DoesNotPanic covers the log-and-continue
// branch: a failed sweep must not propagate or crash the caller (there's no
// caller to hand the error to -- Start runs this in a bare goroutine).
func TestExpirer_SweepOnce_RepoError_DoesNotPanic(t *testing.T) {
	transfers := &fakeTransfers{expireStaleErr: errors.New("db unreachable")}
	expirer := ownership_transfer_service.NewExpirer(transfers)

	expirer.SweepOnce(context.Background())

	if transfers.expireStaleCalls != 1 {
		t.Errorf("expireStaleCalls = %d, want 1", transfers.expireStaleCalls)
	}
}

func TestExpirer_Start_ReturnsWhenContextCancelled(t *testing.T) {
	expirer := ownership_transfer_service.NewExpirer(&fakeTransfers{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		expirer.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context was cancelled")
	}
}

// fakeCapturingTransfers is fakeTransfers plus recording the exact args
// SweepOnce passed, to check the cutoff/resolvedAt math without needing to
// inject Expirer's internal clock.
type fakeCapturingTransfers struct {
	fakeTransfers
	cutoff     time.Time
	resolvedAt time.Time
}

func (f *fakeCapturingTransfers) ExpireStalePending(ctx context.Context, cutoff, resolvedAt time.Time) (int, error) {
	f.cutoff = cutoff
	f.resolvedAt = resolvedAt
	return f.fakeTransfers.ExpireStalePending(ctx, cutoff, resolvedAt)
}
