package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type snkrdunk struct {
	client *Client
}

func newSnkrdunk(client *Client) *snkrdunk { return &snkrdunk{client: client} }

func (s *snkrdunk) ID() string          { return "snkrdunk" }
func (s *snkrdunk) DisplayName() string { return "SNKRDUNK" }

func (s *snkrdunk) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	brand, categoryID, urlOrder, err := parseSnkrdunkURL(spec.Query)
	if err != nil {
		return nil, err
	}

	order := urlOrder
	if v := spec.Param("order"); v != "" {
		order = v
	}
	if order == "" {
		order = "release"
	}
	perPage := "60"
	if v := spec.Param("per_page"); v != "" {
		perPage = v
	}

	q := url.Values{}
	q.Set("perPage", perPage)
	q.Set("page", "1")
	q.Set("order", order)

	endpoint := fmt.Sprintf("https://snkrdunk.com/v1/apparel-used-items/brands/%s/apparel-categories/%d?%s",
		brand, categoryID, q.Encode())

	body, err := s.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":           "application/json",
		"X-Requested-With": "XMLHttpRequest",
		"Accept-Language":  "ja,en;q=0.8",
	})
	if err != nil {
		return nil, fmt.Errorf("snkrdunk: fetch: %w", err)
	}

	var out struct {
		ApparelUsedItems []struct {
			UsedItemID    int64   `json:"usedItemId"`
			LocalizedName string  `json:"localizedName"`
			Price         float64 `json:"price"`
			DisplaySize   string  `json:"displaySize"`
			Condition     string  `json:"displayShortConditionTitle"`
			ImageURL      string  `json:"imageUrl"`
			ItemLink      string  `json:"itemLink"`
			IsDisplaySold bool    `json:"isDisplaySold"`
		} `json:"apparelUsedItems"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("snkrdunk: decode: %w", err)
	}

	listings := make([]Listing, 0, len(out.ApparelUsedItems))
	for _, it := range out.ApparelUsedItems {
		if it.IsDisplaySold {
			continue
		}
		if !withinPriceBounds(spec, it.Price) {
			continue
		}
		listings = append(listings, Listing{
			ExternalID: strconv.FormatInt(it.UsedItemID, 10),
			Title:      it.LocalizedName,
			Price:      it.Price,
			Currency:   "JPY",
			URL:        absoluteURL("https://snkrdunk.com", it.ItemLink),
			ImageURL:   it.ImageURL,
			Extra: map[string]string{
				"condition": it.Condition,
				"size":      it.DisplaySize,
			},
		})
	}
	return listings, nil
}

func (s *snkrdunk) Snapshot(ctx context.Context, rawURL string) (ItemSnapshot, error) {
	return ldjsonSnapshot(ctx, s.client, "snkrdunk", rawURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
}

func parseSnkrdunkURL(raw string) (brand string, categoryID int64, order string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, "", fmt.Errorf("snkrdunk: query must be a browse URL, e.g. https://snkrdunk.com/apparel-used-items/brands/<brand>/categories/<id>")
	}
	u, perr := url.Parse(raw)
	if perr != nil {
		return "", 0, "", fmt.Errorf("snkrdunk: invalid URL %q: %w", raw, perr)
	}
	order = u.Query().Get("order")

	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, seg := range segs {
		switch seg {
		case "brands":
			if i+1 < len(segs) {
				brand = segs[i+1]
			}
		case "categories", "apparel-categories":
			if i+1 < len(segs) {
				categoryID, _ = strconv.ParseInt(segs[i+1], 10, 64)
			}
		}
	}
	if brand == "" || categoryID == 0 {
		return "", 0, "", fmt.Errorf("snkrdunk: could not find brand slug and category id in %q (expected .../brands/<brand>/categories/<id>)", raw)
	}
	return brand, categoryID, order, nil
}
