package main

import (
	"bufio"
	"cod2-statistics/internal/matcher"
	"cod2-statistics/internal/model"
	"cod2-statistics/internal/parser"
	"cod2-statistics/internal/store"
	"flag"
	"log"
	"os"
)

func main() {
	input := flag.String("input", "", "path to log file (required)")
	dbPath := flag.String("db", os.Getenv("DB_PATH"), "SQLite database path")
	flag.Parse()

	if *input == "" {
		log.Fatal("usage: seed -input <logfile> [-db <path>]")
	}
	if *dbPath == "" {
		*dbPath = "cod2stats.db"
	}

	f, err := os.Open(*input)
	if err != nil {
		log.Fatalf("open %s: %v", *input, err)
	}
	defer f.Close()

	var lines []*model.RawLine
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if rl, ok := parser.ParseLine(scanner.Text()); ok {
			lines = append(lines, rl)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("read: %v", err)
	}

	matches, err := matcher.ProcessLines(lines)
	if err != nil {
		log.Fatalf("match: %v", err)
	}

	st, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Close()

	for _, m := range matches {
		if err := st.SaveMatch(m); err != nil {
			log.Fatalf("save match %s: %v", m.ID[:8], err)
		}
	}
	log.Printf("seeded %d matches from %s into %s", len(matches), *input, *dbPath)
}
