package stats

import (
	"cod2-statistics/internal/model"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// JSONMatch is the serialisable form of a match for JSON output.
type JSONMatch struct {
	ID       string        `json:"id"`
	Map      string        `json:"map"`
	GameType string        `json:"game_type"`
	Players  []*JSONPlayer `json:"players"`
}

// JSONPlayer is the serialisable form of per-player stats.
type JSONPlayer struct {
	Name        string         `json:"name"`
	Aliases     []string       `json:"aliases,omitempty"`
	Kills       int            `json:"kills"`
	Deaths      int            `json:"deaths"`
	DamageDealt int            `json:"damage_dealt"`
	DamageTaken int            `json:"damage_taken"`
	Headshots   int            `json:"headshots"`
	WeaponKills map[string]int `json:"weapon_kills,omitempty"`
	EventCount  int            `json:"event_count"`
}

// ToJSON converts a slice of matches to the JSON output structure.
func ToJSON(matches []*model.Match) []JSONMatch {
	out := make([]JSONMatch, 0, len(matches))
	for _, m := range matches {
		out = append(out, JSONMatch{
			ID:       m.ID,
			Map:      m.MapName,
			GameType: m.GameType,
			Players:  sortedPlayers(m),
		})
	}
	return out
}

// WriteJSON writes JSON output to w.
func WriteJSON(w io.Writer, matches []*model.Match) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]interface{}{"matches": ToJSON(matches)})
}

// WritePretty writes a human-readable table to w.
func WritePretty(w io.Writer, matches []*model.Match) {
	for i, m := range matches {
		fmt.Fprintf(w, "\n=== Match %d: %s (%s) [id: %s...] ===\n",
			i+1, m.MapName, m.GameType, m.ID[:8])

		players := sortedPlayers(m)
		if len(players) == 0 {
			fmt.Fprintln(w, "  (no players)")
			continue
		}

		// Header
		fmt.Fprintf(w, "%-20s %5s %5s %8s %8s %5s %12s\n",
			"Player", "Kills", "Deaths", "Dmg-Out", "Dmg-In", "HS", "KD")
		fmt.Fprintln(w, strings.Repeat("-", 70))

		for _, p := range players {
			kd := 0.0
			if p.Deaths > 0 {
				kd = float64(p.Kills) / float64(p.Deaths)
			} else if p.Kills > 0 {
				kd = float64(p.Kills)
			}
			fmt.Fprintf(w, "%-20s %5d %5d %8d %8d %5d %12.2f\n",
				p.Name, p.Kills, p.Deaths, p.DamageDealt, p.DamageTaken, p.Headshots, kd)
		}

		// Top weapons
		for _, p := range players {
			if len(p.WeaponKills) == 0 {
				continue
			}
			type wk struct {
				w string
				k int
			}
			var ws []wk
			for weapon, kills := range p.WeaponKills {
				ws = append(ws, wk{weapon, kills})
			}
			sort.Slice(ws, func(i, j int) bool { return ws[i].k > ws[j].k })
			parts := make([]string, 0, len(ws))
			for _, w := range ws {
				parts = append(parts, fmt.Sprintf("%s×%d", w.w, w.k))
			}
			fmt.Fprintf(w, "  %-18s weapons: %s\n", p.Name, strings.Join(parts, ", "))
		}
	}
}

func sortedPlayers(m *model.Match) []*JSONPlayer {
	players := make([]*JSONPlayer, 0, len(m.Players))
	for _, ps := range m.Players {
		players = append(players, &JSONPlayer{
			Name:        ps.NormalizedName,
			Aliases:     ps.Aliases,
			Kills:       ps.Kills,
			Deaths:      ps.Deaths,
			DamageDealt: ps.DamageDealt,
			DamageTaken: ps.DamageTaken,
			Headshots:   ps.Headshots,
			WeaponKills: ps.WeaponKills,
			EventCount:  ps.EventCount,
		})
	}
	// Sort by kills descending, then name ascending.
	sort.Slice(players, func(i, j int) bool {
		if players[i].Kills != players[j].Kills {
			return players[i].Kills > players[j].Kills
		}
		return players[i].Name < players[j].Name
	})
	return players
}
