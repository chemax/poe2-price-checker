package pricing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ExchangeClient struct {
	httpClient   *http.Client
	baseURL      string
	league       string
	cookieHeader string
}

func NewExchangeClient(league, poesessid, cfClearance string) *ExchangeClient {
	cookieHeader := "POESESSID=" + poesessid
	if strings.TrimSpace(cfClearance) != "" {
		cookieHeader += "; cf_clearance=" + strings.TrimSpace(cfClearance)
	}
	return &ExchangeClient{
		httpClient:   &http.Client{Timeout: 20 * time.Second},
		baseURL:      "https://www.pathofexile.com",
		league:       league,
		cookieHeader: cookieHeader,
	}
}

type exchangeResp struct {
	Result map[string]struct {
		Listing struct {
			Offers []struct {
				Exchange struct {
					Currency string  `json:"currency"`
					Amount   float64 `json:"amount"`
				} `json:"exchange"`
				Item struct {
					Currency string  `json:"currency"`
					Amount   float64 `json:"amount"`
				} `json:"item"`
			} `json:"offers"`
		} `json:"listing"`
	} `json:"result"`
}

func (c *ExchangeClient) FetchDivineRates(ctx context.Context, currencies []string) (map[string]float64, error) {
	rates := map[string]float64{"divine": 1.0}

	for _, cur := range currencies {
		cur = strings.TrimSpace(strings.ToLower(cur))
		if cur == "" || cur == "divine" {
			continue
		}
		rate, err := c.fetchOne(ctx, cur)
		if err != nil {
			continue
		}
		if rate > 0 {
			rates[cur] = rate
		}
	}
	return rates, nil
}

func (c *ExchangeClient) fetchOne(ctx context.Context, currency string) (float64, error) {
	body := map[string]any{
		"query": map[string]any{
			"status": map[string]any{"option": "online"},
			"have":   []string{currency},
			"want":   []string{"divine"},
		},
		"sort": map[string]any{"have": "asc"},
	}
	b, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/api/trade2/exchange/poe2/%s", c.baseURL, url.PathEscape(c.league))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	applyHeaders(req, c.baseURL, c.league, c.cookieHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", fmt.Sprintf("%s/trade2/exchange/poe2/%s", c.baseURL, url.PathEscape(c.league)))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("exchange status %d", resp.StatusCode)
	}

	var payload exchangeResp
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	vals := make([]float64, 0, len(payload.Result))
	for _, r := range payload.Result {
		if len(r.Listing.Offers) == 0 {
			continue
		}
		o := r.Listing.Offers[0]
		if strings.ToLower(o.Exchange.Currency) != currency || strings.ToLower(o.Item.Currency) != "divine" {
			continue
		}
		if o.Exchange.Amount <= 0 || o.Item.Amount <= 0 {
			continue
		}
		vals = append(vals, o.Item.Amount/o.Exchange.Amount)
	}
	if len(vals) == 0 {
		return 0, fmt.Errorf("no rate for %s", currency)
	}
	sort.Float64s(vals)
	return vals[len(vals)/2], nil
}

func SaveRates(ctx context.Context, db *sql.DB, rates map[string]float64, fetchedAt time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for cur, v := range rates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO market.currency_rates(currency, divine_value, fetched_at, source)
VALUES($1,$2,$3,'ggg.exchange')`, cur, v, fetchedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func applyHeaders(req *http.Request, baseURL, league, cookieHeader string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", cookieHeader)
}
