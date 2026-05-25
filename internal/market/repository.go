package market

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

func (r *Repository) InsertSearchRun(ctx context.Context, leagueSlug, queryName, queryID, queryHash string, responsePayload []byte) error {
	leagueID, err := r.EnsureLeague(ctx, leagueSlug)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO market.search_runs(league_id,query_name,query_id,query_hash,requested_at,response_payload)
VALUES($1,$2,$3,$4,NOW(),$5)
`, leagueID, queryName, queryID, queryHash, responsePayload)
	return err
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) EnsureLeague(ctx context.Context, slug string) (int64, error) {
	return ensureLeagueDB(ctx, r.db, slug)
}

func (r *Repository) UpsertListing(ctx context.Context, in Listing) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	leagueID, err := ensureLeague(ctx, tx, in.LeagueSlug)
	if err != nil {
		return err
	}

	itemID, err := upsertItem(ctx, tx, in.Item)
	if err != nil {
		return err
	}
	if err := replaceItemMods(ctx, tx, itemID, in.Item.Mods); err != nil {
		return err
	}

	priceDivine, err := resolvePriceDivine(ctx, tx, in.PriceCurrency, in.PriceAmount, in.IndexedAt)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO market.listings(
  trade_listing_id,item_id,league_id,account_name,price_currency,price_amount,price_divine,indexed_at,whisper,stash_name,position_x,position_y,payload,seen_at,first_seen_at,last_seen_at,seen_count,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),NOW(),NOW(),1,NOW())
ON CONFLICT (trade_listing_id) DO UPDATE SET
  item_id=EXCLUDED.item_id, league_id=EXCLUDED.league_id, account_name=EXCLUDED.account_name,
  price_currency=EXCLUDED.price_currency, price_amount=EXCLUDED.price_amount, price_divine=EXCLUDED.price_divine, indexed_at=EXCLUDED.indexed_at,
  whisper=EXCLUDED.whisper, stash_name=EXCLUDED.stash_name, position_x=EXCLUDED.position_x, position_y=EXCLUDED.position_y,
  payload=EXCLUDED.payload, seen_at=NOW(), last_seen_at=NOW(), seen_count=market.listings.seen_count+1, updated_at=NOW()`,
		in.TradeListingID, itemID, leagueID, in.AccountName, in.PriceCurrency, in.PriceAmount, priceDivine, ParseTimePtrRFC3339(in.IndexedAt),
		in.Whisper, in.StashName, in.PositionX, in.PositionY, in.Payload,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func ensureLeague(ctx context.Context, tx *sql.Tx, slug string) (int64, error) {
	if slug == "" {
		return 0, fmt.Errorf("league slug is required")
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO market.leagues(slug) VALUES($1)
ON CONFLICT (slug) DO UPDATE SET is_active=TRUE
RETURNING id`, slug).Scan(&id)
	return id, err
}

func ensureLeagueDB(ctx context.Context, db *sql.DB, slug string) (int64, error) {
	if slug == "" {
		return 0, fmt.Errorf("league slug is required")
	}
	var id int64
	err := db.QueryRowContext(ctx, `
INSERT INTO market.leagues(slug) VALUES($1)
ON CONFLICT (slug) DO UPDATE SET is_active=TRUE
RETURNING id`, slug).Scan(&id)
	return id, err
}

func upsertItem(ctx context.Context, tx *sql.Tx, in Item) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO market.items(
 trade_item_id,ref_base_item_id,rarity,item_level,identified,corrupted,mirrored,sockets,properties,requirements,payload,updated_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
ON CONFLICT (trade_item_id) DO UPDATE SET
 ref_base_item_id=EXCLUDED.ref_base_item_id,rarity=EXCLUDED.rarity,item_level=EXCLUDED.item_level,
 identified=EXCLUDED.identified,corrupted=EXCLUDED.corrupted,mirrored=EXCLUDED.mirrored,
 sockets=EXCLUDED.sockets,properties=EXCLUDED.properties,requirements=EXCLUDED.requirements,payload=EXCLUDED.payload,
 updated_at=NOW()
RETURNING id`,
		in.TradeItemID, in.RefBaseItemID, in.Rarity, in.ItemLevel, in.Identified, in.Corrupted, in.Mirrored,
		in.Sockets, in.Properties, in.Requirements, in.Payload,
	).Scan(&id)
	return id, err
}

func replaceItemMods(ctx context.Context, tx *sql.Tx, itemID int64, mods []ItemMod) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM market.item_mods WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for i, m := range mods {
		sortOrder := m.SortOrder
		if sortOrder == 0 {
			sortOrder = i + 1
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO market.item_mods(item_id,mod_id,mod_type,line_text,roll_values,sort_order)
VALUES($1,$2,$3,$4,$5,$6)`, itemID, m.ModID, m.ModType, m.LineText, m.RollValues, sortOrder)
		if err != nil {
			return err
		}
	}
	return nil
}

func resolvePriceDivine(ctx context.Context, tx *sql.Tx, currency, amount, indexedAt *string) (any, error) {
	if currency == nil || amount == nil || *currency == "" || *amount == "" {
		return nil, nil
	}
	if *currency == "divine" {
		return *amount, nil
	}

	at := time.Now().UTC()
	if parsed := ParseTimePtrRFC3339(indexedAt); parsed != nil {
		if t, ok := parsed.(time.Time); ok {
			at = t.UTC()
		}
	}

	var rate string
	err := tx.QueryRowContext(ctx, `
SELECT divine_value::text
FROM market.currency_rates
WHERE currency=$1 AND fetched_at <= $2
ORDER BY fetched_at DESC
LIMIT 1`, *currency, at).Scan(&rate)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	amt, err := strconv.ParseFloat(*amount, 64)
	if err != nil {
		return nil, nil
	}
	r, err := strconv.ParseFloat(rate, 64)
	if err != nil {
		return nil, nil
	}
	v := amt * r
	return strconv.FormatFloat(v, 'f', 8, 64), nil
}

func ParseTimePtrRFC3339(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil
	}
	return t
}
