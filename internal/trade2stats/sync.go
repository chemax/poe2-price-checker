package trade2stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type statPattern struct {
	Norm   string
	StatID string
}

type payload struct {
	Result []struct {
		ID      string `json:"id"`
		Entries []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"entries"`
	} `json:"result"`
}

func Sync(ctx context.Context, db *sql.DB) (string, error) {
	exact, patterns, err := loadStatPatterns(ctx, db)
	if err != nil {
		return "", err
	}
	pl, err := fetchTrade2Stats(ctx)
	if err != nil {
		return "", err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	total, mapped := 0, 0
	now := time.Now().UTC()
	for _, g := range pl.Result {
		for _, e := range g.Entries {
			if !strings.Contains(e.ID, ".stat_") {
				continue
			}
			total++
			n := normalizeLine(e.Text)
			statID := ""
			method := "unmapped"
			if v, ok := exact[n]; ok {
				statID, method = v, "exact"
			} else if v := findStatID(n, exact, patterns); v != "" {
				statID, method = v, "fuzzy"
			}
			if statID != "" {
				mapped++
			}
			_, err := tx.ExecContext(ctx, `
INSERT INTO ref.trade2_stats(hash_id, stat_type, trade_text, normalized_text, stat_id, map_method, source_ts, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,NOW())
ON CONFLICT (hash_id) DO UPDATE SET
 stat_type=EXCLUDED.stat_type,
 trade_text=EXCLUDED.trade_text,
 normalized_text=EXCLUDED.normalized_text,
 stat_id=EXCLUDED.stat_id,
 map_method=EXCLUDED.map_method,
 source_ts=EXCLUDED.source_ts,
 updated_at=NOW()`, e.ID, e.Type, e.Text, n, nullable(statID), method, now)
			if err != nil {
				return "", err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	pct := 0.0
	if total > 0 {
		pct = float64(mapped) * 100 / float64(total)
	}
	return fmt.Sprintf("trade2 stats sync: total=%d mapped=%d mapped_pct=%.2f", total, mapped, pct), nil
}

func fetchTrade2Stats(ctx context.Context) (payload, error) {
	var out payload
	cookie := strings.TrimSpace(os.Getenv("POESESSID"))
	cf := strings.TrimSpace(os.Getenv("CF_CLEARANCE"))
	if cookie == "" || cf == "" {
		return out, fmt.Errorf("POESESSID and CF_CLEARANCE are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.pathofexile.com/api/trade2/data/stats", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "https://www.pathofexile.com")
	req.Header.Set("Referer", "https://www.pathofexile.com/trade2/search/poe2/Standard")
	req.Header.Set("Cookie", "cf_clearance="+cf+"; POESESSID="+cookie)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("trade2 stats status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func loadStatPatterns(ctx context.Context, db *sql.DB) (map[string]string, []statPattern, error) {
	rows, err := db.QueryContext(ctx, `SELECT stat_id, english_text FROM ref.stat_translations WHERE english_text IS NOT NULL AND english_text <> ''`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	exact := map[string]string{}
	patterns := make([]statPattern, 0, 10000)
	for rows.Next() {
		var statID, text string
		if err := rows.Scan(&statID, &text); err != nil {
			return nil, nil, err
		}
		n := normalizeLine(text)
		if n == "" {
			continue
		}
		if _, exists := exact[n]; !exists {
			exact[n] = statID
		}
		patterns = append(patterns, statPattern{Norm: n, StatID: statID})
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i].Norm) > len(patterns[j].Norm) })
	return exact, patterns, rows.Err()
}

func findStatID(norm string, exact map[string]string, pats []statPattern) string {
	if v, ok := exact[norm]; ok {
		return v
	}
	for _, p := range pats {
		if strings.Contains(norm, p.Norm) || strings.Contains(p.Norm, norm) {
			return p.StatID
		}
	}
	return ""
}

var (
	reBracket = regexp.MustCompile(`\[([^\]|]+\|)?([^\]]+)\]`)
	reParam   = regexp.MustCompile(`\{\d+\}`)
	reNumber  = regexp.MustCompile(`[-+]?\d+(?:[\.,]\d+)?`)
	reSpaces  = regexp.MustCompile(`\s+`)
)

func normalizeLine(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\n", " ")
	s = reBracket.ReplaceAllString(s, `$2`)
	s = reParam.ReplaceAllString(s, "#")
	s = reNumber.ReplaceAllString(s, "#")
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, "+", "")
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

func nullable(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
