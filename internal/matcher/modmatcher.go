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
	ModID          string
	Stats          []statEntry
	GenerationType string
	Domain         string
}

type statPattern struct {
	Norm   string
	StatID string
}

type matcherStats struct{ Total, Matched, Unmatched int }

type itemHashes map[string]map[int]string // mod_type -> local index -> hash

func (m *ModMatcher) Run(ctx context.Context) (string, error) {
	exactMap, patterns, err := loadStatPatterns(ctx, m.db)
	if err != nil {
		return "", err
	}
	hashToStatID, hashKnown, err := loadTrade2HashMapFromDB(ctx, m.db)
	if err != nil {
		return "", err
	}
	if err != nil {
		return "", err
	}
	modsByStat, err := loadModsByStat(ctx, m.db)
	if err != nil {
		return "", err
	}

	rows, err := m.db.QueryContext(ctx, `
SELECT im.item_id, im.mod_type, im.sort_order, im.line_text, im.roll_values, i.payload
FROM market.item_mods im
JOIN market.items i ON i.id = im.item_id
ORDER BY im.item_id, im.mod_type, im.sort_order`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	st := matcherStats{}
	currentItemID := int64(-1)
	currentHashes := itemHashes{}
	localIdx := map[string]int{}

	for rows.Next() {
		var itemID int64
		var modType, line string
		var sortOrder int
		var rollRaw []byte
		var payloadRaw []byte
		if err := rows.Scan(&itemID, &modType, &sortOrder, &line, &rollRaw, &payloadRaw); err != nil {
			return "", err
		}
		st.Total++

		if itemID != currentItemID {
			currentItemID = itemID
			currentHashes = extractItemHashes(payloadRaw)
			localIdx = map[string]int{}
		}

		idx := localIdx[modType]
		localIdx[modType] = idx + 1
		hash := currentHashes[modType][idx]

		norm := normalizeLine(line)
		statID := ""
		mappedByHash := false
		if hash != "" {
			if s, ok := hashToStatID[hash]; ok {
				statID = s
				mappedByHash = true
			}
		}
		if statID == "" {
			statID = findStatID(norm, exactMap, patterns)
		}

		if statID == "" {
			reason := "unknown_stat_id"
			if hash == "" {
				reason = "text_only_fallback"
			} else if _, ok := hashKnown[hash]; ok {
				reason = "unknown_stat_id"
			} else {
				reason = "missing_hash_in_ref"
			}
			if err := setUnmatched(ctx, m.db, itemID, modType, sortOrder, hash, reason); err != nil {
				return "", err
			}
			st.Unmatched++
			continue
		}

		candidates := modsByStat[statID]
		if len(candidates) == 0 {
			reason := "missing_hash_in_ref"
			if modType == "rune" {
				reason = "rune_unmapped"
			} else if modType == "desecrated" {
				reason = "desecrated_unmapped"
			}
			if err := setUnmatched(ctx, m.db, itemID, modType, sortOrder, hash, reason); err != nil {
				return "", err
			}
			st.Unmatched++
			continue
		}

		mod := pickBestCandidate(candidates, modType, rollRaw)
		rollPcts := computeRollPcts(mod.Stats, rollRaw)
		rollPctsJSON, _ := json.Marshal(rollPcts)
		reason := "hash_matched"
		if !mappedByHash {
			reason = "text_only_fallback"
		}
		if len(rollPcts) > 0 && len(mod.Stats) > len(rollPcts) {
			reason = "hybrid_partial"
		}

		if _, err := m.db.ExecContext(ctx, `
UPDATE market.item_mods
SET mod_id=$1, match_status='matched', matched_text=$2, roll_pcts=$3, stat_hash=$4, match_reason=$5
WHERE item_id=$6 AND mod_type=$7 AND sort_order=$8`,
			mod.ModID, statID, rollPctsJSON, nullableStr(hash), reason, itemID, modType, sortOrder,
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

func extractItemHashes(payloadRaw []byte) itemHashes {
	out := itemHashes{}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return out
	}
	extRaw, ok := payload["extended"]
	if !ok {
		return out
	}
	var ext map[string]json.RawMessage
	if err := json.Unmarshal(extRaw, &ext); err != nil {
		return out
	}
	hashesRaw, ok := ext["hashes"]
	if !ok {
		return out
	}
	var hByType map[string][]json.RawMessage
	if err := json.Unmarshal(hashesRaw, &hByType); err != nil {
		return out
	}
	for t, arr := range hByType {
		m := map[int]string{}
		for _, pairRaw := range arr {
			var pair []json.RawMessage
			if json.Unmarshal(pairRaw, &pair) != nil || len(pair) < 2 {
				continue
			}
			h := jsonString(pair[0])
			var idxs []int
			if json.Unmarshal(pair[1], &idxs) != nil {
				continue
			}
			for _, idx := range idxs {
				if _, exists := m[idx]; !exists && h != "" {
					m[idx] = h
				}
			}
		}
		out[t] = m
	}
	return out
}

func loadTrade2HashMapFromDB(ctx context.Context, db *sql.DB) (map[string]string, map[string]struct{}, error) {
	out := map[string]string{}
	known := map[string]struct{}{}
	rows, err := db.QueryContext(ctx, `SELECT hash_id, stat_id FROM ref.trade2_stats`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		var statID sql.NullString
		if err := rows.Scan(&hash, &statID); err != nil {
			return nil, nil, err
		}
		known[hash] = struct{}{}
		if statID.Valid && strings.TrimSpace(statID.String) != "" {
			out[hash] = statID.String
		}
	}
	return out, known, rows.Err()
}

func nullableStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func pickBestCandidate(cands []modMeta, modType string, rollRaw []byte) modMeta {
	if len(cands) == 1 {
		return cands[0]
	}
	rolls := parseRolls(rollRaw)
	best := cands[0]
	bestScore := scoreCandidate(best, modType, rolls)
	for _, c := range cands[1:] {
		s := scoreCandidate(c, modType, rolls)
		if s > bestScore || (s == bestScore && c.ModID < best.ModID) {
			best = c
			bestScore = s
		}
	}
	return best
}

func scoreCandidate(c modMeta, modType string, rolls []float64) float64 {
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
	score += modTypeBonus(modType, c)
	return score
}

func modTypeBonus(modType string, c modMeta) float64 {
	switch modType {
	case "desecrated":
		if c.Domain == "desecrated" {
			return 2.0
		}
		return -0.5
	case "explicit":
		switch c.GenerationType {
		case "prefix", "suffix", "unique", "corrupted", "essence", "talisman":
			return 0.5
		default:
			return -0.2
		}
	case "implicit":
		if c.GenerationType == "prefix" || c.GenerationType == "suffix" {
			return -0.3
		}
		return 0.1
	case "rune":
		if strings.Contains(strings.ToLower(c.ModID), "rune") {
			return 0.6
		}
		return -0.1
	default:
		return 0
	}
}

func setUnmatched(ctx context.Context, db *sql.DB, itemID int64, modType string, sortOrder int, hash string, reason string) error {
	_, err := db.ExecContext(ctx, `
UPDATE market.item_mods
SET mod_id=NULL, match_status='unmatched', matched_text=NULL, roll_pcts='[]'::jsonb, stat_hash=$4, match_reason=$5
WHERE item_id=$1 AND mod_type=$2 AND sort_order=$3`, itemID, modType, sortOrder, nullableStr(hash), reason)
	return err
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
	for _, p := range pats {
		if strings.Contains(norm, p.Norm) || strings.Contains(p.Norm, norm) {
			return p.StatID
		}
	}

	baseTokens := tokenSet(norm)
	bestID := ""
	bestScore := 0.0
	for _, p := range pats {
		s := jaccard(baseTokens, tokenSet(p.Norm))
		if s > bestScore {
			bestScore = s
			bestID = p.StatID
		}
	}
	if bestScore >= 0.50 {
		return bestID
	}
	return ""
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

func loadModsByStat(ctx context.Context, db *sql.DB) (map[string][]modMeta, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, stats, generation_type, domain FROM ref.mods`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]modMeta{}
	for rows.Next() {
		var modID, generationType, domain string
		var statsRaw []byte
		if err := rows.Scan(&modID, &statsRaw, &generationType, &domain); err != nil {
			return nil, err
		}
		var stats []statEntry
		if err := json.Unmarshal(statsRaw, &stats); err != nil || len(stats) == 0 {
			continue
		}
		meta := modMeta{ModID: modID, Stats: stats, GenerationType: generationType, Domain: domain}
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
	if i := strings.Index(s, ":"); i >= 0 && i < 40 {
		s = strings.TrimSpace(s[i+1:])
	}
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

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
