package matcher_test

import (
	"bufio"
	"cod2-statistics/internal/matcher"
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"os"
	"strings"
	"testing"
)

func parseRawLines(t *testing.T, input string) []*model.RawLine {
	t.Helper()
	var out []*model.RawLine
	for _, line := range strings.Split(input, "\n") {
		if rl, ok := parser.ParseLine(line); ok {
			out = append(out, rl)
		}
	}
	return out
}

func TestSplitByInitGame(t *testing.T) {
	input := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 K;0;0;;alpha;0;1;;beta;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower
12900:00 InitGame: \g_gametype\dm\mapname\mp_decoy\protocol\118
12900:10 K;0;0;;gamma;0;1;;delta;m1garand_mp;90;MOD_RIFLE_BULLET;torso_lower`

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("got %d matches, want 2", len(matches))
	}
	if len(matches) > 0 && matches[0].MapName != "mp_toujane" {
		t.Errorf("match[0].MapName = %q, want mp_toujane", matches[0].MapName)
	}
	if len(matches) > 1 && matches[1].MapName != "mp_decoy" {
		t.Errorf("match[1].MapName = %q, want mp_decoy", matches[1].MapName)
	}
}

func TestSplitByShutdownGame(t *testing.T) {
	input := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 K;0;0;;alpha;0;1;;beta;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower
12900:00 ShutdownGame:`

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("got %d matches, want 1", len(matches))
	}
	if len(matches) > 0 && matches[0].EndedAt != 12900*60 {
		t.Errorf("EndedAt = %d, want %d", matches[0].EndedAt, 12900*60)
	}
}

func TestClockReset(t *testing.T) {
	// Simulate a wall-clock-sorted Loki stream where the server restarted:
	// the old session (high game clocks) happened first in wall-time, then
	// the server restarted and a new session started at low game clocks.
	// Lines are already in wall-clock order — do NOT sort by game clock or
	// the drop becomes invisible.
	input := `12900:00 K;0;0;;alpha;0;1;;beta;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower
12900:10 K;0;0;;gamma;0;1;;delta;m1garand_mp;90;MOD_RIFLE_BULLET;torso_lower
100:00 K;0;0;;echo;0;1;;foxtrot;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower`

	rls := parseRawLines(t, input)
	// Intentionally NOT calling SortOldestFirst — this test exercises the
	// wall-clock-ordered Loki path where the game-clock drop signals a restart.
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("got %d matches, want 2", len(matches))
	}
}

func TestContinuationKeepsSameMatchAcrossPolls(t *testing.T) {
	firstPoll := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 K;0;0;;alpha;0;1;;beta;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower`

	rls := parseRawLines(t, firstPoll)
	matcher.SortOldestFirst(rls)
	initial, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines first poll: %v", err)
	}
	if len(initial) != 1 {
		t.Fatalf("first poll matches = %d, want 1", len(initial))
	}

	cont := &matcher.Continuation{
		MatchID:   initial[0].ID,
		MapName:   initial[0].MapName,
		GameType:  initial[0].GameType,
		StartedAt: initial[0].StartedAt,
		LastClock: initial[0].EndedAt,
	}

	secondPoll := `12898:40 K;0;0;;gamma;0;1;;delta;m1garand_mp;90;MOD_RIFLE_BULLET;torso_lower`
	rls2 := parseRawLines(t, secondPoll)
	got, err := matcher.ProcessLinesWithContinuation(rls2, cont)
	if err != nil {
		t.Fatalf("ProcessLinesWithContinuation second poll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("second poll matches = %d, want 1", len(got))
	}
	if got[0].ID != cont.MatchID {
		t.Errorf("continued match ID = %q, want %q", got[0].ID, cont.MatchID)
	}
	if got[0].MapName != "mp_toujane" {
		t.Errorf("continued map = %q, want mp_toujane", got[0].MapName)
	}
}

func TestContinuationClockDropStartsNewMatch(t *testing.T) {
	cont := &matcher.Continuation{
		MatchID:   "existing-match",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 12898 * 60,
		LastClock: 12900 * 60,
	}

	secondPoll := `100:00 K;0;0;;gamma;0;1;;delta;m1garand_mp;90;MOD_RIFLE_BULLET;torso_lower`
	rls := parseRawLines(t, secondPoll)
	got, err := matcher.ProcessLinesWithContinuation(rls, cont)
	if err != nil {
		t.Fatalf("ProcessLinesWithContinuation: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("matches = %d, want 1", len(got))
	}
	if got[0].ID == cont.MatchID {
		t.Fatalf("clock reset should start a new match; got same ID %q", got[0].ID)
	}
}

func TestStateClearedOnShutdownGame(t *testing.T) {
	cont := &matcher.Continuation{
		MatchID:   "existing-match",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 12898 * 60,
		LastClock: 12900 * 60,
	}

	rls := parseRawLines(t, `12900:10 ShutdownGame:`)
	got, next, err := matcher.ProcessLinesWithState(rls, cont)
	if err != nil {
		t.Fatalf("ProcessLinesWithState: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("matches = %d, want 0 (seed-only shutdown)", len(got))
	}
	if next != nil {
		t.Fatalf("next continuation = %#v, want nil", next)
	}
}

func TestStatePreservedWhenNoNewLines(t *testing.T) {
	cont := &matcher.Continuation{
		MatchID:   "existing-match",
		MapName:   "mp_toujane",
		GameType:  "dm",
		StartedAt: 12898 * 60,
		LastClock: 12900 * 60,
	}

	got, next, err := matcher.ProcessLinesWithState(nil, cont)
	if err != nil {
		t.Fatalf("ProcessLinesWithState: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("matches = %d, want 0", len(got))
	}
	if next == nil {
		t.Fatal("next continuation is nil, want existing state")
	}
	if next.MatchID != cont.MatchID || next.LastClock != cont.LastClock {
		t.Fatalf("next continuation = %#v, want match_id=%q last_clock=%d", next, cont.MatchID, cont.LastClock)
	}
}

func TestMalformedLineSkipped(t *testing.T) {
	input := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 K;0;0;;onlyFiveFields`

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if len(matches[0].KillEvents) != 0 {
		t.Errorf("kill events = %d, want 0 for malformed line", len(matches[0].KillEvents))
	}
}

