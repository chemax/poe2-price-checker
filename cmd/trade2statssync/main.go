package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"poe2-price-checker/internal/trade2stats"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_DSN"), "postgres dsn")
	timeout := flag.Duration("timeout", 3*time.Minute, "sync timeout")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("dsn is required")
	}
	db, err := sql.Open("postgres", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	summary, err := trade2stats.Sync(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(summary)
}
