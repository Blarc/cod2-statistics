package matcher

import (
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"fmt"
	"sort"
	"strconv"
)

const clockResetThreshold = 60 // seconds drop that implies a server restart

// SortOldestFirst sorts RawLines by ClockSec ascending.
// Both file sources and Loki deliver lines newest-first, so this must always
// be called before ProcessLines.
func SortOldestFirst(lines []*model.RawLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].ClockSec < lines[j].ClockSec
	})
}

// Continuation seeds ProcessLinesWithContinuation with the latest known
// in-progress match from a previous polling batch.
type Continuation struct {
	MatchID   string
	MapName   string
	GameType  string
	StartedAt int
	LastClock int
}

// ProcessLines groups sorted lines into matches using boundary rules (in priority order):
//  1. InitGame  → finalise current match, start a new one
//  2. ShutdownGame → finalise current match
//  3. Clock reset (current < prev - threshold) → implicit match boundary
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

	prevClock := -1
	if cont != nil && cont.MatchID != "" {
		current = newContinuationBuilder(cont)
		prevClock = cont.LastClock
	}

	for _, rl := range lines {
		// Clock-reset boundary check (evaluated before event dispatch).
		if prevClock >= 0 && rl.ClockSec < prevClock-clockResetThreshold {
			matches = appendFinalized(matches, current, rl.ClockSec)
			current = nil
		}
		prevClock = rl.ClockSec

		switch rl.EventType {
		case "InitGame":
			matches = appendFinalized(matches, current, rl.ClockSec)
			ig, err := parser.ParseInitGame(rl)
			if err != nil {
				continue
			}
			current = newMatchBuilder(ig)

		case "ShutdownGame":
			if current != nil {
				matches = appendFinalized(matches, current, rl.ClockSec)
				current = nil
			}

		case "K", "D":
			if current == nil {
				current = newOrphanBuilder(rl.ClockSec)
			}
			matchID := current.match.ID
			ev, err := parser.ParseKD(rl, matchID)
			if err != nil {
				continue
			}
			current.addKD(ev)

		case "Weapon":
			if current == nil {
				current = newOrphanBuilder(rl.ClockSec)
			}
			matchID := current.match.ID
			ev, err := parser.ParseWeapon(rl, matchID)
			if err != nil {
				continue
			}
			current.addWeapon(ev)
		}
	}

	matches = appendFinalized(matches, current, prevClock)

	next := buildContinuation(current, prevClock, cont)
	return matches, next, nil
}

// matchBuilder accumulates events for one in-progress match.
type matchBuilder struct {
	match  *model.Match
	seen   map[string]struct{} // idempotency keys
	seeded bool
}

func newMatchBuilder(ig *model.InitGameEvent) *matchBuilder {
	id := parser.IdempotencyKey(ig.MapName, ig.GameType, strconv.Itoa(ig.ClockSec))
	return &matchBuilder{
		match: &model.Match{
			ID:        id,
			MapName:   ig.MapName,
			GameType:  ig.GameType,
			StartedAt: ig.ClockSec,
			Players:   make(map[string]*model.PlayerStats),
		},
		seen:   make(map[string]struct{}),
		seeded: false,
	}
}

func newOrphanBuilder(clockSec int) *matchBuilder {
	id := parser.IdempotencyKey("orphan", strconv.Itoa(clockSec))
	return &matchBuilder{
		match: &model.Match{
			ID:        id,
			MapName:   "",
			GameType:  "",
			StartedAt: clockSec,
			Players:   make(map[string]*model.PlayerStats),
		},
		seen:   make(map[string]struct{}),
		seeded: false,
	}
}

func newContinuationBuilder(cont *Continuation) *matchBuilder {
	start := cont.StartedAt
	if start == 0 {
		start = cont.LastClock
	}
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

func appendFinalized(matches []*model.Match, current *matchBuilder, endClock int) []*model.Match {
	if current == nil {
		return matches
	}
	if endClock < 0 {
		endClock = current.match.StartedAt
	}
	m := current.finalise(endClock)
	// Ignore empty placeholders (e.g., continuation seed with no new events).
	if current.seeded && len(m.Players) == 0 && len(m.KillEvents) == 0 && len(m.DamageEvents) == 0 && len(m.WeaponEvents) == 0 {
		return matches
	}
	return append(matches, m)
}

func buildContinuation(current *matchBuilder, prevClock int, prior *Continuation) *Continuation {
	if current == nil {
		return nil
	}
	lastClock := prevClock
	if lastClock < 0 {
		if prior != nil {
			lastClock = prior.LastClock
		} else {
			lastClock = current.match.StartedAt
		}
	}
	return &Continuation{
		MatchID:   current.match.ID,
		MapName:   current.match.MapName,
		GameType:  current.match.GameType,
		StartedAt: current.match.StartedAt,
		LastClock: lastClock,
	}
}

func (mb *matchBuilder) finalise(endClock int) *model.Match {
	mb.match.EndedAt = endClock
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
		updateSeen(v, ev.ClockSec)
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
		updateSeen(k, ev.ClockSec)
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
		updateSeen(p, ev.ClockSec)
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

func updateSeen(p *model.PlayerStats, clock int) {
	if p.FirstSeen == 0 || clock < p.FirstSeen {
		p.FirstSeen = clock
	}
	if clock > p.LastSeen {
		p.LastSeen = clock
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
