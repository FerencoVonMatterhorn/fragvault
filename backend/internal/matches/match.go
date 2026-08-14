package matches

import "time"

// Match is a discovered CS2 match, identified by its sharecode. It carries
// only what a sharecode decodes to — no map, score or date, because none of
// that is in the code itself; it takes a Game Coordinator lookup or the demo.
type Match struct {
	ShareCode     string    `json:"share_code"`
	MatchID       uint64    `json:"match_id"`
	ReservationID uint64    `json:"reservation_id"`
	TVPort        uint16    `json:"tv_port"`
	DiscoveredAt  time.Time `json:"discovered_at"`
}

// PlayerRecord is everything stored for one onboarded Steam user.
//
// The JSON tags are still load-bearing: they are the on-disk shape of the
// Phase 1 file store, which db.ImportLegacyJSON reads once when migrating a
// deployment onto Postgres. Changing them breaks that import.
type PlayerRecord struct {
	SteamID         string  `json:"steam_id"`
	PersonaName     string  `json:"persona_name"`
	AvatarURL       string  `json:"avatar_url"`
	AuthCode        string  `json:"auth_code"`         // Valve "game authentication code", user-supplied once
	LatestKnownCode string  `json:"latest_known_code"` // most recent sharecode we've walked to
	Matches         []Match `json:"matches"`           // newest last
}
