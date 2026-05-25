package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"poe2-price-checker/internal/refsync"
)

func main() {
	var (
		dsn     = flag.String("dsn", os.Getenv("DATABASE_DSN"), "postgres dsn")
		baseURL = flag.String("base-url", "https://repoe-fork.github.io/poe2", "repoe base url")
		timeout = flag.Duration("timeout", 2*time.Minute, "sync timeout")
	)
	flag.Parse()

	if *dsn == "" {
		log.Fatal("dsn is required (flag -dsn or env DATABASE_DSN)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := refsync.Run(ctx, *dsn, *baseURL); err != nil {
		log.Fatalf("ref sync failed: %v", err)
	}

	log.Println("ref sync ok")
}
