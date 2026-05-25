package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/net/proxy"
	"poe2-price-checker/internal/config"
	"poe2-price-checker/internal/market"
	"poe2-price-checker/internal/trade2"
)

type Runner struct {
	cfg       config.Config
	poesessid string
}

func New(cfg config.Config, poesessid string) *Runner { return &Runner{cfg: cfg, poesessid: poesessid} }

type runtimeQuery struct {
	Name string
	Raw  json.RawMessage
	Hash string
}

func (r *Runner) Run(ctx context.Context) error {
	db, err := sql.Open("postgres", r.cfg.Postgres.DSN)
	if err != nil {
		return err
	}
	defer db.Close()

	repo := market.NewRepository(db)
	queries, err := buildQueries(r.cfg.Parser.Queries)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup

	for _, wc := range r.cfg.Parser.Workers {
		wc := wc
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runWorker(ctx, wc, repo, queries)
		}()
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (r *Runner) runWorker(ctx context.Context, wc config.WorkerConfig, repo *market.Repository, queries []runtimeQuery) {
	cli := trade2.New("https://www.pathofexile.com", r.cfg.Parser.League, r.poesessid, newHTTPClient(wc.Proxy, r.cfg.Parser.RequestTimeout.Duration), wc)
	ticker := time.NewTicker(r.cfg.Parser.PollInterval.Duration)
	defer ticker.Stop()

	for {
		for _, query := range queries {
			if err := r.runCycle(ctx, cli, wc, repo, query); err != nil {
				log.Printf("worker=%s query=%s cycle error: %v", wc.Name, query.Name, err)
				t := time.NewTimer(wc.Cooldown.Sleep.Duration)
				select {
				case <-ctx.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) runCycle(ctx context.Context, cli *trade2.Client, wc config.WorkerConfig, repo *market.Repository, query runtimeQuery) error {
	sr, err := cli.Search(ctx, query.Raw)
	if err != nil {
		return err
	}
	rawSearchResponse, _ := json.Marshal(sr)
	if err := repo.InsertSearchRun(ctx, r.cfg.Parser.League, query.Name, sr.ID, query.Hash, rawSearchResponse); err != nil {
		log.Printf("worker=%s query=%s search_run insert error: %v", wc.Name, query.Name, err)
	}

	ids := uniqStrings(sr.Result)
	if len(ids) == 0 {
		log.Printf("worker=%s query=%s empty search", wc.Name, query.Name)
		return nil
	}

	const fetchBatchSize = 10
	var fetched, saved, parseErrs, saveErrs int
	for _, batch := range chunkStrings(ids, fetchBatchSize) {
		fr, ferr := cli.Fetch(ctx, sr.ID, batch)
		if ferr != nil {
			return ferr
		}
		fetched += len(fr.Result)
		for _, raw := range fr.Result {
			lst, perr := trade2.MapTradeEntryToListing(raw, r.cfg.Parser.League)
			if perr != nil {
				parseErrs++
				continue
			}
			if serr := repo.UpsertListing(ctx, lst); serr != nil {
				saveErrs++
				continue
			}
			saved++
		}
	}

	log.Printf("worker=%s query=%s search_ids=%d fetched=%d saved=%d parse_err=%d save_err=%d", wc.Name, query.Name, len(ids), fetched, saved, parseErrs, saveErrs)
	return nil
}

func uniqStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func chunkStrings(in []string, size int) [][]string {
	if size <= 0 {
		size = 1
	}
	out := make([][]string, 0, (len(in)+size-1)/size)
	for i := 0; i < len(in); i += size {
		j := i + size
		if j > len(in) {
			j = len(in)
		}
		out = append(out, in[i:j])
	}
	return out
}

func buildQueries(in []config.QueryConfig) ([]runtimeQuery, error) {
	out := make([]runtimeQuery, 0, len(in))
	for i, q := range in {
		b, err := json.Marshal(q.Body)
		if err != nil {
			return nil, fmt.Errorf("marshal parser.queries[%d]: %w", i, err)
		}
		name := q.Name
		if name == "" {
			name = fmt.Sprintf("query_%d", i+1)
		}
		h := sha256.Sum256(b)
		out = append(out, runtimeQuery{Name: name, Raw: b, Hash: hex.EncodeToString(h[:])})
	}
	return out, nil
}

func newHTTPClient(proxyAddr string, timeout time.Duration) *http.Client {
	tr := &http.Transport{}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if proxyAddr != "" {
		u, err := url.Parse(proxyAddr)
		if err == nil {
			switch u.Scheme {
			case "http", "https":
				tr.Proxy = http.ProxyURL(u)
			case "socks5", "socks5h":
				dialer, derr := proxy.FromURL(u, proxy.Direct)
				if derr == nil {
					tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialer.Dial(network, addr)
					}
				}
			}
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}
