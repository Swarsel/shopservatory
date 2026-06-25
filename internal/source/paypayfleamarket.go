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

type payPayFleaMarket struct {
	client *Client
}

func newPayPayFleaMarket(client *Client) *payPayFleaMarket {
	return &payPayFleaMarket{client: client}
}

func (p *payPayFleaMarket) ID() string          { return "paypayfleamarket" }
func (p *payPayFleaMarket) DisplayName() string { return "PayPay Flea Market" }

func (p *payPayFleaMarket) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	if spec.Param("direct") == "1" {
		return p.searchDirect(ctx, spec)
	}
	return p.searchBuyee(ctx, spec)
}

func (p *payPayFleaMarket) searchBuyee(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	q := url.Values{}
	q.Set("keyword", spec.Query)
	q.Set("sort", "openTime")
	q.Set("order", "desc")
	endpoint := "https://buyee.jp/paypayfleamarket/search?" + q.Encode()

	html, err := p.client.RenderHTML(ctx, endpoint, browser.RenderOptions{
		WaitSelector: "a[href*='/item/']",
		SettleDelay:  3 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("paypayfleamarket(buyee): fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("paypayfleamarket(buyee): parse html: %w", err)
	}

	var listings []Listing
	seen := map[string]bool{}

	doc.Find("a[href*='/paypayfleamarket/item/']").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		id := surugayaID(href)
		if id == "" || seen[id] {
			return
		}

		card := link.Closest("li")
		if card.Length() == 0 {
			card = link.Parent()
		}
		title := strings.TrimSpace(firstNonEmpty(
			card.Find("h2.name, .name").First().Text(),
			card.Find("img").AttrOr("alt", ""),
		))
		if title == "" {
			return
		}
		seen[id] = true

		priceText := card.Find("p.price, .price").First().Text()
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(priceText, ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		img := firstNonEmpty(card.Find("img").AttrOr("data-src", ""), card.Find("img").AttrOr("src", ""))

		listings = append(listings, Listing{
			ExternalID: id,
			Title:      title,
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://buyee.jp", href),
			ImageURL:   absoluteURL("https://buyee.jp", img),
			Extra:      map[string]string{"proxy": "buyee"},
		})
	})
	return listings, nil
}

func (p *payPayFleaMarket) searchDirect(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	q := url.Values{}
	q.Set("query", spec.Query)
	q.Set("sort", "ctime")
	q.Set("order", "desc")
	endpoint := "https://paypayfleamarket.yahoo.co.jp/search/" + url.PathEscape(spec.Query) + "?" + q.Encode()

	html, err := p.client.RenderHTML(ctx, endpoint, browser.RenderOptions{
		WaitSelector: "a[href*='/item/']",
		SettleDelay:  3 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("paypayfleamarket(direct): fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("paypayfleamarket(direct): parse html: %w", err)
	}

	var listings []Listing
	doc.Find("a[href*='/item/']").Each(func(_ int, sel *goquery.Selection) {
		href, _ := sel.Attr("href")
		title := strings.TrimSpace(sel.AttrOr("aria-label", sel.Text()))
		if href == "" || title == "" {
			return
		}
		listings = append(listings, Listing{
			ExternalID: surugayaID(href),
			Title:      title,
			Currency:   "JPY",
			URL:        absoluteURL("https://paypayfleamarket.yahoo.co.jp", href),
		})
	})
	return listings, nil
}
