package main

import (
	"cod2-statistics/internal/api"
	"cod2-statistics/internal/config"
	"cod2-statistics/internal/poller"
	"cod2-statistics/internal/store"
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	cfg, err := config.Load(os.Getenv("CONFIG_FILE"))
	if err != nil {
		log.Fatalf("config: %v", err)
	} else {
		log.Printf("config: %v", cfg)
	}

	st, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Close()

	p := poller.New(cfg, st)
	srv := api.New(st, p, webFS)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go p.Run(ctx)

	httpSrv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
