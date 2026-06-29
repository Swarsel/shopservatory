package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type shpock struct {
	client *Client
}

func newShpock(client *Client) *shpock { return &shpock{client: client} }

func (s *shpock) ID() string          { return "shpock" }
func (s *shpock) DisplayName() string { return "Shpock" }

var shpockItemID = regexp.MustCompile(`/i/([^/]+)`)

func (s *shpock) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := shpockURL(spec)
	body, err := s.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("shpock: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("shpock: parse html: %w", err)
	}

	var items []shpockItem
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		var list struct {
			ItemListElement []shpockItem `json:"itemListElement"`
		}
		if err := json.Unmarshal([]byte(sel.Text()), &list); err == nil && len(list.ItemListElement) > 0 {
			items = list.ItemListElement
			return false
		}
		return true
	})
	if items == nil {
		return nil, fmt.Errorf("shpock: itemList not found (page layout changed?)")
	}

	listings := make([]Listing, 0, len(items))
	for _, it := range items {
		var price float64
		if it.Offers.Price != nil {
			price = *it.Offers.Price
		}
		if !withinPriceBounds(spec, price) {
			continue
		}
		id := ""
		if m := shpockItemID.FindStringSubmatch(it.URL); m != nil {
			id = m[1]
		}
		currency := strings.ToUpper(it.Offers.PriceCurrency)
		if currency == "" {
			currency = "EUR"
		}
		listings = append(listings, Listing{
			ExternalID: id,
			Title:      it.Name,
			Price:      price,
			Currency:   currency,
			URL:        it.URL,
			ImageURL:   it.Image,
		})
	}
	return listings, nil
}

type shpockItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	URL         string `json:"url"`
	Offers      struct {
		Price         *float64 `json:"price"`
		PriceCurrency string   `json:"priceCurrency"`
	} `json:"offers"`
}

func (s *shpock) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, s.client, "shpock", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
}

func shpockURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	locale := spec.Param("locale")
	if locale == "" {
		locale = "de-at"
	}
	q := url.Values{}
	q.Set("q", spec.Query)
	return "https://www.shpock.com/" + locale + "/results?" + q.Encode()
}
