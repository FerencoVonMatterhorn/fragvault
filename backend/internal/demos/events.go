// Package demos downloads CS2 demo files, parses them, and turns what
// happened into highlights worth watching.
//
// The parser is deliberately separated from the detectors: parsing produces
// plain normalized events (Parsed), and every detector is a pure function
// over those. That means the interesting logic — what counts as a clutch —
// is unit-testable without a several-hundred-megabyte demo fixture.
package demos

// ParserVersion is the cache key for analysis results. demo_analyses is
// UNIQUE (share_code, parser_version), so bumping this is how a demo gets
// re-parsed after the detectors change. Bump it whenever a change would
// produce different highlights for the same demo.
const ParserVersion = 1

// Highlight kinds. Stored as text so adding one is code, not a migration.
const (
	KindMultiKill   = "multi_kill"
	KindClutch      = "clutch"
	KindOpeningDuel = "opening_duel"
	KindDefuse      = "defuse"
)

// Clip padding around a detected moment. A highlight that starts exactly on
// the first shot has no context and reads as a jump cut, so the window opens
// before the action and closes shortly after it.
const (
	preRollSeconds  = 6.0
	postRollSeconds = 3.0
)

// Kill is one kill, normalized out of the demo.
type Kill struct {
	Round         int
	Tick          int
	Time          float64 // seconds from the start of the demo
	KillerSteamID string
	VictimSteamID string
	KillerTeam    int
	VictimTeam    int
	IsHeadshot    bool
	Weapon        string
}

// Round records how a round ended, so detectors can ask whether the player
// actually won the situation they were in.
type Round struct {
	Number     int
	StartTime  float64
	EndTime    float64
	WinnerTeam int
}

// Defuse is a completed bomb defusal.
type Defuse struct {
	Round         int
	Tick          int
	Time          float64
	PlayerSteamID string
	PlayerTeam    int
	// Seconds left on the bomb timer. Small values are the interesting ones.
	TimeLeft float64
}

// Clutch marks the moment a player became the last one alive on their team.
// It is captured during parsing rather than derived afterwards because
// deriving it from kills alone would miss players who neither killed nor
// died, and would need a roster the kill feed doesn't carry.
type Clutch struct {
	Round         int
	Tick          int
	Time          float64
	PlayerSteamID string
	PlayerTeam    int
	EnemiesAlive  int
}

// Parsed is everything a single demo yielded.
type Parsed struct {
	MapName  string
	TickRate float64
	Duration float64 // seconds
	Kills    []Kill
	Rounds   []Round
	Defuses  []Defuse
	Clutches []Clutch
}

// Highlight is one moment worth watching, ready to be persisted.
type Highlight struct {
	SteamID   string
	Kind      string
	Round     int
	StartTick int
	EndTick   int
	StartS    float64
	EndS      float64
	Score     float64
	Metadata  map[string]any
}
