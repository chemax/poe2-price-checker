package pricing

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

func StartAutoSync(ctx context.Context, db *sql.DB, league, poesessid, cfClearance string, every time.Duration) {
	if every <= 0 {
		every = 10 * time.Minute
	}
	if strings.TrimSpace(poesessid) == "" {
		log.Printf("pricing sync disabled: missing POESESSID")
		return
	}

	client := NewExchangeClient(league, poesessid, cfClearance)
	currencies := []string{"chaos", "exalted", "alchemy", "annul", "vaal", "regal", "artificer", "chance"}

	run := func() {
		rates, err := client.FetchDivineRates(ctx, currencies)
		if err != nil {
			log.Printf("pricing sync error: %v", err)
			return
		}
		if err := SaveRates(ctx, db, rates, time.Now().UTC()); err != nil {
			log.Printf("pricing save error: %v", err)
			return
		}
		log.Printf("pricing sync ok: rates=%d", len(rates))
	}

	run()
	ticker := time.NewTicker(every)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
