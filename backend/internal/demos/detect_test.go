package demos

import "testing"

func kill(round int, tick int, at float64, killer, victim string, killerTeam, victimTeam int) Kill {
	return Kill{
		Round: round, Tick: tick, Time: at,
		KillerSteamID: killer, VictimSteamID: victim,
		KillerTeam: killerTeam, VictimTeam: victimTeam,
	}
}

func countKind(hs []Highlight, kind string) int {
	n := 0
	for _, h := range hs {
		if h.Kind == kind {
			n++
		}
	}
	return n
}

func TestMultiKillNeedsThreeKills(t *testing.T) {
	p := Parsed{Kills: []Kill{
		kill(1, 100, 10, "attacker", "v1", 2, 3),
		kill(1, 200, 12, "attacker", "v2", 2, 3),
		// Two kills is not a highlight.
		kill(2, 300, 30, "other", "v3", 3, 2),
		kill(2, 400, 32, "other", "v4", 3, 2),
		kill(2, 500, 34, "other", "v5", 3, 2),
	}}

	hs := Detect(p)
	if got := countKind(hs, KindMultiKill); got != 1 {
		t.Fatalf("multi-kills = %d, want 1", got)
	}
	for _, h := range hs {
		if h.Kind == KindMultiKill && h.SteamID != "other" {
			t.Errorf("multi-kill attributed to %s", h.SteamID)
		}
	}
}

func TestMultiKillIgnoresTeamKillsAndSuicides(t *testing.T) {
	p := Parsed{Kills: []Kill{
		kill(1, 100, 10, "p", "t1", 2, 2), // team kill
		kill(1, 200, 11, "p", "t2", 2, 2), // team kill
		kill(1, 300, 12, "p", "p", 2, 2),  // suicide
		kill(1, 400, 13, "p", "e1", 2, 3), // the only real kill
	}}

	if got := countKind(Detect(p), KindMultiKill); got != 0 {
		t.Fatalf("multi-kills = %d, want 0 — team kills and suicides must not count", got)
	}
}

func TestMultiKillWindowSpansFirstToLastKill(t *testing.T) {
	p := Parsed{Kills: []Kill{
		kill(1, 100, 20, "p", "v1", 2, 3),
		kill(1, 200, 25, "p", "v2", 2, 3),
		kill(1, 300, 30, "p", "v3", 2, 3),
	}}

	hs := Detect(p)
	var h Highlight
	for _, c := range hs {
		if c.Kind == KindMultiKill {
			h = c
		}
	}
	if h.StartS != 20-preRollSeconds {
		t.Errorf("start = %v, want %v", h.StartS, 20-preRollSeconds)
	}
	if h.EndS != 30+postRollSeconds {
		t.Errorf("end = %v, want %v", h.EndS, 30+postRollSeconds)
	}
	if h.StartTick != 100 || h.EndTick != 300 {
		t.Errorf("ticks = %d..%d, want 100..300", h.StartTick, h.EndTick)
	}
}

func TestClipWindowNeverStartsBeforeZero(t *testing.T) {
	p := Parsed{Kills: []Kill{
		kill(1, 10, 1, "p", "v1", 2, 3),
		kill(1, 20, 2, "p", "v2", 2, 3),
		kill(1, 30, 3, "p", "v3", 2, 3),
	}}

	for _, h := range Detect(p) {
		if h.StartS < 0 {
			t.Fatalf("%s starts at %v, before the demo began", h.Kind, h.StartS)
		}
	}
}

func TestClutchOnlyCountsWhenTheRoundIsWon(t *testing.T) {
	p := Parsed{
		Rounds: []Round{
			{Number: 1, EndTime: 60, WinnerTeam: 3},
			{Number: 2, EndTime: 120, WinnerTeam: 2},
		},
		Clutches: []Clutch{
			{Round: 1, Tick: 100, Time: 40, PlayerSteamID: "hero", PlayerTeam: 3, EnemiesAlive: 3},
			// Same situation, but their team lost the round.
			{Round: 2, Tick: 200, Time: 100, PlayerSteamID: "hero", PlayerTeam: 3, EnemiesAlive: 2},
		},
	}

	hs := Detect(p)
	if got := countKind(hs, KindClutch); got != 1 {
		t.Fatalf("clutches = %d, want 1 — surviving a lost round is not a clutch", got)
	}
	for _, h := range hs {
		if h.Kind == KindClutch && h.Round != 1 {
			t.Errorf("clutch found in round %d, want round 1", h.Round)
		}
	}
}

