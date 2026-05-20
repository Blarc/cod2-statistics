package poller

import (
	"cod2-statistics/internal/config"
	"cod2-statistics/internal/loki"
	"cod2-statistics/internal/matcher"
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"cod2-statistics/internal/store"
	"context"
	"fmt"
	"log"
	"time"
)

type Poller struct {
	cfg   *config.Config
	store *store.Store
}

func New(cfg *config.Config, st *store.Store) *Poller {
	return &Poller{cfg: cfg, store: st}
}

func (p *Poller) Run(ctx context.Context) {
	if err := p.pollOnce(ctx); err != nil {
		log.Printf("poll error: %v", err)
	}

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.pollOnce(ctx); err != nil {
				log.Printf("poll error: %v", err)
			}
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) error {
	if p.cfg.LokiURL == "" || p.cfg.LokiQuery == "" {
		err := fmt.Errorf("LOKI_URL and LOKI_QUERY are required")
		_ = p.store.SetLastPollError(err.Error())
		return err
	}

	status, err := p.store.GetPollStatus()
	if err != nil {
		return fmt.Errorf("get poll status: %w", err)
	}

	var startTime time.Time
	if status.LastPollTime != nil {
		startTime = status.LastPollTime.Add(-p.cfg.LokiPollOverlap)
	} else {
		startTime = time.Now().Add(-p.cfg.LokiInitialLookback)
	}
	queryEnd := time.Now().UTC()

	lokiCfg := loki.Config{
		URL:      p.cfg.LokiURL,
		Query:    p.cfg.LokiQuery,
		Username: p.cfg.LokiUsername,
		Password: p.cfg.LokiPassword,
		Start:    startTime.UTC().Format(time.RFC3339Nano),
		End:      queryEnd.Format(time.RFC3339Nano),
	}

	rawLines, err := loki.FetchLines(ctx, lokiCfg)
	if err != nil {
		_ = p.store.SetLastPollError(err.Error())
		return fmt.Errorf("fetch from loki: %w", err)
	}

	var rls []*model.RawLine
	for _, line := range rawLines {
		if rl, ok := parser.ParseLine(line); ok {
			rls = append(rls, rl)
		}
	}

	var cont *matcher.Continuation
	openMatch, err := p.store.GetOpenMatch()
	if err != nil {
		_ = p.store.SetLastPollError(err.Error())
		return fmt.Errorf("get open match: %w", err)
	}
	if openMatch != nil {
		cont = &matcher.Continuation{
			MatchID:   openMatch.MatchID,
			MapName:   openMatch.MapName,
			GameType:  openMatch.GameType,
			StartedAt: openMatch.StartedAt,
			LastClock: openMatch.LastClock,
		}
	}

	matches, nextCont, err := matcher.ProcessLinesWithState(rls, cont)
	if err != nil {
		_ = p.store.SetLastPollError(err.Error())
		return fmt.Errorf("process lines: %w", err)
	}

	for _, m := range matches {
		if err := p.store.SaveMatch(m); err != nil {
			_ = p.store.SetLastPollError(err.Error())
			return fmt.Errorf("save match %s: %w", m.ID[:8], err)
		}
	}

	var nextOpen *store.OpenMatch
	if nextCont != nil {
		nextOpen = &store.OpenMatch{
			MatchID:   nextCont.MatchID,
			MapName:   nextCont.MapName,
			GameType:  nextCont.GameType,
			StartedAt: nextCont.StartedAt,
			LastClock: nextCont.LastClock,
		}
	}
	if err := p.store.SetOpenMatch(nextOpen); err != nil {
		_ = p.store.SetLastPollError(err.Error())
		return fmt.Errorf("set open match state: %w", err)
	}

	if err := p.store.SetLastPollNS(queryEnd.UnixNano()); err != nil {
		return fmt.Errorf("set last poll ns: %w", err)
	}
	if err := p.store.IncrementPollCount(); err != nil {
		return fmt.Errorf("increment poll count: %w", err)
	}
	_ = p.store.SetLastPollError("")

	log.Printf("poll complete: %d lines → %d matches", len(rawLines), len(matches))
	return nil
}
