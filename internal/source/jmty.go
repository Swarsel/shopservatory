package source

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type jmty struct {
	client *Client
}

func newJmty(client *Client) *jmty { return &jmty{client: client} }

func (j *jmty) ID() string          { return "jmty" }
func (j *jmty) DisplayName() string { return "Jmty" }

func (j *jmty) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := jmtyURL(spec)
	body, err := j.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("jmty: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("jmty: parse html: %w", err)
	}

	var listings []Listing
	doc.Find("li.p-articles-list-item").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find(".p-item-title a").First()
		href, _ := link.Attr("href")
		if href == "" {
			return
		}
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(sel.Find(".p-item-most-important").First().Text(), ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		img, _ := sel.Find("img.p-item-image").First().Attr("src")
		listings = append(listings, Listing{
			ExternalID: lastPathSegment(href),
			Title:      collapseSpaces(link.Text()),
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://jmty.jp", href),
			ImageURL:   img,
			Extra: map[string]string{
				"location": collapseSpaces(sel.Find(".p-item-secondary-important").First().Text()),
			},
		})
	})
	return listings, nil
}

func (j *jmty) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, j.client, "jmty", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
}

func jmtyURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	q := url.Values{}
	q.Set("keyword", spec.Query)
	return "https://jmty.jp/all/sale?" + q.Encode()
}
