package demos

import "testing"

func TestADR(t *testing.T) {
	cases := []struct {
		name string
		stat PlayerStat
		want float64
	}{
		{"typical", PlayerStat{Damage: 2400, Rounds: 24}, 100},
		{"rounds down", PlayerStat{Damage: 2401, Rounds: 24}, 100.04166666666667},
		{"no damage", PlayerStat{Damage: 0, Rounds: 24}, 0},
		// A match cancelled before a round completed is real, and must not
		// divide by zero.
		{"no rounds", PlayerStat{Damage: 300, Rounds: 0}, 0},
		{"negative rounds", PlayerStat{Damage: 300, Rounds: -1}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.stat.ADR(); got != c.want {
				t.Errorf("ADR() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestHeadshotPct(t *testing.T) {
	cases := []struct {
		name string
		stat PlayerStat
		want float64
	}{
		{"half", PlayerStat{Kills: 20, Headshots: 10}, 50},
		{"all", PlayerStat{Kills: 7, Headshots: 7}, 100},
		{"none", PlayerStat{Kills: 7, Headshots: 0}, 0},
		// A player with no kills has no headshot percentage, not a crash.
		{"no kills", PlayerStat{Kills: 0, Headshots: 0}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.stat.HeadshotPct(); got != c.want {
				t.Errorf("HeadshotPct() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFinaliseStatsCountsHeadshotsFromTheKillFeed(t *testing.T) {
	stats := map[string]*PlayerStat{
		"a": {SteamID: "a", Kills: 3},
		"b": {SteamID: "b", Kills: 1},
	}
	kills := []Kill{
		{KillerSteamID: "a", VictimSteamID: "x", KillerTeam: 2, VictimTeam: 3, IsHeadshot: true},
		{KillerSteamID: "a", VictimSteamID: "y", KillerTeam: 2, VictimTeam: 3, IsHeadshot: false},
		{KillerSteamID: "a", VictimSteamID: "z", KillerTeam: 2, VictimTeam: 3, IsHeadshot: true},
		// Team kill: counted by neither the scoreboard nor this.
		{KillerSteamID: "b", VictimSteamID: "t", KillerTeam: 3, VictimTeam: 3, IsHeadshot: true},
	}

	out := finaliseStats(stats, kills)
	byID := map[string]PlayerStat{}
	for _, s := range out {
		byID[s.SteamID] = s
	}

	if byID["a"].Headshots != 2 {
		t.Errorf("a headshots = %d, want 2", byID["a"].Headshots)
	}
	if byID["b"].Headshots != 0 {
		t.Errorf("b headshots = %d, want 0 — team kills must not count", byID["b"].Headshots)
	}
}

func TestFinaliseStatsSortsByKills(t *testing.T) {
	stats := map[string]*PlayerStat{
		"low":  {SteamID: "low", Kills: 4},
		"high": {SteamID: "high", Kills: 25},
		"mid":  {SteamID: "mid", Kills: 12},
	}

	out := finaliseStats(stats, nil)
	if len(out) != 3 {
		t.Fatalf("got %d rows, want 3", len(out))
	}
	if out[0].SteamID != "high" || out[2].SteamID != "low" {
		t.Errorf("order = %s, %s, %s — want high, mid, low", out[0].SteamID, out[1].SteamID, out[2].SteamID)
	}
}

func TestFinaliseStatsIsStableForEqualKills(t *testing.T) {
	// Map iteration order is random, so without a tiebreaker two parses of
	// the same demo could produce different scoreboards.
	stats := map[string]*PlayerStat{
		"p3": {SteamID: "p3", Kills: 10},
		"p1": {SteamID: "p1", Kills: 10},
		"p2": {SteamID: "p2", Kills: 10},
	}

	first := finaliseStats(stats, nil)
	for i := 0; i < 20; i++ {
		again := finaliseStats(stats, nil)
		for j := range first {
			if first[j].SteamID != again[j].SteamID {
				t.Fatalf("ordering is not stable: %s then %s at position %d", first[j].SteamID, again[j].SteamID, j)
			}
		}
	}
}
