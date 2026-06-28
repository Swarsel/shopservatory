package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const vintedUA = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

type vinted struct {
	client *Client

	mu     sync.Mutex
	tokens map[string]vintedToken
}

type vintedToken struct {
	value   string
	expires time.Time
}

func newVinted(client *Client) *vinted {
	return &vinted{client: client, tokens: map[string]vintedToken{}}
}

func (v *vinted) ID() string          { return "vinted" }
func (v *vinted) DisplayName() string { return "Vinted" }

func (v *vinted) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	host, vals, err := vintedTarget(spec)
	if err != nil {
		return nil, err
	}
	endpoint := "https://" + host + "/api/v2/catalog/items?" + vals.Encode()

	body, err := v.apiGet(ctx, host, endpoint)
	if err != nil {
		return nil, fmt.Errorf("vinted: %w", err)
	}

	var out struct {
		Items []struct {
			ID    json.Number `json:"id"`
			Title string      `json:"title"`
			URL   string      `json:"url"`
			Price struct {
				Amount       json.Number `json:"amount"`
				CurrencyCode string      `json:"currency_code"`
			} `json:"price"`
			Photo struct {
				URL string `json:"url"`
			} `json:"photo"`
			BrandTitle string `json:"brand_title"`
			SizeTitle  string `json:"size_title"`
			Status     string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("vinted: decode: %w", err)
	}

	listings := make([]Listing, 0, len(out.Items))
	for _, it := range out.Items {
		price, _ := it.Price.Amount.Float64()
		if !withinPriceBounds(spec, price) {
			continue
		}
		listings = append(listings, Listing{
			ExternalID: it.ID.String(),
			Title:      it.Title,
			Price:      price,
			Currency:   it.Price.CurrencyCode,
			URL:        it.URL,
			ImageURL:   it.Photo.URL,
			Extra: map[string]string{
				"brand":  it.BrandTitle,
				"size":   it.SizeTitle,
				"status": it.Status,
			},
		})
	}
	return listings, nil
}

func vintedTarget(spec SearchSpec) (string, url.Values, error) {
	q := strings.TrimSpace(spec.Query)
	if q == "" {
		return "", nil, fmt.Errorf("vinted: empty query")
	}
	vals := url.Values{}
	var host string
	if strings.HasPrefix(q, "http") {
		u, err := url.Parse(q)
		if err != nil {
			return "", nil, fmt.Errorf("vinted: invalid URL %q: %w", q, err)
		}
		host = u.Host
		for k, vv := range u.Query() {
			vals[k] = vv
		}
	} else {
		host = spec.Param("domain")
		if host == "" {
			host = "www.vinted.com"
		}
		vals.Set("search_text", q)
	}
	if !strings.Contains(host, "vinted.") {
		return "", nil, fmt.Errorf("vinted: %q is not a vinted domain", host)
	}
	if vals.Get("order") == "" {
		vals.Set("order", "newest_first")
	}
	if vals.Get("per_page") == "" {
		vals.Set("per_page", "96")
	}
	vals.Set("page", "1")
	if spec.MinPrice != nil {
		vals.Set("price_from", strconv.FormatFloat(*spec.MinPrice, 'f', -1, 64))
	}
	if spec.MaxPrice != nil {
		vals.Set("price_to", strconv.FormatFloat(*spec.MaxPrice, 'f', -1, 64))
	}
	return host, vals, nil
}

func (v *vinted) apiGet(ctx context.Context, host, endpoint string) ([]byte, error) {
	token, err := v.token(ctx, host, false)
	if err != nil {
		return nil, err
	}
	body, status, err := v.doAPI(ctx, endpoint, token)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		token, err = v.token(ctx, host, true)
		if err != nil {
			return nil, err
		}
		body, status, err = v.doAPI(ctx, endpoint, token)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("catalog status %d", status)
	}
	return body, nil
}

func (v *vinted) doAPI(ctx context.Context, endpoint, token string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", vintedUA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return body, resp.StatusCode, nil
}

func (v *vinted) token(ctx context.Context, host string, force bool) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !force {
		if t, ok := v.tokens[host]; ok && time.Now().Before(t.expires) {
			return t.value, nil
		}
	}
	tok, err := v.harvestToken(ctx, host)
	if err != nil {
		return "", err
	}
	v.tokens[host] = vintedToken{value: tok, expires: time.Now().Add(50 * time.Minute)}
	return tok, nil
}

func (v *vinted) harvestToken(ctx context.Context, host string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", vintedUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	for _, c := range resp.Cookies() {
		if c.Name == "access_token_web" && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("could not obtain access token from %s (status %s)", host, resp.Status)
}
