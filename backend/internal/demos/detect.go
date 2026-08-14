package demos

import "sort"

// Detect turns a parsed demo into highlights.
//
// Every detector here is a pure function of Parsed, which is what makes them
// testable. Scores are deliberately crude — they only need to rank moments
// against each other, not mean anything absolute.
func Detect(p Parsed) []Highlight {
	var out []Highlight
	out = append(out, detectMultiKills(p)...)
	out = append(out, detectClutches(p)...)
	out = append(out, detectOpeningDuels(p)...)
	out = append(out, detectDefuses(p)...)

	// Stable, chronological order so the UI and any later clip pipeline see
	// the same sequence every time the same demo is parsed.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Round != out[j].Round {
			return out[i].Round < out[j].Round
		}
		if out[i].StartS != out[j].StartS {
			return out[i].StartS < out[j].StartS
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// window applies clip padding, clamped so a moment in the opening seconds of
// a demo doesn't produce a negative start.
func window(startS, endS float64) (float64, float64) {
	s := startS - preRollSeconds
	if s < 0 {
		s = 0
	}
	return s, endS + postRollSeconds
}

// detectMultiKills finds rounds where one player killed three or more.
//
// Counting per (round, killer) rather than by time gap: a 3K spread over a
// long round is still a 3K, and the clip window covers first to last kill.
func detectMultiKills(p Parsed) []Highlight {
	type key struct {
		round  int
		player string
	}
	grouped := map[key][]Kill{}
	order := []key{}

	for _, k := range p.Kills {
		if k.KillerSteamID == "" || k.KillerSteamID == k.VictimSteamID {
			continue // suicides and world deaths aren't anyone's highlight
		}
		if k.KillerTeam == k.VictimTeam {
			continue // team kills are not a highlight either
		}
		kk := key{round: k.Round, player: k.KillerSteamID}
		if _, seen := grouped[kk]; !seen {
			order = append(order, kk)
		}
		grouped[kk] = append(grouped[kk], k)
	}

	var out []Highlight
	for _, kk := range order {
		kills := grouped[kk]
		if len(kills) < 3 {
			continue
		}
		first, last := kills[0], kills[len(kills)-1]
		headshots := 0
		for _, k := range kills {
			if k.IsHeadshot {
				headshots++
			}
		}
		start, end := window(first.Time, last.Time)
		out = append(out, Highlight{
			SteamID:   kk.player,
			Kind:      KindMultiKill,
			Round:     kk.round,
			StartTick: first.Tick,
			EndTick:   last.Tick,
			StartS:    start,
			EndS:      end,
			// An ace outranks a 4K outranks a 3K; headshots break ties.
			Score: float64(len(kills)) + float64(headshots)*0.1,
			Metadata: map[string]any{
				"kills":     len(kills),
				"headshots": headshots,
			},
		})
	}
	return out
}

// detectClutches finds last-player-alive situations that were actually won.
//
// Surviving a lost round isn't a clutch, so this needs the round result and
// silently drops situations from rounds with no recorded outcome.
func detectClutches(p Parsed) []Highlight {
	winners := map[int]int{}
	for _, r := range p.Rounds {
		winners[r.Number] = r.WinnerTeam
	}

	var out []Highlight
	for _, c := range p.Clutches {
		if c.EnemiesAlive < 1 {
			continue
		}
		winner, ok := winners[c.Round]
		if !ok || winner != c.PlayerTeam {
			continue
		}

		// Runs to the end of the round: the tension is the whole point, and
		// cutting at the last kill would drop the defuse or the timer.
		endTime := c.Time
		endTick := c.Tick
		for _, r := range p.Rounds {
			if r.Number == c.Round && r.EndTime > endTime {
				endTime = r.EndTime
			}
		}
		for _, k := range p.Kills {
			if k.Round == c.Round && k.Tick > endTick {
				endTick = k.Tick
			}
		}

		start, end := window(c.Time, endTime)
		out = append(out, Highlight{
			SteamID:   c.PlayerSteamID,
			Kind:      KindClutch,
			Round:     c.Round,
			StartTick: c.Tick,
			EndTick:   endTick,
			StartS:    start,
			EndS:      end,
			// A 1v4 is worth more than a 1v1, and any clutch outranks a 3K.
			Score: 4 + float64(c.EnemiesAlive),
			Metadata: map[string]any{
				"enemies_alive": c.EnemiesAlive,
			},
		})
	}
	return out
}

// detectOpeningDuels finds the first kill of each round.
//
// Low value on its own — it's included because opening picks decide rounds
// and are cheap to surface, not because every one is worth watching.
func detectOpeningDuels(p Parsed) []Highlight {
	firstByRound := map[int]Kill{}
	rounds := []int{}
	for _, k := range p.Kills {
		if k.KillerSteamID == "" || k.KillerTeam == k.VictimTeam {
			continue
		}
		existing, seen := firstByRound[k.Round]
		if !seen {
			firstByRound[k.Round] = k
			rounds = append(rounds, k.Round)
			continue
		}
		if k.Time < existing.Time {
			firstByRound[k.Round] = k
		}
	}

	var out []Highlight
	for _, round := range rounds {
		k := firstByRound[round]
		start, end := window(k.Time, k.Time)
		out = append(out, Highlight{
			SteamID:   k.KillerSteamID,
			Kind:      KindOpeningDuel,
			Round:     round,
			StartTick: k.Tick,
			EndTick:   k.Tick,
			StartS:    start,
			EndS:      end,
			Score:     1,
			Metadata: map[string]any{
				"weapon":   k.Weapon,
				"headshot": k.IsHeadshot,
			},
		})
	}
	return out
}

// detectDefuses surfaces bomb defusals, scoring the tight ones higher.
func detectDefuses(p Parsed) []Highlight {
	var out []Highlight
	for _, d := range p.Defuses {
		score := 2.0
		// Under two seconds is the difference between a defuse and a story.
		if d.TimeLeft > 0 && d.TimeLeft < 2 {
			score = 5
		}
		start, end := window(d.Time, d.Time)
		out = append(out, Highlight{
			SteamID:   d.PlayerSteamID,
			Kind:      KindDefuse,
			Round:     d.Round,
			StartTick: d.Tick,
			EndTick:   d.Tick,
			StartS:    start,
			EndS:      end,
			Score:     score,
			Metadata: map[string]any{
				"time_left": d.TimeLeft,
			},
		})
	}
	return out
}
