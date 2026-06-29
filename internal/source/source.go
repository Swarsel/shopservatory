package source

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/Swarsel/shopservatory/internal/browser"
	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/flaresolverr"
)

type SearchSpec struct {
	Query    string
	MinPrice *float64
	MaxPrice *float64

	Params map[string]string
}

func (s SearchSpec) Param(key string) string {
	if s.Params == nil {
		return ""
	}
	return s.Params[key]
}

type Listing struct {
	ExternalID string
	Title      string
	Price      float64
	Currency   string
	URL        string
	ImageURL   string

	SaleType string

	ListedAt time.Time

	Extra map[string]string
}

type Source interface {
	ID() string

	DisplayName() string

	Search(ctx context.Context, spec SearchSpec) ([]Listing, error)
}

type ItemSnapshot struct {
	Title    string
	Price    float64
	Currency string
	ImageURL string
	Status   string
	SaleType string
}

type ItemMonitor interface {
	Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error)
}

type Client struct {
	http      *http.Client
	userAgent string
	browser   *browser.Browser
	flare     *flaresolverr.Client
	log       *slog.Logger
}

func NewClient(cfg config.Scrape, log *slog.Logger) (*Client, error) {
	transport := &http.Transport{}
	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse scrape.proxy_url: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &Client{
		http:      &http.Client{Timeout: cfg.Timeout.Duration, Transport: transport},
		userAgent: cfg.UserAgent,
		browser:   browser.New(cfg.BrowserPath, cfg.UserAgent, cfg.BrowserTimeout.Duration, cfg.BrowserProxy, log),
		flare:     flaresolverr.New(cfg.FlareSolverrURL, cfg.FlareSolverrTimeout.Duration, cfg.BrowserProxy),
		log:       log,
	}, nil
}

func (c *Client) BrowserAvailable() bool { return c.browser != nil }

func (c *Client) FlareSolverrAvailable() bool { return c.flare != nil }

func (c *Client) RenderHTML(ctx context.Context, rawURL string, opts browser.RenderOptions) (string, error) {
	if c.browser == nil {
		return "", fmt.Errorf("this source requires a headless browser; set scrape.browser_path (or SHOPSERVATORY_CHROMIUM)")
	}
	return c.browser.RenderHTML(ctx, rawURL, opts)
}

func (c *Client) SolveHTML(ctx context.Context, rawURL string) (string, error) {
	if c.flare == nil {
		return "", fmt.Errorf("this source requires FlareSolverr; set scrape.flaresolverr_url")
	}
	return c.flare.Get(ctx, rawURL)
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return c.http.Do(req)
}

func (c *Client) GetBody(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	body, status, err := c.Fetch(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GET %s: unexpected status %d", rawURL, status)
	}
	return body, nil
}

func (c *Client) Fetch(ctx context.Context, rawURL string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

type Registry struct {
	sources map[string]Source
}

func NewRegistry(cfg config.Config, client *Client, log *slog.Logger) *Registry {
	r := &Registry{sources: map[string]Source{}}

	r.add(newMercari(client))
	r.add(newSnkrdunk(client))
	r.add(newSurugaya(client))
	r.add(newPayPayFleaMarket(client))
	r.add(newWillhaben(client))
	r.add(newVinted(client))
	r.add(newKleinanzeigen(client))
	r.add(newBazar(client))
	r.add(newShpock(client))
	r.add(newCraigslist(client))
	r.add(newJmty(client))

	if cfg.Ebay.Configured() {
		r.add(newEbay(client, cfg.Ebay, log))
	} else {
		log.Warn("eBay source disabled: credentials not configured")
	}

	return r
}

func (r *Registry) add(s Source) { r.sources[s.ID()] = s }

func (r *Registry) Get(id string) (Source, bool) {
	s, ok := r.sources[id]
	return s, ok
}

func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.sources))
	for id := range r.sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) All() []Source {
	out := make([]Source, 0, len(r.sources))
	for _, id := range r.IDs() {
		out = append(out, r.sources[id])
	}
	return out
}

func withinPriceBounds(spec SearchSpec, price float64) bool {
	if spec.MinPrice != nil && price < *spec.MinPrice {
		return false
	}
	if spec.MaxPrice != nil && price > *spec.MaxPrice {
		return false
	}
	return true
}
