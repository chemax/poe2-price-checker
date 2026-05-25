package refsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/lib/pq"
)

var requiredFiles = []string{
	"base_items.json",
	"item_classes.json",
	"tags.json",
	"mods.json",
	"mods_by_base.json",
	"stat_translations.json",
}

func Run(ctx context.Context, dsn, baseURL string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	blobs, checksum, err := fetchAll(ctx, strings.TrimRight(baseURL, "/"))
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	datasetID, err := upsertDataset(ctx, tx, baseURL, checksum)
	if err != nil {
		return err
	}

	if err := syncItemClasses(ctx, tx, datasetID, blobs["item_classes.json"]); err != nil {
		return err
	}
	if err := syncTags(ctx, tx, datasetID, blobs["tags.json"]); err != nil {
		return err
	}
	if err := syncBaseItems(ctx, tx, datasetID, blobs["base_items.json"]); err != nil {
		return err
	}
	if err := syncMods(ctx, tx, datasetID, blobs["mods.json"]); err != nil {
		return err
	}
	baseIDs, err := loadIDSet(ctx, tx, "ref.base_items")
	if err != nil {
		return err
	}
	modIDs, err := loadIDSet(ctx, tx, "ref.mods")
	if err != nil {
		return err
	}
	if err := syncModsByBase(ctx, tx, datasetID, blobs["mods_by_base.json"], baseIDs, modIDs); err != nil {
		return err
	}
	if err := syncStatTranslations(ctx, tx, datasetID, blobs["stat_translations.json"]); err != nil {
		return err
	}

	return tx.Commit()
}

func fetchAll(ctx context.Context, baseURL string) (map[string][]byte, string, error) {
	client := &http.Client{}
	result := make(map[string][]byte, len(requiredFiles))
	h := sha256.New()

	files := append([]string(nil), requiredFiles...)
	sort.Strings(files)

	for _, f := range files {
		b, err := fetchFirstAvailable(ctx, client, candidateURLs(baseURL, f))
		if err != nil {
			return nil, "", err
		}
		result[f] = b
		h.Write([]byte(f))
		h.Write(b)
	}
	return result, hex.EncodeToString(h.Sum(nil)), nil
}

func fetchFirstAvailable(ctx context.Context, client *http.Client, urls []string) ([]byte, error) {
	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			b, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr != nil {
				return nil, rerr
			}
			return b, nil
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			lastErr = fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all candidates returned 404: %v", urls)
	}
	return nil, lastErr
}

func candidateURLs(baseURL, file string) []string {
	base := strings.TrimRight(baseURL, "/")
	root := base
	if strings.HasSuffix(base, "/poe2") {
		root = strings.TrimSuffix(base, "/poe2")
	}
	return uniq([]string{
		base + "/" + file,
		base + "/" + strings.TrimSuffix(file, ".json") + ".min.json",
		root + "/" + file,
		root + "/" + strings.TrimSuffix(file, ".json") + ".min.json",
	})
}

