package demos

import (
	"fmt"
	"io"
	"sort"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// c4Timer is how long the bomb burns in CS2. Used to derive how close a
// defuse was, since that is more reliably computed from plant and defuse
// times than read out of parser state.
const c4Timer = 40.0

// Parse streams a demo and collects the events the detectors need.
//
// Streaming rather than buffering: demos run to hundreds of megabytes and
// this runs on a VM with a gigabyte of RAM.
func Parse(r io.Reader) (Parsed, error) {
	p := demoinfocs.NewParser(r)
	defer p.Close()

	out := Parsed{}

	var (
		currentRound   int
		roundStartTime float64
		plantedAt      float64
		bombPlanted    bool
		// One clutch per player per round: the situation begins once.
		clutchSeen = map[string]bool{}
		// Last known scoreboard row per player, keyed by steam ID.
		stats = map[string]*PlayerStat{}
	)

	now := func() float64 { return p.CurrentTime().Seconds() }
	tick := func() int { return p.GameState().IngameTick() }

	// v5's Parser interface exposes no Header(), but the map name still
	// arrives as a demo command, so take it from the message directly.
	p.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		out.MapName = m.GetMapName()
	})

	p.RegisterEventHandler(func(events.RoundStart) {
		currentRound++
		roundStartTime = now()
		bombPlanted = false
		plantedAt = 0
		clutchSeen = map[string]bool{}
	})

	p.RegisterEventHandler(func(e events.RoundEnd) {
		if currentRound == 0 {
			return // warmup or a demo that starts mid-round
		}
		out.Rounds = append(out.Rounds, Round{
			Number:     currentRound,
			StartTime:  roundStartTime,
			EndTime:    now(),
			WinnerTeam: int(e.Winner),
		})
		// Snapshot every round rather than only at the end: a player who
		// disconnects before the final round would otherwise vanish from the
		// scoreboard entirely, taking their stats with them.
		snapshotPlayers(p.GameState(), stats, currentRound)
	})

	p.RegisterEventHandler(func(events.BombPlanted) {
		bombPlanted = true
		plantedAt = now()
	})

	p.RegisterEventHandler(func(e events.BombDefused) {
		if e.Player == nil {
			return
		}
		timeLeft := 0.0
		if bombPlanted {
			timeLeft = c4Timer - (now() - plantedAt)
			if timeLeft < 0 {
				timeLeft = 0
			}
		}
		out.Defuses = append(out.Defuses, Defuse{
			Round:         currentRound,
			Tick:          tick(),
			Time:          now(),
			PlayerSteamID: steamID(e.Player),
			PlayerTeam:    int(e.Player.Team),
			TimeLeft:      timeLeft,
		})
	})

	p.RegisterEventHandler(func(e events.Kill) {
		if e.Victim == nil {
			return
		}
		k := Kill{
			Round:         currentRound,
			Tick:          tick(),
			Time:          now(),
			VictimSteamID: steamID(e.Victim),
			VictimTeam:    int(e.Victim.Team),
			IsHeadshot:    e.IsHeadshot,
		}
		if e.Killer != nil {
			k.KillerSteamID = steamID(e.Killer)
			k.KillerTeam = int(e.Killer.Team)
		}
		if e.Weapon != nil {
			k.Weapon = e.Weapon.String()
		}
		out.Kills = append(out.Kills, k)

		// A kill can create a clutch. Checked here rather than derived later
		// because it needs to know who is still alive, which the kill feed
		// alone can't tell you.
		if last, enemies := lastAliveOnTeam(p.GameState()); last != nil && enemies > 0 {
			id := steamID(last)
			if !clutchSeen[id] {
				clutchSeen[id] = true
				out.Clutches = append(out.Clutches, Clutch{
					Round:         currentRound,
					Tick:          tick(),
					Time:          now(),
					PlayerSteamID: id,
					PlayerTeam:    int(last.Team),
					EnemiesAlive:  enemies,
				})
			}
		}
	})

	if err := p.ParseToEnd(); err != nil {
		// Whatever was collected before the failure is still returned: a demo
		// truncated near the end is common, and losing 25 good rounds because
		// the 26th is corrupt would be the wrong trade.
		return out, fmt.Errorf("parsing demo: %w", err)
	}

	// Final snapshot: the last round's stats land after its RoundEnd, and a
	// demo may simply stop without one.
	snapshotPlayers(p.GameState(), stats, currentRound)
	out.Players = finaliseStats(stats, out.Kills)
	out.TeamAScore = p.GameState().TeamTerrorists().Score()
	out.TeamBScore = p.GameState().TeamCounterTerrorists().Score()

	out.TickRate = p.TickRate()
	out.Duration = now()
	return out, nil
}

// snapshotPlayers records each connected player's current scoreboard row,
// overwriting whatever was there. Last write wins, which is what makes a
// disconnect harmless.
func snapshotPlayers(gs demoinfocs.GameState, stats map[string]*PlayerStat, rounds int) {
	for _, pl := range gs.Participants().Playing() {
		if pl == nil {
			continue
		}
		id := steamID(pl)
		if id == "" || id == "0" {
			continue // bots and unresolved entities
		}
		stats[id] = &PlayerStat{
			SteamID: id,
			Name:    pl.Name,
			Team:    int(pl.Team),
			Kills:   pl.Kills(),
			Deaths:  pl.Deaths(),
			Assists: pl.Assists(),
			MVPs:    pl.MVPs(),
			Damage:  pl.TotalDamage(),
			Rounds:  rounds,
		}
	}
}

// finaliseStats folds in headshot counts, which the scoreboard doesn't track
// but the kill feed does.
func finaliseStats(stats map[string]*PlayerStat, kills []Kill) []PlayerStat {
	headshots := map[string]int{}
	for _, k := range kills {
		if k.IsHeadshot && k.KillerSteamID != "" && k.KillerTeam != k.VictimTeam {
			headshots[k.KillerSteamID]++
		}
	}

	out := make([]PlayerStat, 0, len(stats))
	for id, s := range stats {
		s.Headshots = headshots[id]
		out = append(out, *s)
	}
	// Scoreboard order, and stable for players on equal kills so repeated
	// parses of one demo produce identical output.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kills != out[j].Kills {
			return out[i].Kills > out[j].Kills
		}
		return out[i].SteamID < out[j].SteamID
	})
	return out
}

func steamID(p *common.Player) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", p.SteamID64)
}

// lastAliveOnTeam returns the sole survivor of a team, plus how many enemies
// are still alive. It returns nil unless exactly one player on one side is
// left standing.
func lastAliveOnTeam(gs demoinfocs.GameState) (*common.Player, int) {
	var (
		tAlive  []*common.Player
		ctAlive []*common.Player
	)
	for _, pl := range gs.Participants().Playing() {
		if pl == nil || !pl.IsAlive() {
			continue
		}
		switch pl.Team {
		case common.TeamTerrorists:
			tAlive = append(tAlive, pl)
		case common.TeamCounterTerrorists:
			ctAlive = append(ctAlive, pl)
		}
	}

	if len(tAlive) == 1 && len(ctAlive) > 0 {
		return tAlive[0], len(ctAlive)
	}
	if len(ctAlive) == 1 && len(tAlive) > 0 {
		return ctAlive[0], len(tAlive)
	}
	return nil, 0
}
