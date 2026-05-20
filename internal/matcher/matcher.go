package matcher

import (
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"fmt"
	"sort"
	"time"
)

const wallTimeResetThreshold = 60 * time.Second // backwards jump that implies a new session

// SortOldestFirst sorts RawLines by wall-clock timestamp ascending.
// Both file sources and Loki deliver lines newest-first, so this must always
// be called before ProcessLines.
func SortOldestFirst(lines []*model.RawLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		ti := lines[i].Time
		tj := lines[j].Time
		if ti.IsZero() || tj.IsZero() {
			return lines[i].Raw < lines[j].Raw
		}
		return ti.Before(tj)
	})
}

// Continuation seeds ProcessLinesWithContinuation with the latest known
// in-progress match from a previous polling batch.
type Continuation struct {
	MatchID   string
	MapName   string
	GameType  string
	StartedAt time.Time
}

// ProcessLines groups sorted lines into matches using boundary rules (in priority order):
//  1. InitGame  → finalise current match, start a new one
//  2. ShutdownGame → finalise current match
//  3. Wall-time rewind (current < prev - threshold) → implicit match boundary
//
// Lines that appear before the first InitGame are assigned to an implicit match
// with no map/gametype metadata.
func ProcessLines(lines []*model.RawLine) ([]*model.Match, error) {
	matches, _, err := ProcessLinesWithState(lines, nil)
	return matches, err
}

// ProcessLinesWithContinuation behaves like ProcessLines, but can continue the
// previous open match across poll boundaries.
func ProcessLinesWithContinuation(lines []*model.RawLine, cont *Continuation) ([]*model.Match, error) {
	matches, _, err := ProcessLinesWithState(lines, cont)
	return matches, err
}

// ProcessLinesWithState behaves like ProcessLinesWithContinuation and also
// returns the continuation state for the next poll cycle.
func ProcessLinesWithState(lines []*model.RawLine, cont *Continuation) ([]*model.Match, *Continuation, error) {
	var matches []*model.Match
	var current *matchBuilder

	var prevTime time.Time
	var lastSeenTime time.Time
	if cont != nil && cont.MatchID != "" {
		current = newContinuationBuilder(cont)
	}

	for _, rl := range lines {
		// Wall-time rewind boundary check (evaluated before event dispatch).
		if !prevTime.IsZero() && !rl.Time.IsZero() && rl.Time.Before(prevTime.Add(-wallTimeResetThreshold)) {
			matches = appendFinalized(matches, current, rl.Time)
			current = nil
		}
		if !rl.Time.IsZero() {
			prevTime = rl.Time
			lastSeenTime = rl.Time
		}

		switch rl.EventType {
		case "InitGame":
			matches = appendFinalized(matches, current, rl.Time)
			ig, err := parser.ParseInitGame(rl)
			if err != nil {
				continue
			}
			current = newMatchBuilder(ig)

		case "ShutdownGame":
			if current != nil {
				matches = appendFinalized(matches, current, rl.Time)
				current = nil
			}

		case "K", "D":
			if current == nil {
				current = newOrphanBuilder(rl.Raw, rl.Time)
			}
			matchID := current.match.ID
			ev, err := parser.ParseKD(rl, matchID)
			if err != nil {
				continue
			}
			current.addKD(ev)

		case "Weapon":
			if current == nil {
				current = newOrphanBuilder(rl.Raw, rl.Time)
			}
			matchID := current.match.ID
			ev, err := parser.ParseWeapon(rl, matchID)
			if err != nil {
				continue
			}
			current.addWeapon(ev)
		}
	}

	matches = appendFinalized(matches, current, lastSeenTime)

	next := buildContinuation(current)
	return matches, next, nil
}

// matchBuilder accumulates events for one in-progress match.
type matchBuilder struct {
	match  *model.Match
	seen   map[string]struct{} // idempotency keys
	seeded bool
}

func newMatchBuilder(ig *model.InitGameEvent) *matchBuilder {
	id := parser.IdempotencyKey(ig.MapName, ig.GameType, ig.Raw)
	return &matchBuilder{
		match: &model.Match{
			ID:        id,
			MapName:   ig.MapName,
			GameType:  ig.GameType,
			StartedAt: ig.Time,
			Players:   make(map[string]*model.PlayerStats),
		},
		seen:   make(map[string]struct{}),
		seeded: false,
	}
}

func newOrphanBuilder(seed string, startedAt time.Time) *matchBuilder {
	id := parser.IdempotencyKey("orphan", seed)
	return &matchBuilder{
		match: &model.Match{
			ID:        id,
			MapName:   "",
			GameType:  "",
			StartedAt: startedAt,
			Players:   make(map[string]*model.PlayerStats),
		},
		seen:   make(map[string]struct{}),
		seeded: false,
	}
}

