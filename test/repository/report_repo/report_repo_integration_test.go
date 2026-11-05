package report_repo_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/report_repo"
)

const checkViolation = "23514"

func newRepo(t *testing.T) (*report_repo.Repository, *pgxpool.Pool) {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() { dropTables(ctx, pool) })

	dropTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0017_user_profiles.up.sql",
		"../../../migrations/0029_reports.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return report_repo.NewRepository(pool), pool
}

func dropTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS reports, user_profiles, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func seedServer(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner-1')`, id, "s-"+id.String()); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedChannel(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'general', 'text')`, id, serverID); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedProfile(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID, name string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, name, version) VALUES ($1, $2, 1)`, userID, name); err != nil {
		t.Fatal(err)
	}
}

func messageReport(serverID, channelID uuid.UUID, reporter string, at time.Time) report.Report {
	messageID := uuid.New()
	return report.Report{
		ID: uuid.New(), ServerID: serverID, Kind: report.KindMessage, ReporterID: reporter,
		ChannelID: &channelID, MessageID: &messageID, Reason: "spam", Status: report.StatusPending, CreatedAt: at,
	}
}

func userReport(serverID uuid.UUID, reporter, target string, at time.Time) report.Report {
	return report.Report{
		ID: uuid.New(), ServerID: serverID, Kind: report.KindUser, ReporterID: reporter,
		ReportedUserID: &target, Reason: "acoso", Status: report.StatusPending, CreatedAt: at,
	}
}

func TestCreateAndList_StoreAMessageAndAUserReport(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)
	now := time.Now().UTC().Truncate(time.Microsecond)

	msg := messageReport(serverID, channelID, "u1", now)
	usr := userReport(serverID, "u1", "u2", now.Add(time.Second))
	for _, rep := range []report.Report{msg, usr} {
		if err := repo.Create(ctx, rep); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, total, err := repo.List(ctx, serverID, nil, 20, 0)
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("List = %d reports, total %d, %v", len(got), total, err)
	}
	if got[0].ID != usr.ID || got[1].ID != msg.ID {
		t.Errorf("order = %v, %v; want newest first", got[0].ID, got[1].ID)
	}
	if got[1].Kind != report.KindMessage || got[1].ChannelID == nil || *got[1].ChannelID != channelID ||
		got[1].MessageID == nil || *got[1].MessageID != *msg.MessageID || got[1].ReportedUserID != nil {
		t.Errorf("message report = %+v", got[1])
	}
	if got[0].Kind != report.KindUser || got[0].ReportedUserID == nil || *got[0].ReportedUserID != "u2" ||
		got[0].ChannelID != nil || got[0].MessageID != nil {
		t.Errorf("user report = %+v", got[0])
	}
	if got[0].Status != report.StatusPending || got[0].ResolvedAt != nil || got[0].ResolvedBy != nil || got[0].Reason != "acoso" {
		t.Errorf("state = %+v", got[0])
	}
	if !got[1].CreatedAt.Equal(now) {
		t.Errorf("created_at = %v, want %v", got[1].CreatedAt, now)
	}
}

func TestList_CarriesTheReplicatedProfilesAndNilWhenMissing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()
	seedProfile(ctx, t, pool, "u1", "Ana")
	seedProfile(ctx, t, pool, "u2", "Beto")

	if err := repo.Create(ctx, userReport(serverID, "u1", "u2", now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u3", "u4", now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}

	got, _, err := repo.List(ctx, serverID, nil, 20, 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("List = %d, %v", len(got), err)
	}
	if got[0].ReporterProfile == nil || got[0].ReporterProfile.Name != "Ana" ||
		got[0].ReportedProfile == nil || got[0].ReportedProfile.Name != "Beto" {
		t.Errorf("profiles = %+v / %+v", got[0].ReporterProfile, got[0].ReportedProfile)
	}
	if got[1].ReporterProfile != nil || got[1].ReportedProfile != nil {
		t.Errorf("missing profiles must be nil: %+v / %+v", got[1].ReporterProfile, got[1].ReportedProfile)
	}
}

func TestCreate_CA2_APendingDuplicateIsRejected(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)
	now := time.Now().UTC()

	first := messageReport(serverID, channelID, "u1", now)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	again := messageReport(serverID, channelID, "u1", now)
	again.MessageID = first.MessageID
	if err := repo.Create(ctx, again); !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("same message: err = %v, want ErrReportAlreadyPending", err)
	}

	if err := repo.Create(ctx, userReport(serverID, "u1", "u2", now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u1", "u2", now)); !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("same user: err = %v, want ErrReportAlreadyPending", err)
	}

	_, total, _ := repo.List(ctx, serverID, nil, 20, 0)
	if total != 2 {
		t.Errorf("total = %d, want the 2 originals", total)
	}
}

