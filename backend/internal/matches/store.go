package matches

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Match is a discovered CS2 match, identified by its sharecode. See
// /docs/architecture.md for why this doesn't include map/score/etc yet.
type Match struct {
	ShareCode     string    `json:"share_code"`
	MatchID       uint64    `json:"match_id"`
	ReservationID uint64    `json:"reservation_id"`
	TVPort        uint16    `json:"tv_port"`
	DiscoveredAt  time.Time `json:"discovered_at"`
}

// PlayerRecord is everything stored for one onboarded Steam user.
type PlayerRecord struct {
	SteamID         string  `json:"steam_id"`
	PersonaName     string  `json:"persona_name"`
	AvatarURL       string  `json:"avatar_url"`
	AuthCode        string  `json:"auth_code"`         // Valve "game authentication code", user-supplied once
	LatestKnownCode string  `json:"latest_known_code"` // most recent sharecode we've walked to
	Matches         []Match `json:"matches"`           // newest last
}

// Store is a minimal JSON-file-backed persistence layer. It's intentionally
// not a database — for the Phase 1 POC's traffic (a handful of users
// manually onboarding themselves), a single JSON file guarded by a mutex is
// simpler to operate and reason about than adding a database dependency.
// Swap for real storage when this moves onto the Azure backend proper.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

type fileFormat struct {
	Players map[string]*PlayerRecord `json:"players"` // keyed by steamid64
}

func (s *Store) load() (*fileFormat, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &fileFormat{Players: map[string]*PlayerRecord{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading store file: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing store file: %w", err)
	}
	if f.Players == nil {
		f.Players = map[string]*PlayerRecord{}
	}
	return &f, nil
}

func (s *Store) save(f *fileFormat) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling store file: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing store temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("renaming store temp file: %w", err)
	}
	return nil
}

// UpsertProfile creates or updates the basic profile info for a player after
// Steam login (called from the OpenID callback handler).
func (s *Store) UpsertProfile(steamID, personaName, avatarURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	rec, ok := f.Players[steamID]
	if !ok {
		rec = &PlayerRecord{SteamID: steamID}
		f.Players[steamID] = rec
	}
	rec.PersonaName = personaName
	rec.AvatarURL = avatarURL
	return s.save(f)
}

// SetOnboarding stores the one-time auth code + starting sharecode a user
// pastes in during setup (see /docs/architecture.md).
func (s *Store) SetOnboarding(steamID, authCode, startingShareCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	rec, ok := f.Players[steamID]
	if !ok {
		return fmt.Errorf("no player record for steamid %s — must log in first", steamID)
	}
	rec.AuthCode = authCode
	rec.LatestKnownCode = startingShareCode
	return s.save(f)
}

// GetPlayer returns the stored record for a steamid, or nil if none exists.
func (s *Store) GetPlayer(steamID string) (*PlayerRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return nil, err
	}
	return f.Players[steamID], nil
}

// AppendMatches records newly discovered matches and advances the player's
// latest known sharecode.
func (s *Store) AppendMatches(steamID string, newMatches []Match, newLatestCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	rec, ok := f.Players[steamID]
	if !ok {
		return fmt.Errorf("no player record for steamid %s", steamID)
	}
	now := time.Now()
	for i := range newMatches {
		newMatches[i].DiscoveredAt = now
	}
	rec.Matches = append(rec.Matches, newMatches...)
	rec.LatestKnownCode = newLatestCode
	return s.save(f)
}