func newContinuationBuilder(cont *Continuation) *matchBuilder {
	start := cont.StartedAt
	return &matchBuilder{
		match: &model.Match{
			ID:        cont.MatchID,
			MapName:   cont.MapName,
			GameType:  cont.GameType,
			StartedAt: start,
			Players:   make(map[string]*model.PlayerStats),
		},
		seen:   make(map[string]struct{}),
		seeded: true,
	}
}

func appendFinalized(matches []*model.Match, current *matchBuilder, endTime time.Time) []*model.Match {
	if current == nil {
		return matches
	}
	if endTime.IsZero() {
		endTime = current.match.StartedAt
	}
	m := current.finalise(endTime)
	// Ignore empty placeholders (e.g., continuation seed with no new events).
	if current.seeded && len(m.Players) == 0 && len(m.KillEvents) == 0 && len(m.DamageEvents) == 0 && len(m.WeaponEvents) == 0 {
		return matches
	}
	return append(matches, m)
}

func buildContinuation(current *matchBuilder) *Continuation {
	if current == nil {
		return nil
	}
	return &Continuation{
		MatchID:   current.match.ID,
		MapName:   current.match.MapName,
		GameType:  current.match.GameType,
		StartedAt: current.match.StartedAt,
	}
}

func (mb *matchBuilder) finalise(endTime time.Time) *model.Match {
	mb.match.EndedAt = endTime
	return mb.match
}

func (mb *matchBuilder) addKD(ev *model.KDEvent) {
	if _, dup := mb.seen[ev.IdempotencyKey]; dup {
		return
	}
	mb.seen[ev.IdempotencyKey] = struct{}{}

	if ev.IsKill {
		mb.match.KillEvents = append(mb.match.KillEvents, ev)
	} else {
		mb.match.DamageEvents = append(mb.match.DamageEvents, ev)
	}

	isHeadshot := ev.Mod == "MOD_HEAD_SHOT" || ev.HitLoc == "head"

	// Update victim stats.
	if ev.VictimNameNorm != "" {
		v := mb.ensurePlayer(ev.VictimNameNorm, ev.VictimName)
		v.EventCount++
		updateSeen(v, ev.Time)
		if ev.IsKill {
			v.Deaths++
		}
		if !ev.IsKill {
			v.DamageTaken += ev.Damage
		}
	}

	// Update attacker stats (skip world/environment kills where name is empty).
	if ev.KillerNameNorm != "" {
		k := mb.ensurePlayer(ev.KillerNameNorm, ev.KillerName)
		k.EventCount++
		updateSeen(k, ev.Time)
		if ev.IsKill {
			k.Kills++
			if k.WeaponKills == nil {
				k.WeaponKills = make(map[string]int)
			}
			k.WeaponKills[ev.Weapon]++
			if isHeadshot {
				k.Headshots++
			}
		}
		if !ev.IsKill {
			k.DamageDealt += ev.Damage
		}
	}
}

func (mb *matchBuilder) addWeapon(ev *model.WeaponEvent) {
	if _, dup := mb.seen[ev.IdempotencyKey]; dup {
		return
	}
	mb.seen[ev.IdempotencyKey] = struct{}{}
	mb.match.WeaponEvents = append(mb.match.WeaponEvents, ev)

	if ev.PlayerNameNorm != "" {
		p := mb.ensurePlayer(ev.PlayerNameNorm, ev.PlayerName)
		p.EventCount++
		updateSeen(p, ev.Time)
	}
}

func (mb *matchBuilder) ensurePlayer(norm, raw string) *model.PlayerStats {
	p, ok := mb.match.Players[norm]
	if !ok {
		p = &model.PlayerStats{
			NormalizedName: norm,
			WeaponKills:    make(map[string]int),
		}
		mb.match.Players[norm] = p
	}
	// Track raw aliases without duplicates.
	if raw != "" && raw != norm {
		if !containsAlias(p.Aliases, raw) {
			p.Aliases = append(p.Aliases, raw)
		}
	}
	return p
}

func updateSeen(p *model.PlayerStats, eventTime time.Time) {
	if eventTime.IsZero() {
		return
	}
	if p.FirstSeen.IsZero() || eventTime.Before(p.FirstSeen) {
		p.FirstSeen = eventTime
	}
	if p.LastSeen.IsZero() || eventTime.After(p.LastSeen) {
		p.LastSeen = eventTime
	}
}

func containsAlias(aliases []string, s string) bool {
	for _, a := range aliases {
		if a == s {
			return true
		}
	}
	return false
}

// MatchSummary returns a human-readable one-line summary of a match.
func MatchSummary(m *model.Match) string {
	return fmt.Sprintf("match=%s map=%s type=%s events(k=%d d=%d w=%d)",
		m.ID[:8], m.MapName, m.GameType,
		len(m.KillEvents), len(m.DamageEvents), len(m.WeaponEvents))
}
