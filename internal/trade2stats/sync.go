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
			if v, ok := trade2HashOverrides[e.ID]; ok {
				statID, method = v, "override"
			} else if v, ok := exact[n]; ok {
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
	valid, err := loadValidModStatIDs(ctx, db)
	if err != nil {
		return nil, nil, err
	}
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
		if _, ok := valid[statID]; !ok {
			continue
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

func loadValidModStatIDs(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT stats FROM ref.mods`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var statsRaw []byte
		if err := rows.Scan(&statsRaw); err != nil {
			return nil, err
		}
		var arr []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(statsRaw, &arr); err != nil {
			continue
		}
		for _, s := range arr {
			if strings.TrimSpace(s.ID) != "" {
				out[s.ID] = struct{}{}
			}
		}
	}
	return out, rows.Err()
}

func findStatID(norm string, exact map[string]string, pats []statPattern) string {
	if v, ok := exact[norm]; ok {
		return v
	}
	base := tokenSet(norm)
	bestID := ""
	best := 0.0
	second := 0.0
	for _, p := range pats {
		s := jaccard(base, tokenSet(p.Norm))
		if s > best {
			second = best
			best = s
			bestID = p.StatID
		} else if s > second {
			second = s
		}
	}
	if best >= 0.85 && (best-second) >= 0.05 {
		return bestID
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
	repl := map[string]string{
		"critical hit chance":   "critical strike chance",
		"critical damage bonus": "critical strike multiplier",
		"stun buildup":          "stun",
		"leeches":               "leech",
	}
	for k, v := range repl {
		s = strings.ReplaceAll(s, k, v)
	}
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

func tokenSet(s string) map[string]struct{} {
	parts := strings.Fields(s)
	out := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		if p == "#" || p == "to" || p == "of" || p == "and" || len(p) < 2 {
			continue
		}
		out[p] = struct{}{}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union <= 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func nullable(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

var trade2HashOverrides = map[string]string{
	"explicit.stat_1509134228":   "physical_damage_+%",
	"desecrated.stat_1509134228": "physical_damage_+%",
	"enchant.stat_1509134228":    "physical_damage_+%",

	"explicit.stat_3035140377": "attack_skill_gem_level_+",
	"explicit.stat_9187492":    "melee_skill_gem_level_+",

	"explicit.stat_1881230714": "base_should_have_onslaught_from_stat",

	"explicit.stat_55876295":    "local_life_leech_from_physical_damage_permyriad",
	"explicit.stat_669069897":   "local_mana_leech_from_physical_damage_permyriad",
	"desecrated.stat_669069897": "local_mana_leech_from_physical_damage_permyriad",

	"explicit.stat_387439868":   "elemental_damage_+%",
	"desecrated.stat_387439868": "elemental_damage_+%",
	"enchant.stat_387439868":    "elemental_damage_+%",

	"explicit.stat_210067635":   "local_attack_speed_+%",
	"desecrated.stat_210067635": "local_attack_speed_+%",
	"enchant.stat_210067635":    "local_attack_speed_+%",

	"explicit.stat_791928121": "stun_threshold_+%",
	"explicit.stat_748522257": "base_stun_duration_+%",

	"implicit.stat_2527686725": "shock_magnitude_+%",
	"implicit.stat_2968503605": "flammability_magnitude_+%",
	"implicit.stat_1702195217": "local_additional_block_chance_%",

	"explicit.stat_691932474": "local_accuracy_rating",

	"explicit.stat_4019237939": "gain_%_of_damage_as_extra_physical_damage",
	"explicit.stat_3015669065": "gain_%_of_damage_as_extra_fire_damage",
	"explicit.stat_2505884597": "gain_%_of_damage_as_extra_cold_damage",
	"explicit.stat_3278136794": "gain_%_of_damage_as_extra_lightning_damage",

	"desecrated.stat_1434716233": "warcry_empowers_next_x_melee_attacks",
}
