package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"poe2-price-checker/internal/config"
	"poe2-price-checker/internal/parser"
)

func main() {
	cfgPath := flag.String("config", "config.example.yaml", "path to parser config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if cfg.Postgres.DSN == "" {
		log.Fatal("postgres.dsn is required")
	}

	poeSessID := os.Getenv("POESESSID")
	if poeSessID == "" {
		log.Fatal("POESESSID is required (set via environment)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r := parser.New(cfg, poeSessID)
	if err := r.Run(ctx); err != nil {
		log.Fatalf("parser run: %v", err)
	}
}
