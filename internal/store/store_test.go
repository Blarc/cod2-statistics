package store_test

import (
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/store"
	"path/filepath"
	"testing"
)

func TestSaveMatchUpdatesWindowAndMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	first := &model.Match{
		ID:        "match-1",
		MapName:   "",
		GameType:  "",
		StartedAt: 100,
		EndedAt:   110,
		Players: map[string]*model.PlayerStats{
			"alpha": {NormalizedName: "alpha", EventCount: 1},
		},
	}
	if err := st.SaveMatch(first); err != nil {
		t.Fatalf("SaveMatch first: %v", err)
	}

	second := &model.Match{
		ID:        "match-1",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 120,
		EndedAt:   150,
		Players: map[string]*model.PlayerStats{
			"alpha": {NormalizedName: "alpha", EventCount: 1},
		},
	}
	if err := st.SaveMatch(second); err != nil {
		t.Fatalf("SaveMatch second: %v", err)
	}

	got, err := st.GetMatch("match-1")
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}
	if got.StartedAt != 100 {
		t.Errorf("StartedAt = %d, want 100", got.StartedAt)
	}
	if got.EndedAt != 150 {
		t.Errorf("EndedAt = %d, want 150", got.EndedAt)
	}
	if got.MapName != "mp_toujane" {
		t.Errorf("MapName = %q, want mp_toujane", got.MapName)
	}
	if got.GameType != "dm" {
		t.Errorf("GameType = %q, want dm", got.GameType)
	}
}

func TestOpenMatchStateRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	want := &store.OpenMatch{
		MatchID:   "match-open",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 100,
		LastClock: 130,
	}
	if err := st.SetOpenMatch(want); err != nil {
		t.Fatalf("SetOpenMatch: %v", err)
	}

	got, err := st.GetOpenMatch()
	if err != nil {
		t.Fatalf("GetOpenMatch: %v", err)
	}
	if got == nil {
		t.Fatal("GetOpenMatch returned nil")
	}
	if *got != *want {
		t.Fatalf("GetOpenMatch = %#v, want %#v", got, want)
	}

	if err := st.SetOpenMatch(nil); err != nil {
		t.Fatalf("SetOpenMatch(nil): %v", err)
	}
	cleared, err := st.GetOpenMatch()
	if err != nil {
		t.Fatalf("GetOpenMatch after clear: %v", err)
	}
	if cleared != nil {
		t.Fatalf("GetOpenMatch after clear = %#v, want nil", cleared)
	}
}

func TestSaveMatchDuplicateEventsDoNotDoubleCountStats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dedupe.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	m := &model.Match{
		ID:        "match-dedupe",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 100,
		EndedAt:   120,
		Players: map[string]*model.PlayerStats{
			"alpha": {NormalizedName: "alpha"},
			"beta":  {NormalizedName: "beta"},
		},
		KillEvents: []*model.KDEvent{
			{
				ClockSec:       110,
				VictimNameNorm: "alpha",
				KillerNameNorm: "beta",
				Weapon:         "kar98k_mp",
				Damage:         100,
				Mod:            "MOD_HEAD_SHOT",
				HitLoc:         "head",
				IdempotencyKey: "k1",
			},
		},
		DamageEvents: []*model.KDEvent{
			{
				ClockSec:       111,
				VictimNameNorm: "alpha",
				KillerNameNorm: "beta",
				Weapon:         "kar98k_mp",
				Damage:         30,
				Mod:            "MOD_RIFLE_BULLET",
				HitLoc:         "torso_lower",
				IdempotencyKey: "d1",
			},
		},
		WeaponEvents: []*model.WeaponEvent{
			{
				ClockSec:       112,
				PlayerNameNorm: "beta",
				Weapon:         "kar98k_mp",
				IdempotencyKey: "w1",
			},
		},
	}

	if err := st.SaveMatch(m); err != nil {
		t.Fatalf("SaveMatch first: %v", err)
	}
	if err := st.SaveMatch(m); err != nil {
		t.Fatalf("SaveMatch second: %v", err)
	}

	detail, err := st.GetMatch("match-dedupe")
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}

	var alpha, beta *store.PlayerMatchStat
	for i := range detail.Players {
		p := &detail.Players[i]
		if p.Name == "alpha" {
			alpha = p
		}
		if p.Name == "beta" {
			beta = p
		}
	}
	if alpha == nil || beta == nil {
		t.Fatalf("expected players alpha and beta, got %#v", detail.Players)
	}

	if alpha.Deaths != 1 || alpha.DamageTaken != 30 {
		t.Fatalf("alpha stats changed by duplicate overlap: deaths=%d damage_taken=%d", alpha.Deaths, alpha.DamageTaken)
	}
	if beta.Kills != 1 || beta.DamageDealt != 30 || beta.Headshots != 1 {
		t.Fatalf("beta stats changed by duplicate overlap: kills=%d damage_dealt=%d hs=%d", beta.Kills, beta.DamageDealt, beta.Headshots)
	}
	if beta.WeaponKills["kar98k_mp"] != 1 {
		t.Fatalf("beta weapon kills = %d, want 1", beta.WeaponKills["kar98k_mp"])
	}

	_, killCount, err := st.ListKillEvents("match-dedupe", "", "", 10, 0)
	if err != nil {
		t.Fatalf("ListKillEvents: %v", err)
	}
	if killCount != 1 {
		t.Fatalf("kill events = %d, want 1", killCount)
	}
}