func upsertDataset(ctx context.Context, tx *sql.Tx, sourceURL, checksum string) (int64, error) {
	version := checksum[:12]
	var id int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO ref.datasets (source_name, source_url, version, checksum)
VALUES ('repoe-fork', $1, $2, $3)
ON CONFLICT (source_name, version, checksum) DO UPDATE
SET source_url = EXCLUDED.source_url,
    fetched_at = NOW()
RETURNING id`, sourceURL, version, checksum).Scan(&id)
	return id, err
}

func syncItemClasses(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	q := `INSERT INTO ref.item_classes(id,name,dataset_id,payload)
VALUES($1,$2,$3,$4)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,dataset_id=EXCLUDED.dataset_id,payload=EXCLUDED.payload`
	for id, payload := range m {
		name := jsonFieldString(payload, "name")
		if name == "" {
			name = id
		}
		if _, err := tx.ExecContext(ctx, q, id, name, datasetID, payload); err != nil {
			return err
		}
	}
	return nil
}

func syncTags(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte) error {
	q := `INSERT INTO ref.tags(id,name,dataset_id,payload)
VALUES($1,$2,$3,$4)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,dataset_id=EXCLUDED.dataset_id,payload=EXCLUDED.payload`

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err == nil {
		for id, payload := range m {
			name := jsonFieldString(payload, "name")
			if name == "" {
				name = id
			}
			if _, err := tx.ExecContext(ctx, q, id, name, datasetID, payload); err != nil {
				return err
			}
		}
		return nil
	}

	var a []string
	if err := json.Unmarshal(raw, &a); err == nil {
		for _, tag := range uniq(a) {
			payload, _ := json.Marshal(map[string]string{"name": tag})
			if _, err := tx.ExecContext(ctx, q, tag, tag, datasetID, payload); err != nil {
				return err
			}
		}
		return nil
	}

	return fmt.Errorf("unsupported tags.json format")
}

func syncBaseItems(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	q := `INSERT INTO ref.base_items(id,name,item_class_id,required_level,width,height,tags,requirements,properties,payload,dataset_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (id) DO UPDATE SET
name=EXCLUDED.name,item_class_id=EXCLUDED.item_class_id,required_level=EXCLUDED.required_level,width=EXCLUDED.width,height=EXCLUDED.height,
tags=EXCLUDED.tags,requirements=EXCLUDED.requirements,properties=EXCLUDED.properties,payload=EXCLUDED.payload,dataset_id=EXCLUDED.dataset_id`
	for id, payload := range m {
		name := jsonFieldString(payload, "name")
		if name == "" {
			name = id
		}
		itemClassID := jsonFieldString(payload, "item_class")
		reqLevel := jsonFieldInt(payload, "required_level")
		width := jsonFieldInt(payload, "width")
		height := jsonFieldInt(payload, "height")
		tags := jsonFieldStringArray(payload, "tags")
		reqs := jsonFieldRaw(payload, "requirements", []byte(`{}`))
		props := jsonFieldRaw(payload, "properties", []byte(`{}`))
		if _, err := tx.ExecContext(ctx, q, id, name, nullable(itemClassID), nullableInt(reqLevel), nullableInt(width), nullableInt(height), pqStringArray(tags), reqs, props, payload, datasetID); err != nil {
			return err
		}
	}
	return nil
}

func syncMods(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	q := `INSERT INTO ref.mods(id,domain,generation_type,required_level,is_essence_only,is_fractured,stats,spawn_weights,tags,payload,dataset_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (id) DO UPDATE SET
 domain=EXCLUDED.domain,generation_type=EXCLUDED.generation_type,required_level=EXCLUDED.required_level,
 is_essence_only=EXCLUDED.is_essence_only,is_fractured=EXCLUDED.is_fractured,
 stats=EXCLUDED.stats,spawn_weights=EXCLUDED.spawn_weights,tags=EXCLUDED.tags,payload=EXCLUDED.payload,dataset_id=EXCLUDED.dataset_id`
	for id, payload := range m {
		domain := jsonFieldString(payload, "domain")
		genType := jsonFieldString(payload, "generation_type")
		reqLevel := jsonFieldInt(payload, "required_level")
		tags := jsonFieldStringArray(payload, "tags")
		stats := jsonFieldRaw(payload, "stats", []byte(`[]`))
		spawn := jsonFieldRaw(payload, "spawn_weights", []byte(`[]`))
		essence := jsonFieldBoolPtr(payload, "is_essence_only")
		fractured := jsonFieldBoolPtr(payload, "is_fractured")
		if _, err := tx.ExecContext(ctx, q, id, nullable(domain), nullable(genType), nullableInt(reqLevel), essence, fractured, stats, spawn, pqStringArray(tags), payload, datasetID); err != nil {
			return err
		}
	}
	return nil
}

func syncModsByBase(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte, baseIDs, modIDSet map[string]struct{}) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref.mods_by_base`); err != nil {
		return err
	}
	q := `INSERT INTO ref.mods_by_base(base_item_id, mod_id, dataset_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`

	var simple map[string]json.RawMessage
	if err := json.Unmarshal(raw, &simple); err == nil {
		for baseID, payload := range simple {
			modsForBase := collectModIDs(payload)
			if _, ok := baseIDs[baseID]; !ok {
				continue
			}
			for _, modID := range modsForBase {
				if _, ok := modIDSet[modID]; !ok {
					continue
				}
				if _, err := tx.ExecContext(ctx, q, baseID, modID, datasetID); err != nil {
					return err
				}
			}
		}
	}

	pairs := extractPairsFromNestedModsByBase(raw)
	for _, p := range pairs {
		if _, ok := baseIDs[p.baseID]; !ok {
			continue
		}
		if _, ok := modIDSet[p.modID]; !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, q, p.baseID, p.modID, datasetID); err != nil {
			return err
		}
	}
	return nil
}

