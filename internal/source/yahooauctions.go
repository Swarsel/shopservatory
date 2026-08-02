package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type yahooAuctions struct {
	client *Client
	jp     *Client
	log    *slog.Logger
}

func newYahooAuctions(client, jp *Client, log *slog.Logger) *yahooAuctions {
	return &yahooAuctions{client: client, jp: jp, log: log}
}

func (y *yahooAuctions) ID() string          { return "yahooauctions" }
func (y *yahooAuctions) DisplayName() string { return "Yahoo! Auctions" }

var zenItemCode = regexp.MustCompile(`itemCode=([^&]+)`)

func (y *yahooAuctions) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	if y.jp != nil {
		listings, err := y.searchDirect(ctx, spec)
		if err == nil {
			return listings, nil
		}
		if y.log != nil {
			y.log.Warn("yahooauctions: direct search failed, falling back to zenmarket", "err", err)
		}
	}
	return y.searchZenmarket(ctx, spec)
}

var errYahooNoItems = errors.New("yahooauctions(direct): no items in response")

func (y *yahooAuctions) searchDirect(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	q := url.Values{}
	q.Set("p", spec.Query)
	q.Set("s1", "new")
	q.Set("o1", "d")
	if spec.MinPrice != nil && *spec.MinPrice > 0 {
		q.Set("aucminprice", strconv.FormatFloat(*spec.MinPrice, 'f', -1, 64))
	}
	if spec.MaxPrice != nil && *spec.MaxPrice > 0 {
		q.Set("aucmaxprice", strconv.FormatFloat(*spec.MaxPrice, 'f', -1, 64))
	}
	endpoint := "https://auctions.yahoo.co.jp/search/search?" + q.Encode()

	body, err := y.jp.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja-JP,ja;q=0.9",
	})
	if err != nil {
		return nil, fmt.Errorf("yahooauctions(direct): fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("yahooauctions(direct): parse html: %w", err)
	}

	var listings []Listing
	seen := map[string]bool{}
	doc.Find("a.Product__titleLink").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		id := yahooAuctionID(href)
		if id == "" || seen[id] {
			return
		}
		title := collapseSpaces(link.Text())
		if title == "" {
			return
		}
		card := link.Closest("li")
		if card.Length() == 0 {
			card = link.Parent()
		}
		priceText := card.Find(".Product__priceValue").First().Text()
		price, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(priceText, ""), 64)
		if !withinPriceBounds(spec, price) {
			return
		}
		seen[id] = true

		img := firstNonEmpty(
			card.Find(".Product__imageData").First().AttrOr("src", ""),
			card.Find("img").First().AttrOr("src", ""),
		)
		listings = append(listings, Listing{
			ExternalID: id,
			Title:      title,
			Price:      price,
			Currency:   "JPY",
			URL:        "https://auctions.yahoo.co.jp/jp/auction/" + id,
			ImageURL:   img,
			SaleType:   "auction",
			Extra: map[string]string{
				"bids": nonDigits.ReplaceAllString(card.Find(".Product__bid").First().Text(), ""),
			},
		})
	})
	if len(listings) == 0 {
		return nil, errYahooNoItems
	}
	return listings, nil
}

var yahooAuctionPath = regexp.MustCompile(`/auction/([A-Za-z0-9]+)`)

func yahooAuctionID(href string) string {
	m := yahooAuctionPath.FindStringSubmatch(href)
	if m == nil {
		return ""
	}
	return m[1]
}

func (y *yahooAuctions) searchZenmarket(ctx context.Context, spec SearchSpec) ([]Listing, error) {
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
