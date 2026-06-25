package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Swarsel/shopservatory/internal/config"
)

type ebay struct {
	client    *Client
	cfg       config.Ebay
	log       *slog.Logger
	tokenURL  string
	searchURL string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func newEbay(client *Client, cfg config.Ebay, log *slog.Logger) *ebay {
	return &ebay{
		client:    client,
		cfg:       cfg,
		log:       log,
		tokenURL:  "https://api.ebay.com/identity/v1/oauth2/token",
		searchURL: "https://api.ebay.com/buy/browse/v1/item_summary/search",
	}
}

func (e *ebay) ID() string          { return "ebay" }
func (e *ebay) DisplayName() string { return "eBay" }

func (e *ebay) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	token, err := e.accessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("ebay: auth: %w", err)
	}

	q := url.Values{}
	q.Set("q", spec.Query)
	q.Set("limit", "50")
	if v := spec.Param("category_ids"); v != "" {
		q.Set("category_ids", v)
	}
	if v := spec.Param("sort"); v != "" {
		q.Set("sort", v)
	}
	if f := e.priceFilter(spec); f != "" {
		q.Set("filter", f)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.searchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", e.cfg.Marketplace)
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ebay: search status %s: %s", resp.Status, truncate(body, 300))
	}

	var out struct {
		ItemSummaries []struct {
			ItemID    string `json:"itemId"`
			Title     string `json:"title"`
			Condition string `json:"condition"`
			Price     struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"price"`
			ItemWebURL string `json:"itemWebUrl"`
			Image      struct {
				ImageURL string `json:"imageUrl"`
			} `json:"image"`
			Seller struct {
				Username string `json:"username"`
			} `json:"seller"`
		} `json:"itemSummaries"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ebay: decode: %w", err)
	}

	listings := make([]Listing, 0, len(out.ItemSummaries))
	for _, it := range out.ItemSummaries {
		price, _ := strconv.ParseFloat(it.Price.Value, 64)
		if !withinPriceBounds(spec, price) {
			continue
		}
		listings = append(listings, Listing{
			ExternalID: it.ItemID,
			Title:      it.Title,
			Price:      price,
			Currency:   it.Price.Currency,
			URL:        it.ItemWebURL,
			ImageURL:   it.Image.ImageURL,
			Extra: map[string]string{
				"condition": it.Condition,
				"seller":    it.Seller.Username,
			},
		})
	}
	return listings, nil
}

func (e *ebay) priceFilter(spec SearchSpec) string {
	var parts []string
	if spec.MinPrice != nil || spec.MaxPrice != nil {
		lo, hi := "", ""
		if spec.MinPrice != nil {
			lo = strconv.FormatFloat(*spec.MinPrice, 'f', -1, 64)
		}
		if spec.MaxPrice != nil {
			hi = strconv.FormatFloat(*spec.MaxPrice, 'f', -1, 64)
		}
		parts = append(parts, fmt.Sprintf("price:[%s..%s]", lo, hi))
	}
	if raw := spec.Param("filter"); raw != "" {
		parts = append(parts, raw)
	}
	return strings.Join(parts, ",")
}

func (e *ebay) accessToken(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.token != "" && time.Now().Before(e.tokenExpiry) {
		return e.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "https://api.ebay.com/oauth/api_scope")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(e.cfg.ClientID + ":" + e.cfg.ClientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token status %s: %s", resp.Status, truncate(body, 300))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	e.token = tok.AccessToken

	e.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	return e.token, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
