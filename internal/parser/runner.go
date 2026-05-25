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
	if len(sr.Result) == 0 {
		log.Printf("worker=%s query=%s empty search", wc.Name, query.Name)
		return nil
	}
	ids := sr.Result
	if len(ids) > 10 {
		ids = ids[:10]
	}
	fr, err := cli.Fetch(ctx, sr.ID, ids)
	if err != nil {
		return err
	}
	for _, raw := range fr.Result {
		lst, err := trade2.MapTradeEntryToListing(raw, r.cfg.Parser.League)
		if err != nil {
			continue
		}
		if err := repo.UpsertListing(ctx, lst); err != nil {
			log.Printf("worker=%s upsert error: %v", wc.Name, err)
		}
	}
	log.Printf("worker=%s query=%s synced=%d", wc.Name, query.Name, len(fr.Result))
	return nil
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
