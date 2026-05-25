package trade2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"poe2-price-checker/internal/config"
)

type Client struct {
	baseURL      string
	league       string
	httpClient   *http.Client
	cookieHeader string
	searchLim    *rate.Limiter
	fetchLim     *rate.Limiter
	retry        config.RetryConfig
}

type SearchResponse struct {
	ID     string   `json:"id"`
	Result []string `json:"result"`
}

type FetchResponse struct {
	Result []json.RawMessage `json:"result"`
}

func New(baseURL, league, poesessid, cfClearance string, httpClient *http.Client, w config.WorkerConfig) *Client {
	cookieHeader := "POESESSID=" + poesessid
	if strings.TrimSpace(cfClearance) != "" {
		cookieHeader += "; cf_clearance=" + strings.TrimSpace(cfClearance)
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		league:       league,
		httpClient:   httpClient,
		cookieHeader: cookieHeader,
		searchLim:    rate.NewLimiter(rate.Limit(w.RateLimit.SearchRPS), w.RateLimit.SearchBurst),
		fetchLim:     rate.NewLimiter(rate.Limit(w.RateLimit.FetchRPS), w.RateLimit.FetchBurst),
		retry:        w.Retry,
	}
}

func (c *Client) Search(ctx context.Context, query json.RawMessage) (SearchResponse, error) {
	var out SearchResponse
	err := c.doWithRetry(ctx, c.searchLim, func() error {
		endpoint := fmt.Sprintf("%s/api/trade2/search/poe2/%s", c.baseURL, url.PathEscape(c.league))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(query))
		if err != nil {
			return err
		}
		c.applyBrowserLikeHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Referer", fmt.Sprintf("%s/trade2/search/poe2/%s", c.baseURL, url.PathEscape(c.league)))
		req.Header.Set("Cookie", c.cookieHeader)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return retryableStatus{code: resp.StatusCode}
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("search status %d: %s", resp.StatusCode, string(b))
		}
		return json.NewDecoder(resp.Body).Decode(&out)
	})
	return out, err
}

func (c *Client) Fetch(ctx context.Context, queryID string, itemIDs []string) (FetchResponse, error) {
	var out FetchResponse
	if len(itemIDs) == 0 {
		return out, nil
	}
	err := c.doWithRetry(ctx, c.fetchLim, func() error {
		endpoint := fmt.Sprintf("%s/api/trade2/fetch/%s?query=%s", c.baseURL, strings.Join(itemIDs, ","), url.QueryEscape(queryID))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		c.applyBrowserLikeHeaders(req)
		req.Header.Set("Referer", fmt.Sprintf("%s/trade2/search/poe2/%s/%s", c.baseURL, url.PathEscape(c.league), queryID))
		req.Header.Set("Cookie", c.cookieHeader)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return retryableStatus{code: resp.StatusCode}
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("fetch status %d: %s", resp.StatusCode, string(b))
		}
		return json.NewDecoder(resp.Body).Decode(&out)
	})
	return out, err
}

func (c *Client) applyBrowserLikeHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}

type retryableStatus struct{ code int }

func (e retryableStatus) Error() string { return fmt.Sprintf("retryable status %d", e.code) }

func (c *Client) doWithRetry(ctx context.Context, limiter *rate.Limiter, fn func() error) error {
	attempts := c.retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	base := c.retry.BaseBackoff.Duration
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	maxBackoff := c.retry.MaxBackoff.Duration
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}

	var last error
	for i := 1; i <= attempts; i++ {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
		err := fn()
		if err == nil {
			return nil
		}
		last = err
		if _, ok := err.(retryableStatus); !ok || i == attempts {
			break
		}
		d := base * time.Duration(1<<(i-1))
		if d > maxBackoff {
			d = maxBackoff
		}
		if c.retry.Jitter {
			d = time.Duration(rand.Int63n(int64(d)))
			if d < 100*time.Millisecond {
				d = 100 * time.Millisecond
			}
		}
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return last
}
