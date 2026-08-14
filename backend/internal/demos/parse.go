package demos

import (
	"fmt"
	"io"

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

	out.TickRate = p.TickRate()
	out.Duration = now()
	return out, nil
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
