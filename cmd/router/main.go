// Command router is the strangler layer in front of legacy and newsvc.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a-berahman/strangler-go/internal/router"
)

func main() {
	path := flag.String("config", "router.json", "config file")
	listen := flag.String("listen", "", "listen address, overrides the config")
	debug := flag.Bool("debug", false, "also log the shadow matches")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := router.Load(*path)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	rt, err := router.New(cfg, log)
	if err != nil {
		log.Error("router", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{Addr: cfg.Listen, Handler: rt, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info("router listening", "addr", cfg.Listen, "routes", len(cfg.Routes))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Error("router stopped", "err", err)
		os.Exit(1)
	}
	// Let the last shadow compares finish and log before we exit.
	rt.Wait()
}
