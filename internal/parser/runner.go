package parser

import (
	"context"
	"database/sql"
	"encoding/json"
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

const defaultQuery = `{"query":{"status":{"option":"online"}},"sort":{"price":"asc"}}`

type Runner struct {
	cfg       config.Config
	poesessid string
}

func New(cfg config.Config, poesessid string) *Runner { return &Runner{cfg: cfg, poesessid: poesessid} }

func (r *Runner) Run(ctx context.Context) error {
	db, err := sql.Open("postgres", r.cfg.Postgres.DSN)
	if err != nil {
		return err
	}
	defer db.Close()

	repo := market.NewRepository(db)
	query := json.RawMessage(defaultQuery)
	var wg sync.WaitGroup

	for _, wc := range r.cfg.Parser.Workers {
		wc := wc
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runWorker(ctx, wc, repo, query)
		}()
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (r *Runner) runWorker(ctx context.Context, wc config.WorkerConfig, repo *market.Repository, query json.RawMessage) {
	cli := trade2.New("https://www.pathofexile.com", r.cfg.Parser.League, r.poesessid, newHTTPClient(wc.Proxy, r.cfg.Parser.RequestTimeout.Duration), wc)
	ticker := time.NewTicker(r.cfg.Parser.PollInterval.Duration)
	defer ticker.Stop()

	for {
		if err := r.runCycle(ctx, cli, wc, repo, query); err != nil {
			log.Printf("worker=%s cycle error: %v", wc.Name, err)
			t := time.NewTimer(wc.Cooldown.Sleep.Duration)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) runCycle(ctx context.Context, cli *trade2.Client, wc config.WorkerConfig, repo *market.Repository, query json.RawMessage) error {
	sr, err := cli.Search(ctx, query)
	if err != nil {
		return err
	}
	if len(sr.Result) == 0 {
		log.Printf("worker=%s empty search", wc.Name)
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
	log.Printf("worker=%s synced=%d", wc.Name, len(fr.Result))
	return nil
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
