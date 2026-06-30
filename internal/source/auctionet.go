package source

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type auctionet struct {
	client *Client
}

func newAuctionet(client *Client) *auctionet { return &auctionet{client: client} }

func (a *auctionet) ID() string          { return "auctionet" }
func (a *auctionet) DisplayName() string { return "Auctionet" }

func (a *auctionet) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := auctionetURL(spec)
	body, err := a.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("auctionet: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("auctionet: parse html: %w", err)
	}

	var items []auctionetItem
	doc.Find("[data-react-props]").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		raw, _ := sel.Attr("data-react-props")
		if !strings.Contains(raw, `"items"`) {
			return true
		}
		var props struct {
			Items []auctionetItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(html.UnescapeString(raw)), &props); err == nil && len(props.Items) > 0 {
			items = props.Items
			return false
		}
		return true
	})

	listings := make([]Listing, 0, len(items))
	for _, it := range items {
		price := it.price()
		if !withinPriceBounds(spec, price) {
			continue
		}
		title := it.ShortTitle
		if title == "" {
			title = it.LongTitle
		}
		listings = append(listings, Listing{
			ExternalID: strconv.FormatInt(it.ID, 10),
			Title:      collapseSpaces(title),
			Price:      price,
			Currency:   it.Currency,
			URL:        absoluteURL("https://auctionet.com", it.URL),
			ImageURL:   it.MainImageURL,
			SaleType:   "auction",
			Extra: map[string]string{
				"bids": it.AmountLabel,
				"ends": it.AuctionEndTime,
			},
		})
	}
	return listings, nil
}

type auctionetItem struct {
	ID             int64   `json:"id"`
	ShortTitle     string  `json:"shortTitle"`
	LongTitle      string  `json:"longTitle"`
	Estimate       float64 `json:"estimate"`
	Currency       string  `json:"currency"`
	URL            string  `json:"url"`
	MainImageURL   string  `json:"mainImageUrl"`
	AmountLabel    string  `json:"amountLabel"`
	AmountValue    string  `json:"amountValue"`
	AuctionEndTime string  `json:"auctionEndTime"`
}

func (it auctionetItem) price() float64 {
	if v, _ := strconv.ParseFloat(nonDigits.ReplaceAllString(it.AmountValue, ""), 64); v > 0 {
		return v
	}
	return it.Estimate
}

func auctionetURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	q := url.Values{}
	q.Set("q", spec.Query)
	return "https://auctionet.com/de/search?" + q.Encode()
}
