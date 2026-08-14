package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fragvault/fragvault/backend/internal/db"
	"github.com/fragvault/fragvault/backend/internal/demos"
	"github.com/fragvault/fragvault/backend/internal/matches"
)

type matchesMatch = matches.Match

// seedMatch creates the player and match rows an analysis needs to exist.
func seedMatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, steamID, shareCode string) *db.Store {
	t.Helper()
	store := db.NewStore(pool)
	if err := store.UpsertProfile(ctx, steamID, "tester", ""); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	if err := store.AppendMatches(ctx, steamID, []matchesMatch{{ShareCode: shareCode}}, shareCode); err != nil {
		t.Fatalf("AppendMatches: %v", err)
	}
	return store
}

func statusOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shareCode string) (status, errMsg, demoURL string) {
	t.Helper()
	if err := pool.QueryRow(ctx,
		`SELECT status, error, demo_url FROM demo_analyses WHERE share_code = $1 AND parser_version = $2`,
		shareCode, demos.ParserVersion).Scan(&status, &errMsg, &demoURL); err != nil {
		t.Fatalf("reading analysis: %v", err)
	}
	return
}

func TestEnqueueIsIdempotentWhilePending(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const shareCode = "CSGO-aaaaa-aaaaa-aaaaa-aaaaa-aaaaa"
	store := seedMatch(t, ctx, pool, "76561198000000010", shareCode)

	queued, err := store.Enqueue(ctx, shareCode, "http://example.test/a.dem")
	if err != nil || !queued {
		t.Fatalf("first Enqueue: queued=%v err=%v", queued, err)
	}

	// Asking again must not start a second parse of the same demo.
	queued, err = store.Enqueue(ctx, shareCode, "http://example.test/b.dem")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if queued {
		t.Error("re-enqueueing a pending analysis queued more work")
	}
	if _, _, url := statusOf(t, ctx, pool, shareCode); url != "http://example.test/a.dem" {
		t.Errorf("demo_url = %q — a no-op enqueue must not overwrite it", url)
	}
}

func TestEnqueueRetriesAFailedAnalysis(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const shareCode = "CSGO-bbbbb-bbbbb-bbbbb-bbbbb-bbbbb"
	store := seedMatch(t, ctx, pool, "76561198000000011", shareCode)

	if _, err := store.Enqueue(ctx, shareCode, "http://example.test/old.dem"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil || job == nil {
		t.Fatalf("ClaimNext: job=%v err=%v", job, err)
	}
	if err := store.Fail(ctx, job.ID, "game coordinator unavailable"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	// The whole point: a failure is recoverable without bumping the parser
	// version, because the cause is usually transient.
	queued, err := store.Enqueue(ctx, shareCode, "http://example.test/new.dem")
	if err != nil {
		t.Fatalf("retry Enqueue: %v", err)
	}
	if !queued {
		t.Fatal("retrying a failed analysis queued nothing")
	}

	status, errMsg, url := statusOf(t, ctx, pool, shareCode)
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
	if errMsg != "" {
		t.Errorf("error = %q — the previous failure must be cleared", errMsg)
	}
	if url != "http://example.test/new.dem" {
		t.Errorf("demo_url = %q — a retry must pick up the freshly resolved URL", url)
	}

	// And it must be claimable again, or the retry achieved nothing.
	if job, err := store.ClaimNext(ctx); err != nil || job == nil {
		t.Fatalf("retried analysis was not claimable: job=%v err=%v", job, err)
	}
}

func TestEnqueueLeavesAFinishedAnalysisAlone(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const shareCode = "CSGO-ccccc-ccccc-ccccc-ccccc-ccccc"
	store := seedMatch(t, ctx, pool, "76561198000000012", shareCode)

	if _, err := store.Enqueue(ctx, shareCode, "http://example.test/a.dem"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job, err := store.ClaimNext(ctx)
	if err != nil || job == nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if err := store.Complete(ctx, job.ID, demos.Result{MapName: "de_mirage"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Re-analysing a finished demo is what bumping ParserVersion is for.
	queued, err := store.Enqueue(ctx, shareCode, "http://example.test/b.dem")
	if err != nil {
		t.Fatalf("Enqueue after done: %v", err)
	}
	if queued {
		t.Error("re-enqueueing a completed analysis queued more work")
	}
	if status, _, _ := statusOf(t, ctx, pool, shareCode); status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestRequeueOrphanedRecoversInterruptedJobs(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const shareCode = "CSGO-ddddd-ddddd-ddddd-ddddd-ddddd"
	store := seedMatch(t, ctx, pool, "76561198000000013", shareCode)

	if _, err := store.Enqueue(ctx, shareCode, "http://example.test/a.dem"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := store.ClaimNext(ctx); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	// Simulates a restart mid-analysis: the row is running, but the process
	// that claimed it is gone and nothing will ever finish it.
	n, err := store.RequeueOrphaned(ctx)
	if err != nil {
		t.Fatalf("RequeueOrphaned: %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued %d, want 1", n)
	}
	if job, err := store.ClaimNext(ctx); err != nil || job == nil {
		t.Fatalf("orphaned job was not claimable after requeue: job=%v err=%v", job, err)
	}
}
