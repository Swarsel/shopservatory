package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type bazar struct {
	client *Client
}

func newBazar(client *Client) *bazar { return &bazar{client: client} }

func (b *bazar) ID() string          { return "bazar" }
func (b *bazar) DisplayName() string { return "bazar.at" }

func (b *bazar) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := bazarURL(spec)
	body, err := b.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "application/json",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("bazar: fetch: %w", err)
	}

	var page struct {
		Content []bazarItem `json:"content"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("bazar: decode json: %w", err)
	}

	listings := make([]Listing, 0, len(page.Content))
	for _, it := range page.Content {
		var price float64
		if it.Common.Price.Price != nil {
			price = *it.Common.Price.Price
		}
		if !withinPriceBounds(spec, price) {
			continue
		}
		listed, _ := time.Parse(time.RFC3339, it.MetaInformation.Created)
		listings = append(listings, Listing{
			ExternalID: strconv.FormatInt(it.ID, 10),
			Title:      it.Common.Title,
			Price:      price,
			Currency:   "EUR",
			URL:        absoluteURL("https://www.bazar.at", it.Path),
			ImageURL:   it.Image.Link,
			ListedAt:   listed,
			Extra: map[string]string{
				"location":   it.Common.Location.DisplayText,
				"price_type": it.Common.Price.PriceType.Name,
			},
		})
	}
	return listings, nil
}

type bazarItem struct {
	ID     int64  `json:"id"`
	Path   string `json:"path"`
	Common struct {
		Title    string `json:"title"`
		Location struct {
			DisplayText string `json:"displayText"`
		} `json:"location"`
		Price struct {
			Price     *float64 `json:"price"`
			PriceType struct {
				Name string `json:"name"`
			} `json:"priceType"`
		} `json:"price"`
	} `json:"common"`
	MetaInformation struct {
		Created string `json:"created"`
	} `json:"metaInformation"`
	Image struct {
		Link string `json:"link"`
	} `json:"image"`
}

func (b *bazar) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, b.client, "bazar", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
}

func bazarURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		if u, err := url.Parse(spec.Query); err == nil {
			if strings.HasPrefix(u.Path, "/l/") {
				u.Path = "/api/article" + u.Path
			}
			return u.String()
		}
		return spec.Query
	}
	allShops := "false"
	if v := spec.Param("all_shops"); v != "" {
		allShops = v
	}
	q := url.Values{}
	q.Set("term", spec.Query)
	q.Set("allShops", allShops)
	q.Set("page", "0")
	q.Set("size", "50")
	q.Set("sort", "sort.date,desc")
	return "https://www.bazar.at/api/article/l/00-alle/h?" + q.Encode()
}
