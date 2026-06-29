package source

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type magi struct {
	client *Client
}

func newMagi(client *Client) *magi { return &magi{client: client} }

func (m *magi) ID() string          { return "magi" }
func (m *magi) DisplayName() string { return "magi" }

func (m *magi) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := magiURL(spec)
	body, err := m.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "en,ja;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("magi: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("magi: parse html: %w", err)
	}

	var listings []Listing
	doc.Find("div.item-list__box").Each(func(_ int, sel *goquery.Selection) {
		if sel.Find(".item-list__sold-icon").Length() > 0 {
			return
		}
		link := sel.Find("a.item-list__link").First()
		href, _ := link.Attr("href")
		if href == "" {
			return
		}
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(sel.Find(".item-list__price-box--price").First().Text(), ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		image, _ := sel.Find(".item-list__thumbnail img").First().Attr("data-src")
		listings = append(listings, Listing{
			ExternalID: lastPathSegment(href),
			Title:      collapseSpaces(sel.Find(".item-list__item-name").First().Text()),
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://en.magi.camp", href),
			ImageURL:   image,
		})
	})
	return listings, nil
}

func (m *magi) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, m.client, "magi", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "en,ja;q=0.8",
	})
}

func magiURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	q := url.Values{}
	q.Set("forms_search_items[keyword]", spec.Query)
	return "https://en.magi.camp/items/search?" + q.Encode()
}
