package model

// RawLine is a tokenised log line (clock + event type + raw fields).
type RawLine struct {
	ClockSec  int
	EventType string
	Fields    []string
	Raw       string // full trimmed original text
}

// KDEvent covers both K (kill) and D (damage) events — identical field layout.
type KDEvent struct {
	ClockSec       int
	IsKill         bool
	VictimName     string // raw
	VictimNameNorm string // color-stripped
	VictimTeam     string
	KillerName     string // raw; empty string means world/environment
	KillerNameNorm string
	KillerTeam     string
	Weapon         string
	Damage         int
	Mod            string
	HitLoc         string
	IdempotencyKey string
}

// WeaponEvent is a Weapon pickup/switch line.
type WeaponEvent struct {
	ClockSec       int
	PlayerName     string
	PlayerNameNorm string
	Weapon         string
	IdempotencyKey string
}

// InitGameEvent holds parsed key-value metadata from an InitGame line.
type InitGameEvent struct {
	ClockSec int
	MapName  string
	GameType string
	Meta     map[string]string
}

// PlayerStats holds per-match statistics for one player, keyed by normalised name.
type PlayerStats struct {
	NormalizedName string
	Aliases        []string
	Kills          int
	Deaths         int
	DamageDealt    int
	DamageTaken    int
	Headshots      int
	WeaponKills    map[string]int
	FirstSeen      int // clock seconds
	LastSeen       int
	EventCount     int
}

// Match is one game session bounded by InitGame/ShutdownGame or a clock reset.
type Match struct {
	ID           string
	MapName      string
	GameType     string
	StartedAt    int // clock seconds
	EndedAt      int
	KillEvents   []*KDEvent
	DamageEvents []*KDEvent
	WeaponEvents []*WeaponEvent
	Players      map[string]*PlayerStats // key: normalised name
}