func syncStatTranslations(ctx context.Context, tx *sql.Tx, datasetID int64, raw []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return err
	}
	q := `INSERT INTO ref.stat_translations(stat_id, english_text, payload, dataset_id)
VALUES($1,$2,$3,$4)
ON CONFLICT (stat_id) DO UPDATE SET english_text=EXCLUDED.english_text,payload=EXCLUDED.payload,dataset_id=EXCLUDED.dataset_id`
	for _, payload := range arr {
		ids := jsonFieldStringArray(payload, "ids")
		if len(ids) == 0 {
			continue
		}
		english := extractEnglishText(payload)
		for _, statID := range ids {
			if _, err := tx.ExecContext(ctx, q, statID, nullable(english), payload, datasetID); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadIDSet(ctx context.Context, tx *sql.Tx, table string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT id FROM %s", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

type baseModPair struct {
	baseID string
	modID  string
}

func extractPairsFromNestedModsByBase(raw []byte) []baseModPair {
	var root map[string]map[string]struct {
		Bases []string                          `json:"bases"`
		Mods  map[string]map[string]interface{} `json:"mods"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	pairs := make([]baseModPair, 0)
	seen := map[string]struct{}{}
	for _, byGroup := range root {
		for _, entry := range byGroup {
			if len(entry.Bases) == 0 || len(entry.Mods) == 0 {
				continue
			}
			modIDs := make([]string, 0)
			for _, bucket := range entry.Mods {
				for modID := range bucket {
					modIDs = append(modIDs, modID)
				}
			}
			modIDs = uniq(modIDs)
			for _, baseID := range entry.Bases {
				for _, modID := range modIDs {
					k := baseID + "|" + modID
					if _, ok := seen[k]; ok {
						continue
					}
					seen[k] = struct{}{}
					pairs = append(pairs, baseModPair{baseID: baseID, modID: modID})
				}
			}
		}
	}
	return pairs
}

func collectModIDs(payload json.RawMessage) []string {
	var arr []string
	if err := json.Unmarshal(payload, &arr); err == nil {
		return uniq(arr)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err == nil {
		ids := make([]string, 0, len(obj))
		for k := range obj {
			ids = append(ids, k)
		}
		return uniq(ids)
	}
	return nil
}

func uniq(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func jsonFieldString(raw json.RawMessage, key string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	v, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

func jsonFieldInt(raw json.RawMessage, key string) int {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0
	}
	v, ok := obj[key]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		return 0
	}
	return n
}

func jsonFieldBoolPtr(raw json.RawMessage, key string) any {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	v, ok := obj[key]
	if !ok {
		return nil
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return nil
	}
	return b
}

func jsonFieldStringArray(raw json.RawMessage, key string) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	v, ok := obj[key]
	if !ok {
		return nil
	}
	var a []string
	if err := json.Unmarshal(v, &a); err != nil {
		return nil
	}
	return uniq(a)
}

func jsonFieldRaw(raw json.RawMessage, key string, fallback []byte) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fallback
	}
	v, ok := obj[key]
	if !ok || len(v) == 0 {
		return fallback
	}
	return v
}

func extractEnglishText(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	eng, ok := obj["English"]
	if !ok {
		return ""
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(eng, &entries); err != nil || len(entries) == 0 {
		return ""
	}
	for _, e := range entries {
		if tRaw, ok := e["string"]; ok {
			var s string
			if json.Unmarshal(tRaw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func pqStringArray(in []string) any {
	if len(in) == 0 {
		in = []string{}
	}
	return pq.Array(in)
}
