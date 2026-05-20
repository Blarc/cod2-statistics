package parser_test

import (
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"testing"
)

func TestStripColorCodes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gg^3^4hu^1nt", "gghunt"},
		{"v hrbet", "v hrbet"},
		{"", ""},
		{"^0^1^2^3^4^5^6^7^8^9", ""},
		{"no codes", "no codes"},
		{"^9red^0", "red"},
	}
	for _, c := range cases {
		got := parser.StripColorCodes(c.in)
		if got != c.want {
			t.Errorf("StripColorCodes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseClock(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"12912:15", 12912*60 + 15, false},
		{"0:00", 0, false},
		{"1:30", 90, false},
		{"999:59", 999*60 + 59, false},
		{"bad", 0, true},
		{"12:60", 0, true},
		{"", 0, true},
		{":30", 0, true},
	}
	for _, c := range cases {
		got, err := parser.ParseClock(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseClock(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseClock(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseClock(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseLinePlainText(t *testing.T) {
	plainLines := []string{
		"Sending heartbeat to cod2master.activision.com",
		"Going from CS_FREE to CS_CONNECTED for  (num 4 guid 0)",
		"gamedate: Jun 23 2006",
		"",
		"   ",
		"2024-01-15T10:30:00Z",
	}
	for _, line := range plainLines {
		rl, ok := parser.ParseLine(line)
		if ok || rl != nil {
			t.Errorf("ParseLine(%q): expected (nil, false), got (%v, %v)", line, rl, ok)
		}
	}
}

func TestParseLineKill(t *testing.T) {
	line := "12912:13 K;0;0;;gg^3^4hu^1nt;0;2;;v hrbet;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower"
	rl, ok := parser.ParseLine(line)
	if !ok || rl == nil {
		t.Fatal("ParseLine kill: expected non-nil RawLine")
	}
	if rl.EventType != "K" {
		t.Errorf("EventType = %q, want K", rl.EventType)
	}
	if len(rl.Fields) != 13 {
		t.Errorf("Fields len = %d, want 13", len(rl.Fields))
	}
}

func TestParseLineDamageWorld(t *testing.T) {
	line := "12917:22 D;0;4;allies;v hrbet;;-1;world;;none;4;MOD_FALLING;none"
	rl, ok := parser.ParseLine(line)
	if !ok || rl == nil {
		t.Fatal("ParseLine world-damage: expected non-nil RawLine")
	}
	if rl.EventType != "D" {
		t.Errorf("EventType = %q, want D", rl.EventType)
	}
}

func TestParseLineWeapon(t *testing.T) {
	line := "12912:15 Weapon;0;1;puci;frag_grenade_german_mp"
	rl, ok := parser.ParseLine(line)
	if !ok || rl == nil {
		t.Fatal("ParseLine weapon: expected non-nil")
	}
	if rl.EventType != "Weapon" {
		t.Errorf("EventType = %q, want Weapon", rl.EventType)
	}
}

func TestParseLineInitGame(t *testing.T) {
	line := `12898:14 InitGame: \_Admin\Blarc\_Email\jakob@example.com\g_gametype\dm\mapname\mp_toujane\protocol\118`
	rl, ok := parser.ParseLine(line)
	if !ok || rl == nil {
		t.Fatal("ParseLine InitGame: expected non-nil")
	}
	if rl.EventType != "InitGame" {
		t.Errorf("EventType = %q, want InitGame", rl.EventType)
	}
}

func TestParseLineShutdownGame(t *testing.T) {
	line := "12917:34 ShutdownGame:"
	rl, ok := parser.ParseLine(line)
	if !ok || rl == nil {
		t.Fatal("ParseLine ShutdownGame: expected non-nil")
	}
	if rl.EventType != "ShutdownGame" {
		t.Errorf("EventType = %q, want ShutdownGame", rl.EventType)
	}
}

func TestParseKD(t *testing.T) {
	line := "12912:13 K;0;0;;gg^3^4hu^1nt;0;2;;v hrbet;kar98k_mp;135;MOD_RIFLE_BULLET;torso_lower"
	rl, _ := parser.ParseLine(line)
	ev, err := parser.ParseKD(rl, "matchXYZ")
	if err != nil {
		t.Fatalf("ParseKD error: %v", err)
	}
	if !ev.IsKill {
		t.Error("IsKill should be true")
	}
	if ev.VictimNameNorm != "gghunt" {
		t.Errorf("VictimNameNorm = %q, want gghunt", ev.VictimNameNorm)
	}
	if ev.KillerName != "v hrbet" {
		t.Errorf("KillerName = %q, want 'v hrbet'", ev.KillerName)
	}
	if ev.Damage != 135 {
		t.Errorf("Damage = %d, want 135", ev.Damage)
	}
	if ev.Mod != "MOD_RIFLE_BULLET" {
		t.Errorf("Mod = %q, want MOD_RIFLE_BULLET", ev.Mod)
	}
	if ev.IdempotencyKey == "" {
		t.Error("IdempotencyKey must not be empty")
	}
}

func TestParseKDWorldDamage(t *testing.T) {
	line := "12917:22 D;0;4;allies;v hrbet;;-1;world;;none;4;MOD_FALLING;none"
	rl, _ := parser.ParseLine(line)
	ev, err := parser.ParseKD(rl, "matchXYZ")
	if err != nil {
		t.Fatalf("ParseKD world-damage error: %v", err)
	}
	if ev.IsKill {
		t.Error("IsKill should be false for D event")
	}
	if ev.KillerName != "" {
		t.Errorf("KillerName = %q, want empty string for world damage", ev.KillerName)
	}
	if ev.KillerTeam != "world" {
		t.Errorf("KillerTeam = %q, want 'world'", ev.KillerTeam)
	}
}

func TestParseKDTooFewFields(t *testing.T) {
	// Build a RawLine with too few fields directly to test ParseKD error path.
	rl := &model.RawLine{
		EventType: "K",
		Fields:    []string{"K", "0", "1"},
		Raw:       "short",
	}
	_, err := parser.ParseKD(rl, "m")
	if err == nil {
		t.Error("ParseKD with too few fields: expected error, got nil")
	}
}

func TestParseWeapon(t *testing.T) {
	line := "12912:15 Weapon;0;1;puci;frag_grenade_german_mp"
	rl, _ := parser.ParseLine(line)
	ev, err := parser.ParseWeapon(rl, "matchXYZ")
	if err != nil {
		t.Fatalf("ParseWeapon error: %v", err)
	}
	if ev.PlayerName != "puci" {
		t.Errorf("PlayerName = %q, want puci", ev.PlayerName)
	}
	if ev.PlayerNameNorm != "puci" {
		t.Errorf("PlayerNameNorm = %q, want puci", ev.PlayerNameNorm)
	}
	if ev.Weapon != "frag_grenade_german_mp" {
		t.Errorf("Weapon = %q, want frag_grenade_german_mp", ev.Weapon)
	}
}

func TestParseWeaponColorCodedName(t *testing.T) {
	line := "12915:37 Weapon;0;0;gg^3^4hu^1nt;kar98k_mp"
	rl, _ := parser.ParseLine(line)
	ev, err := parser.ParseWeapon(rl, "m")
	if err != nil {
		t.Fatalf("ParseWeapon error: %v", err)
	}
	if ev.PlayerNameNorm != "gghunt" {
		t.Errorf("PlayerNameNorm = %q, want gghunt", ev.PlayerNameNorm)
	}
}

func TestParseInitGame(t *testing.T) {
	line := `12898:14 InitGame: \_Admin\Blarc\_Email\jakob@example.com\g_gametype\dm\mapname\mp_toujane\protocol\118`
	rl, _ := parser.ParseLine(line)
	ev, err := parser.ParseInitGame(rl)
	if err != nil {
		t.Fatalf("ParseInitGame error: %v", err)
	}
	if ev.MapName != "mp_toujane" {
		t.Errorf("MapName = %q, want mp_toujane", ev.MapName)
	}
	if ev.GameType != "dm" {
		t.Errorf("GameType = %q, want dm", ev.GameType)
	}
	if ev.Meta["_Admin"] != "Blarc" {
		t.Errorf("Meta[_Admin] = %q, want Blarc", ev.Meta["_Admin"])
	}
}

func TestIdempotencyKeyDeterministic(t *testing.T) {
	k1 := parser.IdempotencyKey("matchA", "3600", "some raw line")
	k2 := parser.IdempotencyKey("matchA", "3600", "some raw line")
	k3 := parser.IdempotencyKey("matchA", "3600", "different line")
	if k1 != k2 {
		t.Error("IdempotencyKey must be deterministic")
	}
	if k1 == k3 {
		t.Error("IdempotencyKey must differ for different inputs")
	}
}
