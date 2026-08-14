package db_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fragvault/fragvault/backend/internal/db"
	"github.com/fragvault/fragvault/backend/internal/matches"
)

// These exercise real SQL against a real Postgres, because the parts most
// worth testing here — migrations applying at boot, the uniqueness constraint
// that prevents re-analysis, transactional appends — don't exist until a
// database does. CI provides one; locally the tests skip.
func testPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping database integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	// Each run starts from nothing, so migrations are genuinely exercised
	// rather than found already applied.
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("resetting schema: %v", err)
	}
	return ctx, pool
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx, pool := testPool(t)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Running again is the normal case: every backend restart calls this.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var tables int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('players','matches','demo_analyses','highlights','schema_migrations')
	`).Scan(&tables); err != nil {
		t.Fatalf("counting tables: %v", err)
	}
	if tables != 5 {
		t.Fatalf("expected 5 tables, got %d", tables)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(pool)

	const steamID = "76561198000000001"

	if err := store.UpsertProfile(ctx, steamID, "tester", "http://avatar"); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	// Logging in again must not duplicate or wipe the player.
	if err := store.UpsertProfile(ctx, steamID, "tester-renamed", "http://avatar2"); err != nil {
		t.Fatalf("UpsertProfile (second): %v", err)
	}

	if err := store.SetOnboarding(ctx, steamID, "AUTH-CODE", "CSGO-aaaaa-bbbbb-ccccc-ddddd-eeeee"); err != nil {
		t.Fatalf("SetOnboarding: %v", err)
	}

	found := []matches.Match{
		{ShareCode: "CSGO-11111-11111-11111-11111-11111", MatchID: 3700000000000000001, ReservationID: 42, TVPort: 60000},
		{ShareCode: "CSGO-22222-22222-22222-22222-22222", MatchID: 3700000000000000002, ReservationID: 43, TVPort: 60001},
	}
	if err := store.AppendMatches(ctx, steamID, found, found[1].ShareCode); err != nil {
		t.Fatalf("AppendMatches: %v", err)
	}
	// Discovery re-reporting a known match must not error or duplicate.
	if err := store.AppendMatches(ctx, steamID, found, found[1].ShareCode); err != nil {
		t.Fatalf("AppendMatches (repeat): %v", err)
	}

	rec, err := store.GetPlayer(ctx, steamID)
	if err != nil {
		t.Fatalf("GetPlayer: %v", err)
	}
	if rec == nil {
		t.Fatal("GetPlayer returned nil for an existing player")
	}
	if rec.PersonaName != "tester-renamed" {
		t.Errorf("persona = %q, want tester-renamed", rec.PersonaName)
	}
	if rec.AuthCode != "AUTH-CODE" {
		t.Errorf("auth code = %q", rec.AuthCode)
	}
	if rec.LatestKnownCode != found[1].ShareCode {
		t.Errorf("latest known code = %q, want %q", rec.LatestKnownCode, found[1].ShareCode)
	}
	if len(rec.Matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(rec.Matches))
	}
	// uint64 survives the round trip through BIGINT.
	if rec.Matches[0].MatchID != found[0].MatchID {
		t.Errorf("match id = %d, want %d", rec.Matches[0].MatchID, found[0].MatchID)
	}

	missing, err := store.GetPlayer(ctx, "76561198000000999")
	if err != nil {
		t.Fatalf("GetPlayer (unknown): %v", err)
	}
	if missing != nil {
		t.Error("expected nil for an unknown player, got a record")
	}
}

func TestAnalysisIsUniquePerParserVersion(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(pool)

	const steamID = "76561198000000002"
	const shareCode = "CSGO-33333-33333-33333-33333-33333"

	if err := store.UpsertProfile(ctx, steamID, "tester", ""); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	if err := store.AppendMatches(ctx, steamID, []matches.Match{{ShareCode: shareCode}}, shareCode); err != nil {
		t.Fatalf("AppendMatches: %v", err)
	}

	insert := `INSERT INTO demo_analyses (share_code, parser_version, status) VALUES ($1, $2, 'pending')`
	if _, err := pool.Exec(ctx, insert, shareCode, 1); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// This is the whole point of the constraint: asking again must not queue
	// a second parse of the same demo.
	if _, err := pool.Exec(ctx, insert, shareCode, 1); err == nil {
		t.Fatal("expected a unique violation for the same (share_code, parser_version)")
	}
	// A new parser version is exactly when re-analysis *should* happen.
	if _, err := pool.Exec(ctx, insert, shareCode, 2); err != nil {
		t.Fatalf("insert with bumped parser version: %v", err)
	}
}

func TestImportLegacyJSON(t *testing.T) {
	ctx, pool := testPool(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	legacy := struct {
		Players map[string]*matches.PlayerRecord `json:"players"`
	}{
		Players: map[string]*matches.PlayerRecord{
			"76561198000000003": {
				SteamID:         "76561198000000003",
				PersonaName:     "legacy-user",
				AuthCode:        "LEGACY-AUTH",
				LatestKnownCode: "CSGO-44444-44444-44444-44444-44444",
				Matches: []matches.Match{
					{ShareCode: "CSGO-44444-44444-44444-44444-44444", MatchID: 7, DiscoveredAt: time.Now().UTC().Truncate(time.Second)},
				},
			},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fragvault-data.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	n, err := db.ImportLegacyJSON(ctx, pool, path)
	if err != nil {
		t.Fatalf("ImportLegacyJSON: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d players, want 1", n)
	}

	// Every restart calls this. It must be a no-op once data exists, rather
	// than re-importing and clobbering newer state.
	n, err = db.ImportLegacyJSON(ctx, pool, path)
	if err != nil {
		t.Fatalf("ImportLegacyJSON (second): %v", err)
	}
	if n != 0 {
		t.Fatalf("second import moved %d players, want 0", n)
	}

	// A missing file is the normal case for every deployment after the first.
	if n, err := db.ImportLegacyJSON(ctx, pool, filepath.Join(t.TempDir(), "nope.json")); err != nil || n != 0 {
		t.Fatalf("missing file: got (%d, %v), want (0, nil)", n, err)
	}

	rec, err := db.NewStore(pool).GetPlayer(ctx, "76561198000000003")
	if err != nil {
		t.Fatalf("GetPlayer: %v", err)
	}
	if rec == nil || rec.AuthCode != "LEGACY-AUTH" || len(rec.Matches) != 1 {
		t.Fatalf("imported record looks wrong: %+v", rec)
	}
}
