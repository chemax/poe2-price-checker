package market

import "encoding/json"

type ItemMod struct {
	ModID      *string
	ModType    string
	LineText   string
	RollValues json.RawMessage
	SortOrder  int
}

type Item struct {
	TradeItemID   string
	RefBaseItemID *string
	Rarity        *string
	ItemLevel     *int
	Identified    *bool
	Corrupted     *bool
	Mirrored      *bool
	Sockets       json.RawMessage
	Properties    json.RawMessage
	Requirements  json.RawMessage
	Payload       json.RawMessage
	Mods          []ItemMod
}

type Listing struct {
	TradeListingID string
	LeagueSlug     string
	AccountName    *string
	PriceCurrency  *string
	PriceAmount    *string
	IndexedAt      *string
	Whisper        *string
	StashName      *string
	PositionX      *int
	PositionY      *int
	Payload        json.RawMessage
	Item           Item
}
