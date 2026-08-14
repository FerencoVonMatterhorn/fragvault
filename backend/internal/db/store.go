package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fragvault/fragvault/backend/internal/matches"
)

// Store is the database-backed replacement for matches.Store. It keeps the
// same method shapes and the same types on the boundary, so the HTTP handlers
// didn't have to change beyond passing a context.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertProfile creates or refreshes the basic profile after Steam login.
func (s *Store) UpsertProfile(ctx context.Context, steamID, personaName, avatarURL string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO players (steam_id, persona_name, avatar_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (steam_id) DO UPDATE
		SET persona_name = EXCLUDED.persona_name,
		    avatar_url   = EXCLUDED.avatar_url,
		    updated_at   = now()`,
		steamID, personaName, avatarURL)
	if err != nil {
		return fmt.Errorf("upserting player %s: %w", steamID, err)
	}
	return nil
}

// SetOnboarding stores the auth code and the sharecode discovery starts from.
func (s *Store) SetOnboarding(ctx context.Context, steamID, authCode, startingShareCode string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE players
		SET auth_code = $2, latest_known_code = $3, updated_at = now()
		WHERE steam_id = $1`,
		steamID, authCode, startingShareCode)
	if err != nil {
		return fmt.Errorf("setting onboarding for %s: %w", steamID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no player record for steamid %s — must log in first", steamID)
	}
	return nil
}

// GetPlayer returns the record for a steamid, or nil if there is none.
func (s *Store) GetPlayer(ctx context.Context, steamID string) (*matches.PlayerRecord, error) {
	rec := &matches.PlayerRecord{}
	err := s.pool.QueryRow(ctx, `
		SELECT steam_id, persona_name, avatar_url, auth_code, latest_known_code
		FROM players WHERE steam_id = $1`, steamID).
		Scan(&rec.SteamID, &rec.PersonaName, &rec.AvatarURL, &rec.AuthCode, &rec.LatestKnownCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading player %s: %w", steamID, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT share_code, match_id, reservation_id, tv_port, discovered_at
		FROM matches WHERE steam_id = $1
		ORDER BY discovered_at, share_code`, steamID)
	if err != nil {
		return nil, fmt.Errorf("loading matches for %s: %w", steamID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			m             matches.Match
			matchID       int64
			reservationID int64
			tvPort        int32
		)
		if err := rows.Scan(&m.ShareCode, &matchID, &reservationID, &tvPort, &m.DiscoveredAt); err != nil {
			return nil, fmt.Errorf("scanning match row: %w", err)
		}
		m.MatchID = uint64(matchID)
		m.ReservationID = uint64(reservationID)
		m.TVPort = uint16(tvPort)
		rec.Matches = append(rec.Matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating matches for %s: %w", steamID, err)
	}
	return rec, nil
}

// AppendMatches records newly discovered matches and advances the player's
// latest known sharecode, as one transaction — advancing the pointer without
// storing the matches would lose them permanently, since discovery only ever
// walks forward.
func (s *Store) AppendMatches(ctx context.Context, steamID string, newMatches []matches.Match, newLatestCode string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		// No-op once the transaction has committed.
		_ = tx.Rollback(context.Background())
	}()

	discoveredAt := time.Now()
	for _, m := range newMatches {
		if _, err := tx.Exec(ctx, `
			INSERT INTO matches (share_code, steam_id, match_id, reservation_id, tv_port, discovered_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (share_code) DO NOTHING`,
			m.ShareCode, steamID, int64(m.MatchID), int64(m.ReservationID), int32(m.TVPort), discoveredAt); err != nil {
			return fmt.Errorf("inserting match %s: %w", m.ShareCode, err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE players SET latest_known_code = $2, updated_at = now()
		WHERE steam_id = $1`, steamID, newLatestCode); err != nil {
		return fmt.Errorf("advancing latest known code for %s: %w", steamID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing matches for %s: %w", steamID, err)
	}
	return nil
}
