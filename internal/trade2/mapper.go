package trade2

import (
	"encoding/json"
	"fmt"
	"strconv"

	"poe2-price-checker/internal/market"
)

func MapTradeEntryToListing(raw json.RawMessage, league string) (market.Listing, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return market.Listing{}, err
	}
	var listingObj map[string]json.RawMessage
	var itemObj map[string]json.RawMessage
	if err := json.Unmarshal(root["listing"], &listingObj); err != nil {
		return market.Listing{}, fmt.Errorf("listing: %w", err)
	}
	if err := json.Unmarshal(root["item"], &itemObj); err != nil {
		return market.Listing{}, fmt.Errorf("item: %w", err)
	}

	tradeListingID := jsonString(listingObj["id"])
	tradeItemID := jsonString(itemObj["id"])
	if tradeListingID == "" || tradeItemID == "" {
		return market.Listing{}, fmt.Errorf("missing ids")
	}

	priceCurrency, priceAmount := extractPrice(listingObj["price"])
	indexedAt := jsonStringPtr(listingObj["indexed"])
	whisper := jsonStringPtr(listingObj["whisper"])
	stash := jsonStringPtr(listingObj["stash"])
	acc := jsonStringPtr(listingObj["accountName"])
	if acc == nil {
		acc = jsonStringPtr(listingObj["account"]) // fallback
	}

	x := jsonIntPtr(itemObj["x"])
	y := jsonIntPtr(itemObj["y"])
	baseID := jsonStringPtr(itemObj["baseType"])

	mods := collectMods(itemObj)
	rarity := jsonStringPtr(itemObj["rarity"])
	ilvl := jsonIntPtr(itemObj["ilvl"])
	identified := jsonBoolPtr(itemObj["identified"])
	corrupted := jsonBoolPtr(itemObj["corrupted"])
	mirrored := jsonBoolPtr(itemObj["mirrored"])

	sockets := getOrDefault(itemObj, "sockets", []byte("[]"))
	props := getOrDefault(itemObj, "properties", []byte("{}"))
	reqs := getOrDefault(itemObj, "requirements", []byte("{}"))

	return market.Listing{
		TradeListingID: tradeListingID,
		LeagueSlug:     league,
		AccountName:    acc,
		PriceCurrency:  priceCurrency,
		PriceAmount:    priceAmount,
		IndexedAt:      indexedAt,
		Whisper:        whisper,
		StashName:      stash,
		PositionX:      x,
		PositionY:      y,
		Payload:        raw,
		Item: market.Item{
			TradeItemID:   tradeItemID,
			RefBaseItemID: baseID,
			Rarity:        rarity,
			ItemLevel:     ilvl,
			Identified:    identified,
			Corrupted:     corrupted,
			Mirrored:      mirrored,
			Sockets:       sockets,
			Properties:    props,
			Requirements:  reqs,
			Payload:       root["item"],
			Mods:          mods,
		},
	}, nil
}

func collectMods(item map[string]json.RawMessage) []market.ItemMod {
	var out []market.ItemMod
	types := []struct {
		key string
		mod string
	}{{"implicitMods", "implicit"}, {"explicitMods", "explicit"}, {"enchantMods", "enchant"}, {"craftedMods", "crafted"}}
	order := 1
	for _, t := range types {
		var lines []string
		if err := json.Unmarshal(item[t.key], &lines); err != nil {
			continue
		}
		for _, line := range lines {
			out = append(out, market.ItemMod{ModType: t.mod, LineText: line, RollValues: []byte("[]"), SortOrder: order})
			order++
		}
	}
	return out
}

func extractPrice(raw json.RawMessage) (*string, *string) {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil
	}
	currency := jsonStringPtr(p["currency"])
	if amt := jsonStringPtr(p["amount"]); amt != nil {
		return currency, amt
	}
	var f float64
	if err := json.Unmarshal(p["amount"], &f); err == nil {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		return currency, &s
	}
	return currency, nil
}

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
func jsonStringPtr(raw json.RawMessage) *string {
	s := jsonString(raw)
	if s == "" {
		return nil
	}
	return &s
}
func jsonBoolPtr(raw json.RawMessage) *bool {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil
	}
	return &b
}
func jsonIntPtr(raw json.RawMessage) *int {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return &n
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		n = int(f)
		return &n
	}
	return nil
}
func getOrDefault(m map[string]json.RawMessage, key string, d []byte) []byte {
	if v, ok := m[key]; ok && len(v) > 0 {
		return v
	}
	return d
}
