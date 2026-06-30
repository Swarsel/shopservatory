package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type ricardo struct {
	client *Client
}

func newRicardo(client *Client) *ricardo { return &ricardo{client: client} }

func (r *ricardo) ID() string          { return "ricardo" }
func (r *ricardo) DisplayName() string { return "Ricardo" }

var (
	ricardoPriceRe = regexp.MustCompile(`[0-9][0-9'’\s\x{00a0}]*\.[0-9]{2}`)
	ricardoIDRe    = regexp.MustCompile(`-(\d+)/?$`)
)

func (r *ricardo) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	endpoint := ricardoURL(spec)
	html, err := r.client.GetHTML(ctx, endpoint, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("ricardo: fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("ricardo: parse html: %w", err)
	}

	var listings []Listing
	seen := map[string]bool{}
	doc.Find(`a[href^="/de/a/"]`).Each(func(_ int, sel *goquery.Selection) {
		href, _ := sel.Attr("href")
		m := ricardoIDRe.FindStringSubmatch(href)
		if m == nil || seen[m[1]] {
			return
		}
		img := sel.Find(`img[src*="ricardostatic.ch"]`).First()
		title, _ := img.Attr("alt")
		if title == "" {
			return
		}
		price := parsePrice(ricardoPriceRe.FindString(sel.Text()))
		if !withinPriceBounds(spec, price) {
			return
		}
		seen[m[1]] = true
		imgURL, _ := img.Attr("src")
		listings = append(listings, Listing{
			ExternalID: m[1],
			Title:      collapseSpaces(title),
			Price:      price,
			Currency:   "CHF",
			URL:        absoluteURL("https://www.ricardo.ch", href),
			ImageURL:   imgURL,
		})
	})
	return listings, nil
}

func ricardoURL(spec SearchSpec) string {
	if strings.HasPrefix(spec.Query, "http") {
		return spec.Query
	}
	return "https://www.ricardo.ch/de/s/" + url.PathEscape(spec.Query) + "/"
}

func parsePrice(s string) float64 {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\'', '’', ' ', ' ':
			return -1
		}
		return r
	}, s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
