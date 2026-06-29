package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func ldjsonSnapshot(ctx context.Context, c *Client, name, rawURL string, headers map[string]string) (ItemSnapshot, error) {
	body, status, err := c.Fetch(ctx, rawURL, headers)
	if err != nil {
		return ItemSnapshot{}, err
	}
	if status == http.StatusNotFound || status == http.StatusGone {
		return ItemSnapshot{Status: "removed"}, nil
	}
	if status < 200 || status >= 300 {
		return ItemSnapshot{}, fmt.Errorf("%s: snapshot status %d", name, status)
	}
	snap, ok := snapshotFromLDJSON(body)
	if !ok {
		return ItemSnapshot{}, fmt.Errorf("%s: item data not found", name)
	}
	return snap, nil
}

var nonDigits = regexp.MustCompile(`[^0-9]`)

func absoluteURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	return base + "/" + strings.TrimPrefix(ref, "/")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lastPathSegment(href string) string {
	parts := strings.Split(strings.Trim(href, "/"), "/")
	if len(parts) == 0 {
		return href
	}
	last := parts[len(parts)-1]
	if i := strings.IndexAny(last, "?#"); i >= 0 {
		last = last[:i]
	}
	return last
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func leadingDigits(s string) string {
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}

func auctionFromURL(u string) string {
	if strings.Contains(u, "/auction/") {
		return "auction"
	}
	return ""
}

func snapshotFromLDJSON(body []byte) (ItemSnapshot, bool) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ItemSnapshot{}, false
	}
	var snap ItemSnapshot
	found := false
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		var p struct {
			Name   string          `json:"name"`
			Image  json.RawMessage `json:"image"`
			Offers json.RawMessage `json:"offers"`
		}
		clean := strings.Map(func(r rune) rune {
			if r < 0x20 {
				return ' '
			}
			return r
		}, sel.Text())
		if json.Unmarshal([]byte(clean), &p) != nil {
			return true
		}
		offer, ok := firstOffer(p.Offers)
		if !ok && p.Name == "" {
			return true
		}
		snap.Title = p.Name
		snap.Price = offer.amount()
		snap.Currency = strings.ToUpper(offer.PriceCurrency)
		snap.Status = availabilityStatus(offer.Availability)
		snap.ImageURL = firstImage(p.Image)
		found = true
		return false
	})
	return snap, found
}

type ldOffer struct {
	Price         json.Number `json:"price"`
	PriceCurrency string      `json:"priceCurrency"`
	Availability  string      `json:"availability"`
}

func (o ldOffer) amount() float64 { v, _ := o.Price.Float64(); return v }

func firstOffer(raw json.RawMessage) (ldOffer, bool) {
	if len(raw) == 0 {
		return ldOffer{}, false
	}
	var one ldOffer
	if json.Unmarshal(raw, &one) == nil && (one.Price != "" || one.Availability != "") {
		return one, true
	}
	var many []ldOffer
	if json.Unmarshal(raw, &many) == nil && len(many) > 0 {
		return many[0], true
	}
	return ldOffer{}, false
}

func firstImage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil && len(ss) > 0 {
		return ss[0]
	}
	var obj struct {
		URL        string `json:"url"`
		ContentURL string `json:"contentUrl"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return firstNonEmpty(obj.URL, obj.ContentURL)
	}
	return ""
}

func availabilityStatus(a string) string {
	a = strings.ToLower(a)
	switch {
	case strings.Contains(a, "soldout"), strings.Contains(a, "sold_out"),
		strings.Contains(a, "outofstock"), strings.Contains(a, "discontinued"):
		return "sold"
	default:
		return "active"
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