func TestDuplicateEventSuppression(t *testing.T) {
	killLine := "12898:30 K;0;0;;alpha;0;1;;beta;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower"
	input := "12898:14 InitGame: \\g_gametype\\dm\\mapname\\mp_toujane\\protocol\\118\n" +
		killLine + "\n" + killLine

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if len(matches[0].KillEvents) != 1 {
		t.Errorf("kill events = %d, want 1 (duplicate suppressed)", len(matches[0].KillEvents))
	}
	beta := matches[0].Players["beta"]
	if beta == nil {
		t.Fatal("player 'beta' not found")
	}
	if beta.Kills != 1 {
		t.Errorf("beta.Kills = %d, want 1", beta.Kills)
	}
}

func TestHeadshotCounting(t *testing.T) {
	input := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 K;0;0;;alpha;0;1;;beta;kar98k_mp;270;MOD_HEAD_SHOT;head
12898:35 K;0;0;;alpha;0;1;;beta;m1garand_mp;90;MOD_RIFLE_BULLET;torso_lower`

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, _ := matcher.ProcessLines(rls)
	if len(matches) == 0 {
		t.Fatal("no matches")
	}
	beta := matches[0].Players["beta"]
	if beta == nil {
		t.Fatal("player 'beta' not found")
	}
	if beta.Kills != 2 {
		t.Errorf("beta.Kills = %d, want 2", beta.Kills)
	}
	if beta.Headshots != 1 {
		t.Errorf("beta.Headshots = %d, want 1", beta.Headshots)
	}
}

func TestWorldDamageNoAttackerEntry(t *testing.T) {
	input := `12898:14 InitGame: \g_gametype\dm\mapname\mp_toujane\protocol\118
12898:30 D;0;4;allies;v hrbet;;-1;world;;none;4;MOD_FALLING;none`

	rls := parseRawLines(t, input)
	matcher.SortOldestFirst(rls)
	matches, _ := matcher.ProcessLines(rls)
	if len(matches) == 0 {
		t.Fatal("no matches")
	}
	if _, ok := matches[0].Players[""]; ok {
		t.Error("empty-name player should not be created for world damage")
	}
	if _, ok := matches[0].Players["world"]; ok {
		t.Error("'world' should not be created as a player")
	}
}

func TestFixtureLog(t *testing.T) {
	f, err := os.Open("../../testdata/log.txt")
	if err != nil {
		t.Fatalf("open testdata/log.txt: %v", err)
	}
	defer f.Close()

	var rls []*model.RawLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rl, ok := parser.ParseLine(sc.Text()); ok {
			rls = append(rls, rl)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	matcher.SortOldestFirst(rls)
	matches, err := matcher.ProcessLines(rls)
	if err != nil {
		t.Fatalf("ProcessLines error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no matches parsed from fixture")
	}

	m := matches[0]
	if m.MapName != "mp_toujane" {
		t.Errorf("MapName = %q, want mp_toujane", m.MapName)
	}

	for _, name := range []string{"gghunt", "puci", "v hrbet", "s4ywoot"} {
		p, ok := m.Players[name]
		if !ok {
			t.Errorf("player %q not found in match", name)
			continue
		}
		if p.Kills == 0 && p.Deaths == 0 {
			t.Errorf("player %q has zero kills and deaths", name)
		}
	}

	gghunt := m.Players["gghunt"]
	if gghunt == nil {
		t.Fatal("gghunt not found")
	}
	if gghunt.Kills <= 0 {
		t.Errorf("gghunt.Kills = %d, want > 0", gghunt.Kills)
	}
}
