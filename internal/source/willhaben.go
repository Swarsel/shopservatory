package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type willhaben struct {
	client *Client
}

func newWillhaben(client *Client) *willhaben { return &willhaben{client: client} }

func (w *willhaben) ID() string          { return "willhaben" }
func (w *willhaben) DisplayName() string { return "willhaben" }

func (w *willhaben) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	q := url.Values{}
	q.Set("keyword", spec.Query)
	q.Set("sort", "1")
	if spec.MinPrice != nil {
		q.Set("PRICE_FROM", strconv.FormatFloat(*spec.MinPrice, 'f', -1, 64))
	}
	if spec.MaxPrice != nil {
		q.Set("PRICE_TO", strconv.FormatFloat(*spec.MaxPrice, 'f', -1, 64))
	}

	endpoint := "https://www.willhaben.at/iad/kaufen-und-verkaufen/marktplatz?" + q.Encode()
	body, err := w.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("willhaben: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("willhaben: parse html: %w", err)
	}
	raw := doc.Find("script#__NEXT_DATA__").First().Text()
	if raw == "" {
		return nil, fmt.Errorf("willhaben: __NEXT_DATA__ not found (page layout changed?)")
	}

	var nd struct {
		Props struct {
			PageProps struct {
				SearchResult struct {
					AdvertSummaryList struct {
						AdvertSummary []whAdvert `json:"advertSummary"`
					} `json:"advertSummaryList"`
				} `json:"searchResult"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(raw), &nd); err != nil {
		return nil, fmt.Errorf("willhaben: decode __NEXT_DATA__: %w", err)
	}

	adverts := nd.Props.PageProps.SearchResult.AdvertSummaryList.AdvertSummary
	listings := make([]Listing, 0, len(adverts))
	for _, a := range adverts {
		price, _ := strconv.ParseFloat(a.attr("PRICE"), 64)
		if !withinPriceBounds(spec, price) {
			continue
		}
		image := firstField(a.attr("ALL_IMAGE_URLS"), "https://cache.willhaben.at/mmo/")
		listings = append(listings, Listing{
			ExternalID: a.ID,
			Title:      a.Description,
			Price:      price,
			Currency:   "EUR",
			URL:        absoluteURL("https://www.willhaben.at/iad", a.attr("SEO_URL")),
			ImageURL:   image,
			Extra: map[string]string{
				"location": a.attr("LOCATION"),
			},
		})
	}
	return listings, nil
}

type whAdvert struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Attributes  struct {
		Attribute []struct {
			Name   string   `json:"name"`
			Values []string `json:"values"`
		} `json:"attribute"`
	} `json:"attributes"`
}

func (a whAdvert) attr(name string) string {
	for _, at := range a.Attributes.Attribute {
		if at.Name == name && len(at.Values) > 0 {
			return at.Values[0]
		}
	}
	return ""
}

func firstField(v, prefix string) string {
	if v == "" {
		return ""
	}
	v = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' })[0]
	if prefix != "" && !strings.HasPrefix(v, "http") {
		return prefix + strings.TrimPrefix(v, "/")
	}
	return v
}
