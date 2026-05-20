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
