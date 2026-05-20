package api

import (
	"cod2-statistics/internal/store"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
)

type IndexData struct {
	Matches []store.MatchSummary
	Players []store.PlayerSummary
	Poll    store.PollStatus
}

type MatchesData struct {
	Matches    []store.MatchSummary
	Total      int
	Limit      int
	Offset     int
	MapFilter  string
	TypeFilter string
}

type MatchData struct {
	Detail      store.MatchDetail
	WeaponChart template.JS
}

type PlayersData struct {
	Players []store.PlayerSummary
	Total   int
	Limit   int
	Offset  int
}

type PlayerData struct {
	Detail      store.PlayerDetail
	Fun         FunStats
	WeaponChart template.JS
}

type FunStats struct {
	KD          string
	HeadshotPct string
	FavWeapon   string
	Nemesis     string
	FavPrey     string
	BestMatch   store.PlayerMatchStat
	WorstMatch  store.PlayerMatchStat
}

type KillsData struct {
	MatchID string
	Kills   []store.KillEventRow
	Total   int
	Limit   int
	Offset  int
}

type DamageData struct {
	MatchID string
	Damage  []store.DamageEventRow
	Total   int
	Limit   int
	Offset  int
}

func (s *Server) handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	matches, _, err := s.store.ListMatches("", "", 5, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	players, _, err := s.store.ListPlayers(5, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	poll, err := s.store.GetPollStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "index", IndexData{
		Matches: matches,
		Players: players,
		Poll:    poll,
	})
}

func (s *Server) handleWebMatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)
	mapFilter := q.Get("map")
	typeFilter := q.Get("game_type")

	matches, total, err := s.store.ListMatches(mapFilter, typeFilter, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "matches", MatchesData{
		Matches:    matches,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		MapFilter:  mapFilter,
		TypeFilter: typeFilter,
	})
}

func (s *Server) handleWebMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	m, err := s.store.GetMatch(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if m == nil {
		http.NotFound(w, r)
		return
	}

	weaponKills := map[string]int{}
	for _, p := range m.Players {
		for w, k := range p.WeaponKills {
			weaponKills[w] += k
		}
	}

	s.renderTemplate(w, "match", MatchData{
		Detail:      *m,
		WeaponChart: marshalJS(weaponKills),
	})
}

func (s *Server) handleWebPlayers(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	players, total, err := s.store.ListPlayers(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "players", PlayersData{
		Players: players,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *Server) handleWebPlayer(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.PathValue("name"))
	if err != nil {
		http.Error(w, "invalid player name", http.StatusBadRequest)
		return
	}

	pd, err := s.store.GetPlayer(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pd == nil {
		http.NotFound(w, r)
		return
	}

	weaponKills := map[string]int{}
	for _, m := range pd.Matches {
		for w, k := range m.WeaponKills {
			weaponKills[w] += k
		}
	}

	s.renderTemplate(w, "player", PlayerData{
		Detail:      *pd,
		Fun:         computeFunStats(s, *pd),
		WeaponChart: marshalJS(weaponKills),
	})
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		fmt.Fprintf(w, "\n<!-- template error: %v -->", err)
	}
}

func marshalJS(v any) template.JS {
	b, _ := json.Marshal(v)
	return template.JS(b)
}

func computeFunStats(s *Server, detail store.PlayerDetail) FunStats {
	var fun FunStats

	if detail.TotalDeaths > 0 {
		fun.KD = fmt.Sprintf("%.2f", float64(detail.TotalKills)/float64(detail.TotalDeaths))
	} else {
		fun.KD = fmt.Sprintf("%d", detail.TotalKills)
	}

	if detail.TotalKills > 0 {
		fun.HeadshotPct = fmt.Sprintf("%.1f%%", float64(detail.TotalHeadshots)/float64(detail.TotalKills)*100)
	} else {
		fun.HeadshotPct = "0.0%"
	}

	weaponTotals := map[string]int{}
	for _, m := range detail.Matches {
		for w, k := range m.WeaponKills {
			weaponTotals[w] += k
		}
	}
	fun.FavWeapon = topWeapon(weaponTotals)

	asKiller, asVictim, _ := s.store.GetPlayerKillPairs(detail.Name)

	fun.Nemesis = "—"
	for _, p := range asVictim {
		if p.Killer != detail.Name {
			fun.Nemesis = p.Killer
			break
		}
	}

	fun.FavPrey = "—"
	for _, p := range asKiller {
		if p.Victim != detail.Name {
			fun.FavPrey = p.Victim
			break
		}
	}

	if len(detail.Matches) > 0 {
		fun.BestMatch = detail.Matches[0]
		fun.WorstMatch = detail.Matches[0]
		bestR := kdRatio(detail.Matches[0].Kills, detail.Matches[0].Deaths)
		worstR := bestR
		for _, m := range detail.Matches[1:] {
			r := kdRatio(m.Kills, m.Deaths)
			if r > bestR {
				bestR = r
				fun.BestMatch = m
			}
			if r < worstR {
				worstR = r
				fun.WorstMatch = m
			}
		}
	}

	return fun
}

func kdRatio(kills, deaths int) float64 {
	if deaths == 0 {
		return float64(kills)
	}
	return float64(kills) / float64(deaths)
}

func topWeapon(m map[string]int) string {
	best := "—"
	bestCount := 0
	for w, c := range m {
		if c > bestCount {
			bestCount = c
			best = w
		}
	}
	return best
}
