package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	var (
		path = flag.String("path", "file://migrations", "migration path")
		dsn  = flag.String("dsn", os.Getenv("DATABASE_DSN"), "postgres DSN")
		cmd  = flag.String("cmd", "up", "migration command: up|down|force|version")
		n    = flag.Int("n", 1, "steps for down or version for force")
	)
	flag.Parse()

	if *dsn == "" {
		log.Fatal("dsn is required (flag -dsn or env DATABASE_DSN)")
	}

	m, err := migrate.New(*path, *dsn)
	if err != nil {
		log.Fatalf("migrate init: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	switch *cmd {
	case "up":
		err = m.Up()
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("no change")
			return
		}
	case "down":
		err = m.Steps(-*n)
	case "force":
		err = m.Force(*n)
	case "version":
		v, dirty, verr := m.Version()
		if verr != nil {
			if errors.Is(verr, migrate.ErrNilVersion) {
				fmt.Println("version: nil")
				return
			}
			log.Fatalf("version: %v", verr)
		}
		fmt.Printf("version: %d dirty=%v\n", v, dirty)
		return
	default:
		log.Fatalf("unknown cmd: %s", *cmd)
	}

	if err != nil {
		log.Fatalf("migration %s failed: %v", *cmd, err)
	}
	fmt.Printf("migration %s ok\n", *cmd)
}
