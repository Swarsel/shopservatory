package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type yahooAuctions struct {
	client *Client
}

func newYahooAuctions(client *Client) *yahooAuctions { return &yahooAuctions{client: client} }

func (y *yahooAuctions) ID() string          { return "yahooauctions" }
func (y *yahooAuctions) DisplayName() string { return "Yahoo! Auctions" }

var zenItemCode = regexp.MustCompile(`itemCode=([^&]+)`)

func (y *yahooAuctions) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	body, err := y.client.GetBody(ctx, yahooAuctionsURL(spec), map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "en,ja;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("yahooauctions: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("yahooauctions: parse html: %w", err)
	}

	var listings []Listing
	doc.Find("div.yahoo-search-result").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find("a.auction-url").First()
		href, _ := link.Attr("href")
		m := zenItemCode.FindStringSubmatch(href)
		if m == nil {
			return
		}
		jpy, _ := sel.Find(".auction-price .amount").First().Attr("data-jpy")
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(jpy, ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		img, _ := sel.Find(".img-wrap img").First().Attr("src")
		listings = append(listings, Listing{
			ExternalID: m[1],
			Title:      collapseSpaces(link.Text()),
			Price:      price,
			Currency:   "JPY",
			URL:        absoluteURL("https://zenmarket.jp/en", href),
			ImageURL:   img,
			SaleType:   "auction",
			Extra: map[string]string{
				"bids":  nonDigits.ReplaceAllString(sel.Find(".auction-label").First().Text(), ""),
				"proxy": "zenmarket",
			},
		})
	})
	return listings, nil
}

var zenJPY = regexp.MustCompile(`data-jpy='([^']*)'`)

func (y *yahooAuctions) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	body, status, err := y.client.Fetch(ctx, rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "en,ja;q=0.8",
	})
	if err != nil {
		return ItemSnapshot{}, err
	}
	if status == http.StatusNotFound || status == http.StatusGone {
		return ItemSnapshot{Status: "removed"}, nil
	}
	if status < 200 || status >= 300 {
		return ItemSnapshot{}, fmt.Errorf("yahooauctions: snapshot status %d", status)
	}
	m := zenJPY.FindStringSubmatch(string(body))
	if m == nil {
		return ItemSnapshot{}, fmt.Errorf("yahooauctions: price not found")
	}
	price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(m[1], ""), 64)
	return ItemSnapshot{Price: price, Currency: "JPY", Status: "active", SaleType: "auction"}, nil
}

func yahooAuctionsURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	q := url.Values{}
	q.Set("q", spec.Query)
	return "https://zenmarket.jp/en/yahoo.aspx?" + q.Encode()
}