func TestCreate_TheSameContentCanBeReportedByAnotherMemberOrAnotherTarget(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)
	now := time.Now().UTC()

	first := messageReport(serverID, channelID, "u1", now)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	byOther := messageReport(serverID, channelID, "u2", now)
	byOther.MessageID = first.MessageID
	if err := repo.Create(ctx, byOther); err != nil {
		t.Errorf("another reporter, same message: %v", err)
	}
	if err := repo.Create(ctx, messageReport(serverID, channelID, "u1", now)); err != nil {
		t.Errorf("same reporter, another message: %v", err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u1", "u9", now)); err != nil {
		t.Errorf("same reporter, a user: %v", err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u2", "u9", now)); err != nil {
		t.Errorf("another reporter, same user: %v", err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u1", "u8", now)); err != nil {
		t.Errorf("same reporter, another user: %v", err)
	}
}

func TestCreate_CA2_AClosedReportAllowsReportingAgain(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()

	first := userReport(serverID, "u1", "u2", now)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, first.ID, report.StatusDismissed, "mod-1", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u1", "u2", now)); err != nil {
		t.Fatalf("report after the previous one was closed: %v", err)
	}
	if err := repo.Create(ctx, userReport(serverID, "u1", "u2", now)); !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("a second pending one: err = %v, want ErrReportAlreadyPending", err)
	}
}

func TestCreate_ConcurrentDuplicatesLeaveExactlyOnePending(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()

	const attempts = 12
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = repo.Create(ctx, userReport(serverID, "u1", "u2", now))
		}(i)
	}
	wg.Wait()

	created, duplicates := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, model.ErrReportAlreadyPending):
			duplicates++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if created != 1 || duplicates != attempts-1 {
		t.Fatalf("created %d, duplicates %d; want 1 and %d", created, duplicates, attempts-1)
	}
}

func TestList_FiltersByStatusAndPaginates(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	base := time.Now().UTC().Truncate(time.Microsecond)

	var ids []uuid.UUID
	for i, target := range []string{"t1", "t2", "t3", "t4", "t5"} {
		rep := userReport(serverID, "u1", target, base.Add(time.Duration(i)*time.Second))
		if err := repo.Create(ctx, rep); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rep.ID)
	}
	if _, err := repo.Close(ctx, serverID, ids[0], report.StatusResolved, "mod-1", base); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, ids[1], report.StatusDismissed, "mod-1", base); err != nil {
		t.Fatal(err)
	}

	pending, resolved, dismissed := report.StatusPending, report.StatusResolved, report.StatusDismissed
	for name, tc := range map[string]struct {
		status *report.Status
		total  int
	}{"all": {nil, 5}, "pending": {&pending, 3}, "resolved": {&resolved, 1}, "dismissed": {&dismissed, 1}} {
		got, total, err := repo.List(ctx, serverID, tc.status, 20, 0)
		if err != nil || total != tc.total || len(got) != tc.total {
			t.Errorf("%s: %d reports, total %d, %v; want %d", name, len(got), total, err, tc.total)
		}
		if tc.status != nil {
			for _, rep := range got {
				if rep.Status != *tc.status {
					t.Errorf("%s: got a %s report", name, rep.Status)
				}
			}
		}
	}

	page, total, err := repo.List(ctx, serverID, &pending, 2, 1)
	if err != nil || total != 3 || len(page) != 2 {
		t.Fatalf("page = %d reports, total %d, %v", len(page), total, err)
	}
	if page[0].ID != ids[3] || page[1].ID != ids[2] {
		t.Errorf("page = %v, %v; want the 2nd and 3rd newest pending", page[0].ID, page[1].ID)
	}
}

