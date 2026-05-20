package parser

import (
	"cod2-statistics/internal/loki"
	"cod2-statistics/internal/model"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var colorCodeRe = regexp.MustCompile(`\^[0-9]`)

// wallClockRe matches ISO-8601 / RFC3339 timestamp lines that Loki/Promtail
// may prepend before each game-log line.
var wallClockRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

// StripColorCodes removes ^[0-9] color-code sequences from s.
func StripColorCodes(s string) string {
	return colorCodeRe.ReplaceAllString(s, "")
}

// ParseClock converts "MM:SS" (minutes may exceed 9999) to total seconds.
func ParseClock(s string) (int, error) {
	idx := strings.Index(s, ":")
	if idx < 1 {
		return 0, fmt.Errorf("invalid clock %q", s)
	}
	mm, err := strconv.Atoi(s[:idx])
	if err != nil {
		return 0, fmt.Errorf("invalid clock minutes in %q: %w", s, err)
	}
	ss, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return 0, fmt.Errorf("invalid clock seconds in %q: %w", s, err)
	}
	if ss < 0 || ss > 59 {
		return 0, fmt.Errorf("seconds out of range in %q", s)
	}
	return mm*60 + ss, nil
}

// ParseLine tokenises one raw log line into a RawLine with unknown wall-clock time.
// Returns (nil, false) for plain-text lines, wall-clock timestamp lines,
// or any line that doesn't begin with a valid MM:SS prefix.
func ParseLine(line string) (*model.RawLine, bool) {
	return parseLine(line, time.Time{})
}

// ParseLokiEntry tokenises one Loki entry using its nanosecond wall-clock timestamp.
func ParseLokiEntry(entry loki.Entry) (*model.RawLine, bool) {
	t := time.Unix(0, entry.Timestamp).UTC()
	return parseLine(entry.Line, t)
}

func parseLine(raw string, lineTime time.Time) (*model.RawLine, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return nil, false
	}
	// Skip wall-clock timestamp lines injected by Loki/Promtail.
	if wallClockRe.MatchString(line) {
		return nil, false
	}

	// The clock token is the first whitespace-delimited word.
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx < 0 {
		return nil, false
	}
	if _, err := ParseClock(line[:spaceIdx]); err != nil {
		return nil, false
	}

	payload := strings.TrimSpace(line[spaceIdx+1:])
	if payload == "" {
		return nil, false
	}

	// InitGame has no semicolons — handle separately.
	if strings.HasPrefix(payload, "InitGame:") {
		return &model.RawLine{
			Time:      lineTime,
			EventType: "InitGame",
			Fields:    []string{payload},
			Raw:       line,
		}, true
	}

	// ShutdownGame
	if strings.HasPrefix(payload, "ShutdownGame:") {
		return &model.RawLine{
			Time:      lineTime,
			EventType: "ShutdownGame",
			Raw:       line,
		}, true
	}

	// All other structured events are semicolon-delimited.
	if !strings.Contains(payload, ";") {
		return nil, false
	}

	fields := strings.Split(payload, ";")
	if len(fields) == 0 {
		return nil, false
	}

	return &model.RawLine{
		Time:      lineTime,
		EventType: fields[0],
		Fields:    fields,
		Raw:       line,
	}, true
}

// ParseKD parses a K or D RawLine into a KDEvent.
//
// Field layout (0-indexed):
//
//	[0] type  [1] victimGUID  [2] victimId  [3] victimTeam  [4] victimName
//	[5] killerGUID  [6] killerId  [7] killerTeam  [8] killerName
//	[9] weapon  [10] damage  [11] mod  [12] hitloc
func ParseKD(rl *model.RawLine, matchID string) (*model.KDEvent, error) {
	if len(rl.Fields) < 13 {
		return nil, fmt.Errorf("K/D line has %d fields, need 13: %q", len(rl.Fields), rl.Raw)
	}
	dmg, _ := strconv.Atoi(rl.Fields[10]) // tolerate non-numeric → 0

	ev := &model.KDEvent{
		Time:           rl.Time,
		IsKill:         rl.Fields[0] == "K",
		VictimName:     rl.Fields[4],
		VictimNameNorm: StripColorCodes(rl.Fields[4]),
		VictimTeam:     rl.Fields[3],
		KillerName:     rl.Fields[8],
		KillerNameNorm: StripColorCodes(rl.Fields[8]),
		KillerTeam:     rl.Fields[7],
		Weapon:         rl.Fields[9],
		Damage:         dmg,
		Mod:            rl.Fields[11],
		HitLoc:         rl.Fields[12],
	}
	ev.IdempotencyKey = IdempotencyKey(matchID, rl.Raw)
	return ev, nil
}

// ParseWeapon parses a Weapon RawLine into a WeaponEvent.
//
// Field layout: [0] Weapon  [1] GUID  [2] clientId  [3] name  [4] weaponName
func ParseWeapon(rl *model.RawLine, matchID string) (*model.WeaponEvent, error) {
	if len(rl.Fields) < 5 {
		return nil, fmt.Errorf("Weapon line has %d fields, need 5: %q", len(rl.Fields), rl.Raw)
	}
	ev := &model.WeaponEvent{
		Time:           rl.Time,
		PlayerName:     rl.Fields[3],
		PlayerNameNorm: StripColorCodes(rl.Fields[3]),
		Weapon:         rl.Fields[4],
	}
	ev.IdempotencyKey = IdempotencyKey(matchID, rl.Raw)
	return ev, nil
}

// ParseInitGame extracts map name and game type from an InitGame RawLine.
// The payload format is: "InitGame: \key\value\key\value\..."
func ParseInitGame(rl *model.RawLine) (*model.InitGameEvent, error) {
	if len(rl.Fields) == 0 {
		return nil, fmt.Errorf("empty InitGame fields")
	}
	raw := rl.Fields[0]
	colonIdx := strings.Index(raw, ":")
	if colonIdx < 0 {
		return nil, fmt.Errorf("malformed InitGame line: %q", raw)
	}
	kvStr := strings.TrimSpace(raw[colonIdx+1:])
	// Remove leading backslash so Split gives clean pairs.
	kvStr = strings.TrimPrefix(kvStr, `\`)
	parts := strings.Split(kvStr, `\`)

	meta := make(map[string]string, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		meta[parts[i]] = parts[i+1]
	}

	return &model.InitGameEvent{
		Time:     rl.Time,
		Raw:      rl.Raw,
		MapName:  meta["mapname"],
		GameType: meta["g_gametype"],
		Meta:     meta,
	}, nil
}

// IdempotencyKey returns sha256(strings joined by ":") as a hex string.
func IdempotencyKey(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return fmt.Sprintf("%x", h)
}
