package matcher

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

type ModMatcher struct{ db *sql.DB }

func NewModMatcher(db *sql.DB) *ModMatcher { return &ModMatcher{db: db} }

type statEntry struct {
	ID  string  `json:"id"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type modMeta struct {
	ModID string
	Stats []statEntry
}

type statPattern struct {
	Norm   string
	StatID string
}

type matcherStats struct{ Total, Matched, Unmatched int }

func (m *ModMatcher) Run(ctx context.Context) (string, error) {
	exactMap, patterns, err := loadStatPatterns(ctx, m.db)
	if err != nil {
		return "", err
	}
	modsByStat, err := loadModsByStat(ctx, m.db)
	if err != nil {
		return "", err
	}

	rows, err := m.db.QueryContext(ctx, `
SELECT item_id, mod_type, sort_order, line_text, roll_values
FROM market.item_mods
ORDER BY item_id, sort_order`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	st := matcherStats{}
	for rows.Next() {
		var itemID int64
		var modType, line string
		var sortOrder int
		var rollRaw []byte
		if err := rows.Scan(&itemID, &modType, &sortOrder, &line, &rollRaw); err != nil {
			return "", err
		}
		st.Total++

		norm := normalizeLine(line)
		statID := findStatID(norm, exactMap, patterns)
		if statID == "" {
			if err := setUnmatched(ctx, m.db, itemID, modType, sortOrder); err != nil {
				return "", err
			}
			st.Unmatched++
			continue
		}

		candidates := modsByStat[statID]
		if len(candidates) == 0 {
			if err := setUnmatched(ctx, m.db, itemID, modType, sortOrder); err != nil {
				return "", err
			}
			st.Unmatched++
			continue
		}

		mod := pickBestCandidate(candidates, rollRaw)
		rollPcts := computeRollPcts(mod.Stats, rollRaw)
		rollPctsJSON, _ := json.Marshal(rollPcts)
		if _, err := m.db.ExecContext(ctx, `
UPDATE market.item_mods
SET mod_id=$1, match_status='matched', matched_text=$2, roll_pcts=$3
WHERE item_id=$4 AND mod_type=$5 AND sort_order=$6`,
			mod.ModID, statID, rollPctsJSON, itemID, modType, sortOrder,
		); err != nil {
			return "", err
		}
		st.Matched++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	return fmt.Sprintf("mod matcher: total=%d matched=%d unmatched=%d matched_pct=%.2f", st.Total, st.Matched, st.Unmatched, percent(st.Matched, st.Total)), nil
}

func pickBestCandidate(cands []modMeta, rollRaw []byte) modMeta {
	if len(cands) == 1 {
		return cands[0]
	}
	rolls := parseRolls(rollRaw)
	best := cands[0]
	bestScore := scoreCandidate(best, rolls)
	for _, c := range cands[1:] {
		s := scoreCandidate(c, rolls)
		if s > bestScore || (s == bestScore && c.ModID < best.ModID) {
			best = c
			bestScore = s
		}
	}
	return best
}

func scoreCandidate(c modMeta, rolls []float64) float64 {
	if len(rolls) == 0 {
		return -float64(len(c.Stats))
	}
	n := len(rolls)
	if len(c.Stats) < n {
		n = len(c.Stats)
	}
	score := 0.0
	for i := 0; i < n; i++ {
		mn, mx, v := c.Stats[i].Min, c.Stats[i].Max, rolls[i]
		if mx <= mn {
			continue
		}
		if v >= mn && v <= mx {
			score += 2
		} else {
			d := math.Min(math.Abs(v-mn), math.Abs(v-mx))
			score += math.Max(0, 1-(d/(mx-mn)))
		}
	}
	if len(c.Stats) == len(rolls) {
		score += 0.5
	}
	return score
}

func setUnmatched(ctx context.Context, db *sql.DB, itemID int64, modType string, sortOrder int) error {
	_, err := db.ExecContext(ctx, `
UPDATE market.item_mods
SET mod_id=NULL, match_status='unmatched', matched_text=NULL, roll_pcts='[]'::jsonb
WHERE item_id=$1 AND mod_type=$2 AND sort_order=$3`, itemID, modType, sortOrder)
	return err
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

func loadModsByStat(ctx context.Context, db *sql.DB) (map[string][]modMeta, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, stats FROM ref.mods`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]modMeta{}
	for rows.Next() {
		var modID string
		var statsRaw []byte
		if err := rows.Scan(&modID, &statsRaw); err != nil {
			return nil, err
		}
		var stats []statEntry
		if err := json.Unmarshal(statsRaw, &stats); err != nil || len(stats) == 0 {
			continue
		}
		meta := modMeta{ModID: modID, Stats: stats}
		for _, s := range stats {
			out[s.ID] = append(out[s.ID], meta)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].ModID < out[k][j].ModID })
	}
	return out, nil
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

func computeRollPcts(stats []statEntry, rollRaw []byte) []float64 {
	rolls := parseRolls(rollRaw)
	if len(rolls) == 0 || len(stats) == 0 {
		return []float64{}
	}
	out := make([]float64, 0, len(rolls))
	if len(stats) == 1 {
		for _, v := range rolls {
			if pct, ok := calcPct(v, stats[0].Min, stats[0].Max); ok {
				out = append(out, pct)
			}
		}
		return out
	}
	n := len(rolls)
	if len(stats) < n {
		n = len(stats)
	}
	for i := 0; i < n; i++ {
		if pct, ok := calcPct(rolls[i], stats[i].Min, stats[i].Max); ok {
			out = append(out, pct)
		}
	}
	return out
}

func parseRolls(raw []byte) []float64 {
	var rolls []float64
	_ = json.Unmarshal(raw, &rolls)
	return rolls
}

func calcPct(v, minV, maxV float64) (float64, bool) {
	if maxV <= minV {
		return 0, false
	}
	pct := (v - minV) / (maxV - minV)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	f, _ := strconv.ParseFloat(fmt.Sprintf("%.4f", pct), 64)
	return f, true
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
