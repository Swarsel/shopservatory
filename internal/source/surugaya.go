package source

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Swarsel/shopservatory/internal/browser"
)

type surugaya struct {
	client *Client
}

func newSurugaya(client *Client) *surugaya { return &surugaya{client: client} }

func (s *surugaya) ID() string          { return "surugaya" }
func (s *surugaya) DisplayName() string { return "Suruga-ya" }

func (s *surugaya) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	q := url.Values{}
	query := strings.TrimSpace(spec.Query)
	if strings.HasPrefix(query, "http") {
		u, err := url.Parse(query)
		if err != nil {
			return nil, fmt.Errorf("surugaya: invalid URL %q: %w", query, err)
		}
		q = u.Query()
	} else {
		q.Set("search_word", query)
	}

	for k, v := range spec.Params {
		q.Set(k, v)
	}
	if q.Get("sort") == "" && q.Get("rankBy") == "" {
		q.Set("sort", "updatetime")
	}

	endpoint := "https://www.suruga-ya.jp/search?" + q.Encode()

	var (
		html string
		err  error
	)
	if s.client.FlareSolverrAvailable() {
		html, err = s.client.SolveHTML(ctx, endpoint)
	} else {
		html, err = s.client.RenderHTML(ctx, endpoint, browser.RenderOptions{

			WaitSelector: "input[name='search_word']",
			SettleDelay:  3 * time.Second,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("surugaya: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("surugaya: parse html: %w", err)
	}

	var listings []Listing
	seen := map[string]bool{}
	doc.Find("div.item").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find("a[href*='/product/detail/']").First()
		href, _ := link.Attr("href")
		if href == "" {
			return
		}
		id := lastPathSegment(href)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		title := strings.TrimSpace(firstNonEmpty(
			sel.Find(".product-name, .title a").First().Text(),
			link.Text(),
		))

		priceText := sel.Find(".item_price .text-red").First().Text()
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(priceText, ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		soldOut := strings.Contains(sel.Find(".item_price .price").First().Text(), "品切れ")

		l := Listing{
			ExternalID: id,
			Title:      title,
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://www.suruga-ya.jp", href),
			ImageURL:   surugayaImageURL(id),
		}
		if soldOut {
			l.Extra = map[string]string{"stock": "sold_out"}
		}
		listings = append(listings, l)
	})
	return listings, nil
}

func (s *surugaya) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	var (
		html string
		err  error
	)
	if s.client.FlareSolverrAvailable() {
		html, err = s.client.SolveHTML(ctx, rawURL)
	} else {
		html, err = s.client.RenderHTML(ctx, rawURL, browser.RenderOptions{WaitSelector: ".price-buy", SettleDelay: 3 * time.Second})
	}
	if err != nil {
		return ItemSnapshot{}, fmt.Errorf("surugaya: fetch: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ItemSnapshot{}, err
	}
	price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(doc.Find(".price-buy").First().Text(), ""), 64)
	status := "active"
	if price == 0 && strings.Contains(doc.Find(".price_group, .item-price").Text(), "品切れ") {
		status = "sold"
	}
	return ItemSnapshot{Price: price, Currency: "JPY", Status: status}, nil
}

func surugayaImageURL(id string) string {
	if id == "" {
		return ""
	}
	lid := strings.ToLower(id)
	return "https://cdn.suruga-ya.jp/pics_webp/boxart_m/" + lid + "m.jpg.webp"
}
