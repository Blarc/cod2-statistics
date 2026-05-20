package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

const defaultLimit = 5000

// lokiResponse is the subset of the Loki query_range API we need.
type lokiResponse struct {
	Data struct {
		Result []struct {
			Values [][2]string `json:"values"` // [ns_timestamp, line]
		} `json:"result"`
	} `json:"data"`
}

// Entry pairs a nanosecond timestamp with a log line.
type Entry struct {
	Timestamp int64
	Line      string
}

// FetchLines calls the Loki query_range API and returns all matching log lines
// sorted oldest-first by nanosecond wall-clock timestamp.
//
// The clock-reset heuristic in matcher.ProcessLines depends on wall-clock order,
// so callers should NOT additionally call matcher.SortOldestFirst on Loki results.
func FetchLines(ctx context.Context, cfg Config) ([]Entry, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("loki URL is required")
	}
	if cfg.Query == "" {
		return nil, fmt.Errorf("loki query is required")
	}

	start := cfg.Start
	if start == "" {
		start = time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	}
	end := cfg.End
	if end == "" {
		end = time.Now().UTC().Format(time.RFC3339)
	}

	var entries []Entry

	// Paginate using 'start' cursor until no more results.
	cursor := start
	client := &http.Client{Timeout: 30 * time.Second}

	for {
		params := url.Values{}
		params.Set("query", cfg.Query)
		params.Set("start", cursor)
		params.Set("end", end)
		params.Set("direction", "forward")
		params.Set("limit", strconv.Itoa(defaultLimit))

		reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", cfg.URL, params.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		if cfg.Username != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("loki request: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read loki response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("loki returned %d: %s", resp.StatusCode, body)
		}

		var lr lokiResponse
		if err := json.Unmarshal(body, &lr); err != nil {
			return nil, fmt.Errorf("parse loki response: %w", err)
		}

		batchSize := 0
		var lastNS int64
		for _, stream := range lr.Data.Result {
			for _, val := range stream.Values {
				ns, err := strconv.ParseInt(val[0], 10, 64)
				if err != nil {
					continue
				}
				entries = append(entries, Entry{Timestamp: ns, Line: val[1]})
				batchSize++
				if ns > lastNS {
					lastNS = ns
				}
			}
		}

		// No more results or under the limit — we're done.
		if batchSize < defaultLimit || lastNS == 0 {
			break
		}

		// Advance cursor past the last timestamp (add 1 ns to avoid re-fetching).
		cursor = time.Unix(0, lastNS+1).UTC().Format(time.RFC3339Nano)
	}

	// Sort oldest-first by wall-clock nanosecond timestamp.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})
	return entries, nil
}
