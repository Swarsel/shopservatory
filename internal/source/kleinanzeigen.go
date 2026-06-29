package source

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type kleinanzeigen struct {
	client *Client
}

func newKleinanzeigen(client *Client) *kleinanzeigen { return &kleinanzeigen{client: client} }

func (k *kleinanzeigen) ID() string          { return "kleinanzeigen" }
func (k *kleinanzeigen) DisplayName() string { return "Kleinanzeigen" }

func (k *kleinanzeigen) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := kleinanzeigenURL(spec)
	body, err := k.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-DE,de;q=0.9,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("kleinanzeigen: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("kleinanzeigen: parse html: %w", err)
	}

	var listings []Listing
	doc.Find("article.aditem").Each(func(_ int, sel *goquery.Selection) {
		id, _ := sel.Attr("data-adid")
		if id == "" {
			return
		}
		price := parseKleinanzeigenPrice(sel.Find(".aditem-main--middle--price-shipping--price").First().Text())
		if !withinPriceBounds(spec, price) {
			return
		}

		var ld struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			ContentURL  string `json:"contentUrl"`
		}
		_ = json.Unmarshal([]byte(sel.Find(`script[type="application/ld+json"]`).First().Text()), &ld)

		title := strings.TrimSpace(ld.Title)
		if title == "" {
			title = collapseSpaces(sel.Find("h2 a").First().Text())
		}

		href, _ := sel.Attr("data-href")

		listings = append(listings, Listing{
			ExternalID: id,
			Title:      title,
			Price:      price,
			Currency:   "EUR",
			URL:        absoluteURL("https://www.kleinanzeigen.de", href),
			ImageURL:   ld.ContentURL,
			Extra: map[string]string{
				"location": collapseSpaces(sel.Find(".aditem-main--top--left").First().Text()),
				"posted":   collapseSpaces(sel.Find(".aditem-main--top--right").First().Text()),
			},
		})
	})
	return listings, nil
}

func (k *kleinanzeigen) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, k.client, "kleinanzeigen", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-DE,de;q=0.9,en;q=0.8",
	})
}

func kleinanzeigenURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	slug := url.PathEscape(strings.Join(strings.Fields(strings.ToLower(spec.Query)), "-"))
	priceSeg := ""
	if spec.MinPrice != nil || spec.MaxPrice != nil {
		lo, hi := "", ""
		if spec.MinPrice != nil {
			lo = strconv.Itoa(int(math.Floor(*spec.MinPrice)))
		}
		if spec.MaxPrice != nil {
			hi = strconv.Itoa(int(math.Ceil(*spec.MaxPrice)))
		}
		priceSeg = "preis:" + lo + ":" + hi + "/"
	}
	return "https://www.kleinanzeigen.de/s-" + priceSeg + slug + "/k0"
}

func parseKleinanzeigenPrice(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' || r == '.' || r == ',' {
			b.WriteRune(r)
		}
	}
	t := strings.ReplaceAll(b.String(), ".", "")
	t = strings.ReplaceAll(t, ",", ".")
	if t == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(t, 64)
	return v
}