func TestList_OnlyReturnsTheServersOwnReports(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	mine, other := seedServer(ctx, t, pool), seedServer(ctx, t, pool)
	now := time.Now().UTC()

	if err := repo.Create(ctx, userReport(mine, "u1", "u2", now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, userReport(other, "u1", "u2", now)); err != nil {
		t.Fatalf("the same report in another server must be allowed: %v", err)
	}

	got, total, err := repo.List(ctx, mine, nil, 20, 0)
	if err != nil || total != 1 || len(got) != 1 || got[0].ServerID != mine {
		t.Fatalf("List = %+v, total %d, %v", got, total, err)
	}
}

func TestList_NoReportsIsAnEmptyNonNilSlice(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	got, total, err := repo.List(ctx, serverID, nil, 20, 0)
	if err != nil || total != 0 || got == nil || len(got) != 0 {
		t.Fatalf("List = %#v, total %d, %v", got, total, err)
	}
}

func TestClose_RecordsWhoAndWhen(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	closedAt := now.Add(time.Hour)

	rep := userReport(serverID, "u1", "u2", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "mod-1", closedAt)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got.ID != rep.ID || got.Status != report.StatusResolved || got.ResolvedBy == nil || *got.ResolvedBy != "mod-1" ||
		got.ResolvedAt == nil || !got.ResolvedAt.Equal(closedAt) || got.Reason != "acoso" {
		t.Errorf("closed = %+v", got)
	}

	listed, _, _ := repo.List(ctx, serverID, nil, 20, 0)
	if len(listed) != 1 || listed[0].Status != report.StatusResolved {
		t.Errorf("listed = %+v", listed)
	}
}

func TestClose_AReportIsClosedOnlyOnce(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()

	rep := userReport(serverID, "u1", "u2", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "mod-1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusDismissed, "mod-2", now); !errors.Is(err, model.ErrReportNotPending) {
		t.Fatalf("second close: err = %v, want ErrReportNotPending", err)
	}

	listed, _, _ := repo.List(ctx, serverID, nil, 20, 0)
	if listed[0].Status != report.StatusResolved || *listed[0].ResolvedBy != "mod-1" {
		t.Errorf("the first closing must stand: %+v", listed[0])
	}
}

func TestClose_ConcurrentClosersLeaveASingleWinner(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()

	rep := userReport(serverID, "u1", "u2", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}

	const attempts = 8
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "mod", now)
		}(i)
	}
	wg.Wait()

	won, lost := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, model.ErrReportNotPending):
			lost++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 || lost != attempts-1 {
		t.Fatalf("won %d, lost %d; want 1 and %d", won, lost, attempts-1)
	}
}

func TestClose_UnknownOrForeignReportIsNotFound(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	mine, other := seedServer(ctx, t, pool), seedServer(ctx, t, pool)
	now := time.Now().UTC()

	rep := userReport(other, "u1", "u2", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Close(ctx, mine, uuid.New(), report.StatusResolved, "mod-1", now); !errors.Is(err, model.ErrReportNotFound) {
		t.Errorf("unknown id: err = %v, want ErrReportNotFound", err)
	}
	if _, err := repo.Close(ctx, mine, rep.ID, report.StatusResolved, "mod-1", now); !errors.Is(err, model.ErrReportNotFound) {
		t.Errorf("report of another server: err = %v, want ErrReportNotFound", err)
	}

	listed, _, _ := repo.List(ctx, other, nil, 20, 0)
	if listed[0].Status != report.StatusPending {
		t.Errorf("the other server's report must stay pending: %+v", listed[0])
	}
}

func TestDeletingTheChannelRemovesItsMessageReportsButNotTheUserOnes(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)
	keptChannel := seedChannel(ctx, t, pool, serverID)
	now := time.Now().UTC()

	for _, rep := range []report.Report{
		messageReport(serverID, channelID, "u1", now),
		messageReport(serverID, keptChannel, "u1", now),
		userReport(serverID, "u1", "u2", now),
	} {
		if err := repo.Create(ctx, rep); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM channels WHERE id = $1`, channelID); err != nil {
		t.Fatal(err)
	}
	_, total, _ := repo.List(ctx, serverID, nil, 20, 0)
	if total != 2 {
		t.Errorf("total after deleting a channel = %d, want 2", total)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM reports`).Scan(&left); err != nil || left != 0 {
		t.Errorf("reports left after deleting the server = %d, %v", left, err)
	}
}

func TestSchema_RejectsInconsistentRows(t *testing.T) {
	_, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)

	bad := map[string]string{
		"message report without a message": `INSERT INTO reports (id, server_id, kind, reporter_id, channel_id, reason, created_at)
			VALUES (gen_random_uuid(), $1, 'message', 'u1', $2, 'x', now())`,
		"user report with a channel": `INSERT INTO reports (id, server_id, kind, reporter_id, channel_id, reported_user_id, reason, created_at)
			VALUES (gen_random_uuid(), $1, 'user', 'u1', $2, 'u2', 'x', now())`,
		"unknown kind": `INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, created_at)
			VALUES (gen_random_uuid(), $1, 'channel', 'u1', 'u2', 'x', now())`,
		"empty reason": `INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, created_at)
			VALUES (gen_random_uuid(), $1, 'user', 'u1', 'u2', '', now())`,
		"unknown status": `INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, status, created_at)
			VALUES (gen_random_uuid(), $1, 'user', 'u1', 'u2', 'x', 'archived', now())`,
		"closed without who and when": `INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, status, created_at)
			VALUES (gen_random_uuid(), $1, 'user', 'u1', 'u2', 'x', 'resolved', now())`,
		"pending with a closing date": `INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, resolved_at, resolved_by, created_at)
			VALUES (gen_random_uuid(), $1, 'user', 'u1', 'u2', 'x', now(), 'mod', now())`,
	}
	for name, query := range bad {
		args := []any{serverID}
		if name == "message report without a message" || name == "user report with a channel" {
			args = append(args, channelID)
		}
		_, err := pool.Exec(ctx, query, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != checkViolation {
			t.Errorf("%s: err = %v, want a CHECK violation", name, err)
		}
	}
}
