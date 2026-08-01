package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	return whListings(nd.Props.PageProps.SearchResult.AdvertSummaryList.AdvertSummary, spec), nil
}

func whListings(adverts []whAdvert, spec SearchSpec) []Listing {
	listings := make([]Listing, 0, len(adverts))
	for _, a := range adverts {
		price, _ := strconv.ParseFloat(a.attr("PRICE"), 64)
		if !withinPriceBounds(spec, price) {
			continue
		}
		cats := whCategoryIDs(a.attr("categorytreeids"))
		if anyCategoryExcluded(spec, cats) {
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
				"category": whLeafCategory(cats),
			},
		})
	}
	return listings
}

func (w *willhaben) SearchByImage(ctx context.Context, image []byte, spec SearchSpec) ([]Listing, error) {
	photo, err := NormalizeSearchImage(image)
	if err != nil {
		return nil, fmt.Errorf("willhaben: prepare image: %w", err)
	}

	boot, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.willhaben.at/iad/kaufen-und-verkaufen/marktplatz", nil)
	if err != nil {
		return nil, err
	}
	boot.Header.Set("Accept", "text/html,application/xhtml+xml")
	boot.Header.Set("Accept-Language", "de-AT,de;q=0.9,en;q=0.8")
	bresp, err := w.client.Do(boot)
	if err != nil {
		return nil, fmt.Errorf("willhaben: bootstrap: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(bresp.Body, 4<<20))
	bresp.Body.Close()
	cookies := bresp.Cookies()
	csrf := ""
	for _, c := range cookies {
		if c.Name == "x-bbx-csrf-token" {
			csrf = c.Value
		}
	}
	if csrf == "" {
		return nil, fmt.Errorf("willhaben: no csrf token from bootstrap")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "https://www.willhaben.at/webapi/ad-search/imagesearch/atz", bytes.NewReader(photo))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-WH-Client", "api@willhaben.at;responsive_web;server;1.0.0;desktop")
	req.Header.Set("Origin", "https://www.willhaben.at")
	req.Header.Set("Referer", "https://www.willhaben.at/iad/kaufen-und-verkaufen/marktplatz")
	req.Header.Set("x-bbx-csrf-token", csrf)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("willhaben: image search status %s: %s", resp.Status, truncate(body, 300))
	}

	var out struct {
		AdvertSummaryList struct {
			AdvertSummary []whAdvert `json:"advertSummary"`
		} `json:"advertSummaryList"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("willhaben: decode image search: %w", err)
	}
	return whListings(out.AdvertSummaryList.AdvertSummary, spec), nil
}

func (w *willhaben) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, w.client, "willhaben", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-AT,de;q=0.9,en;q=0.8",
	})
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

func whCategoryIDs(raw string) []string {
	raw = strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "[]"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, path := range strings.Split(raw, ",") {
		for _, id := range strings.Split(strings.TrimSpace(path), ";") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func whLeafCategory(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
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
