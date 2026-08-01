package source

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type rakuma struct {
	client *Client
}

func newRakuma(client *Client) *rakuma { return &rakuma{client: client} }

func (r *rakuma) ID() string          { return "rakuma" }
func (r *rakuma) DisplayName() string { return "Rakuma" }

func (r *rakuma) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := rakumaURL(spec)
	body, err := r.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("rakuma: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("rakuma: parse html: %w", err)
	}

	var listings []Listing
	doc.Find("div.item-box").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find(".item-box__item-name a").First()
		href, _ := link.Attr("href")
		if href == "" {
			return
		}
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(sel.Find(".item-box__item-price").First().Text(), ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		img := sel.Find(".item-box__image-wrapper img").First()
		image := firstNonEmpty(img.AttrOr("data-original", ""), img.AttrOr("src", ""))
		category := rakumaCardCategory(sel)
		if category != "" && specExcludesCategory(spec, category) {
			return
		}
		extra := map[string]string{
			"brand": collapseSpaces(sel.Find(".item-box__item-sub-name").First().Text()),
		}
		if category != "" {
			extra["category"] = category
		}
		listings = append(listings, Listing{
			ExternalID: lastPathSegment(href),
			Title:      collapseSpaces(link.Text()),
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://item.fril.jp", href),
			ImageURL:   image,
			Extra:      extra,
		})
	})
	return FilterExcluded(spec, listings), nil
}

var rakumaCategoryID = regexp.MustCompile(`"category_id":"(\d+)"`)

func rakumaCardCategory(sel *goquery.Selection) string {
	h, err := goquery.OuterHtml(sel)
	if err != nil {
		return ""
	}
	if m := rakumaCategoryID.FindStringSubmatch(html.UnescapeString(h)); m != nil {
		return m[1]
	}
	return ""
}

func (r *rakuma) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, r.client, "rakuma", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
}

func rakumaURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	q := url.Values{}
	q.Set("query", spec.Query)
	q.Set("transaction", "selling")
	return "https://fril.jp/s?" + q.Encode()
}
