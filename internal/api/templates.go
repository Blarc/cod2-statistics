package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"time"
)

func mustParseTemplates(fsys fs.FS) *template.Template {
	funcMap := template.FuncMap{
		"kd": func(kills, deaths int) string {
			if deaths == 0 {
				return fmt.Sprintf("%d", kills)
			}
			return fmt.Sprintf("%.2f", float64(kills)/float64(deaths))
		},
		"headshotPct": func(hs, kills int) string {
			if kills == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(hs)/float64(kills)*100)
		},
		"fmtDateTime": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "—"
			}
			return t.Local().Format("02 Jan 2006 15:04:05")
		},
		"fmtEventTime": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "—"
			}
			return t.Local().Format("15:04:05")
		},
		"timeISO": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return ""
			}
			return t.UTC().Format(time.RFC3339Nano)
		},
		"toJSON": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"favWeapon": func(m map[string]int) string {
			best := "—"
			bestCount := 0
			for w, c := range m {
				if c > bestCount {
					bestCount = c
					best = w
				}
			}
			return best
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"min": func(a, b int) int {
			if a < b {
				return a
			}
			return b
		},
	}

	t, err := template.New("").Funcs(funcMap).ParseFS(fsys, "web/templates/*.html", "web/templates/partials/*.html")
	if err != nil {
		panic(fmt.Sprintf("parse templates: %v", err))
	}
	return t
}