func TestClutchScoresWithEnemiesAlive(t *testing.T) {
	p := Parsed{
		Rounds: []Round{{Number: 1, EndTime: 60, WinnerTeam: 3}, {Number: 2, EndTime: 120, WinnerTeam: 3}},
		Clutches: []Clutch{
			{Round: 1, Time: 10, PlayerSteamID: "a", PlayerTeam: 3, EnemiesAlive: 1},
			{Round: 2, Time: 70, PlayerSteamID: "a", PlayerTeam: 3, EnemiesAlive: 4},
		},
	}

	var oneVOne, oneVFour float64
	for _, h := range Detect(p) {
		if h.Kind != KindClutch {
			continue
		}
		if h.Round == 1 {
			oneVOne = h.Score
		} else {
			oneVFour = h.Score
		}
	}
	if !(oneVFour > oneVOne) {
		t.Errorf("1v4 scored %v, 1v1 scored %v — the harder clutch must rank higher", oneVFour, oneVOne)
	}
}

func TestClutchRunsToTheEndOfTheRound(t *testing.T) {
	p := Parsed{
		Rounds:   []Round{{Number: 1, EndTime: 95, WinnerTeam: 3}},
		Clutches: []Clutch{{Round: 1, Tick: 500, Time: 40, PlayerSteamID: "hero", PlayerTeam: 3, EnemiesAlive: 2}},
		Kills:    []Kill{kill(1, 900, 80, "hero", "e1", 3, 2)},
	}

	for _, h := range Detect(p) {
		if h.Kind != KindClutch {
			continue
		}
		// The round ends after the last kill; cutting at the kill would lose
		// the defuse or the timer running out.
		if h.EndS != 95+postRollSeconds {
			t.Errorf("clutch ends at %v, want %v", h.EndS, 95+postRollSeconds)
		}
		if h.EndTick != 900 {
			t.Errorf("clutch end tick = %d, want 900", h.EndTick)
		}
	}
}

func TestOpeningDuelIsTheEarliestKillOfTheRound(t *testing.T) {
	p := Parsed{Kills: []Kill{
		// Deliberately out of chronological order.
		kill(1, 300, 30, "late", "v2", 2, 3),
		kill(1, 100, 10, "first", "v1", 2, 3),
		kill(1, 200, 20, "middle", "v3", 2, 3),
	}}

	found := false
	for _, h := range Detect(p) {
		if h.Kind != KindOpeningDuel {
			continue
		}
		found = true
		if h.SteamID != "first" {
			t.Errorf("opening duel credited to %s, want first", h.SteamID)
		}
	}
	if !found {
		t.Fatal("no opening duel detected")
	}
}

func TestDefuseScoresHigherWhenTight(t *testing.T) {
	p := Parsed{Defuses: []Defuse{
		{Round: 1, Time: 50, PlayerSteamID: "a", TimeLeft: 8},
		{Round: 2, Time: 150, PlayerSteamID: "a", TimeLeft: 0.4},
	}}

	var relaxed, tight float64
	for _, h := range Detect(p) {
		if h.Kind != KindDefuse {
			continue
		}
		if h.Round == 1 {
			relaxed = h.Score
		} else {
			tight = h.Score
		}
	}
	if !(tight > relaxed) {
		t.Errorf("0.4s defuse scored %v, 8s defuse scored %v — the tight one must rank higher", tight, relaxed)
	}
}

func TestDetectIsChronological(t *testing.T) {
	p := Parsed{
		Rounds: []Round{{Number: 1, EndTime: 60, WinnerTeam: 3}},
		Kills: []Kill{
			kill(3, 900, 300, "p", "v1", 2, 3),
			kill(1, 100, 10, "p", "v2", 2, 3),
			kill(2, 500, 150, "p", "v3", 2, 3),
		},
		Defuses: []Defuse{{Round: 1, Time: 55, PlayerSteamID: "d", TimeLeft: 1}},
	}

	hs := Detect(p)
	for i := 1; i < len(hs); i++ {
		if hs[i-1].Round > hs[i].Round {
			t.Fatalf("highlights out of order: round %d before round %d", hs[i-1].Round, hs[i].Round)
		}
		if hs[i-1].Round == hs[i].Round && hs[i-1].StartS > hs[i].StartS {
			t.Fatalf("within round %d: %v before %v", hs[i].Round, hs[i-1].StartS, hs[i].StartS)
		}
	}
}

func TestEmptyDemoProducesNothing(t *testing.T) {
	if hs := Detect(Parsed{}); len(hs) != 0 {
		t.Fatalf("empty demo produced %d highlights", len(hs))
	}
}
