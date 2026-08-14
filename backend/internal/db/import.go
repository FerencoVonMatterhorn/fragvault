package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fragvault/fragvault/backend/internal/matches"
)

// legacyFile mirrors the Phase 1 JSON store's on-disk shape.
type legacyFile struct {
	Players map[string]*matches.PlayerRecord `json:"players"`
}

// ImportLegacyJSON moves the JSON-file store into Postgres, once.
//
// Runs at boot and does nothing in the normal case. It exists because the
// live deployment already has an onboarded user, and their auth code is not
// recoverable — losing it means asking them to fetch a new one from Steam
// Support. The import bails if any player already exists, so this is a
// one-way migration rather than a sync that could clobber newer data.
func ImportLegacyJSON(ctx context.Context, pool *pgxpool.Pool, path string) (players int, err error) {
	if path == "" {
		return 0, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading legacy store %s: %w", path, err)
	}

	var existing int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&existing); err != nil {
		return 0, fmt.Errorf("counting existing players: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	var f legacyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return 0, fmt.Errorf("parsing legacy store %s: %w", path, err)
	}
	if len(f.Players) == 0 {
		return 0, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	for steamID, rec := range f.Players {
		if _, err := tx.Exec(ctx, `
			INSERT INTO players (steam_id, persona_name, avatar_url, auth_code, latest_known_code)
			VALUES ($1, $2, $3, $4, $5)`,
			steamID, rec.PersonaName, rec.AvatarURL, rec.AuthCode, rec.LatestKnownCode); err != nil {
			return 0, fmt.Errorf("importing player %s: %w", steamID, err)
		}

		for _, m := range rec.Matches {
			if _, err := tx.Exec(ctx, `
				INSERT INTO matches (share_code, steam_id, match_id, reservation_id, tv_port, discovered_at)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (share_code) DO NOTHING`,
				m.ShareCode, steamID, int64(m.MatchID), int64(m.ReservationID), int32(m.TVPort), m.DiscoveredAt); err != nil {
				return 0, fmt.Errorf("importing match %s: %w", m.ShareCode, err)
			}
		}
		players++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing import: %w", err)
	}
	return players, nil
}
