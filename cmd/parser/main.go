package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"poe2-price-checker/internal/config"
)

func main() {
	cfgPath := flag.String("config", "config.example.yaml", "path to parser config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	poeSessID := os.Getenv("POESESSID")
	if poeSessID == "" {
		log.Fatal("POESESSID is required (set via environment)")
	}

	fmt.Printf("parser bootstrap ok: league=%s workers=%d\n", cfg.Parser.League, len(cfg.Parser.Workers))
	for _, w := range cfg.Parser.Workers {
		fmt.Printf("worker=%s proxy=%q search_rps=%.2f fetch_rps=%.2f\n", w.Name, w.Proxy, w.RateLimit.SearchRPS, w.RateLimit.FetchRPS)
	}
}
