package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

func TestMonitorViewIncludesDoorzoURL(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")

	cases := []struct {
		source     string
		externalID string
		url        string
		wantDoorzo bool
	}{
		{"yahooauctions", "1238998047", "https://auctions.yahoo.co.jp/jp/auction/1238998047", false},
		{"paypayfleamarket", "z651582616", "https://buyee.jp/paypayfleamarket/item/z651582616", true},
		{"mercari", "m123", "https://jp.mercari.com/item/m123", true},
		{"rakuma", "r123", "https://item.fril.jp/r123", true},
		{"surugaya", "s123", "https://www.suruga-ya.jp/product/detail/s123", true},
		{"snkrdunk", "sd123", "https://snkrdunk.com/products/sd123", true},
		{"ebay", "e123", "https://www.ebay.com/itm/e123", false},
		{"willhaben", "w123", "https://willhaben.at/iad/x/w123", false},
	}
	for _, tc := range cases {
		if _, err := e.st.AddMonitor(ctx, store.MonitoredItem{
			UserID: u.ID, Source: tc.source, ExternalID: tc.externalID,
			URL: tc.url, Title: "t", Interval: time.Hour, Enabled: true, Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}

	views, err := e.srv.monitorViews(ctx, u.ID, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	bySource := map[string]monitorView{}
	for _, v := range views {
		bySource[v.Source] = v
	}
	for _, tc := range cases {
		v, ok := bySource[tc.source]
		if !ok {
			t.Fatalf("no view for %s", tc.source)
		}
		if tc.wantDoorzo && v.DoorzoURL == "" {
			t.Errorf("%s should expose a doorzo url", tc.source)
		}
		if !tc.wantDoorzo && v.DoorzoURL != "" {
			t.Errorf("%s must not expose a doorzo url, got %q", tc.source, v.DoorzoURL)
		}
	}

	if by := bySource["yahooauctions"].BuyeeURL; by != "https://buyee.jp/item/yahoo/auction/1238998047" {
		t.Errorf("yahoo should expose a buyee url instead of doorzo, got %q", by)
	}
	if bySource["mercari"].BuyeeURL != "" {
		t.Error("mercari has no buyee item path and must not expose one")
	}
	if dz := bySource["paypayfleamarket"].DoorzoURL; dz != "" {
		if want := "https://www.doorzo.com/en/mall/paypay/detail/"; len(dz) <= len(want) || dz[:len(want)] != want {
			t.Errorf("paypay doorzo url = %q", dz)
		}
	}
}

func TestDoorzoButtonsRenderInDashboard(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/", nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)
	for _, want := range []string{
		"m.doorzoUrl",
		"item.doorzoUrl",
		"open in doorzo",
		"'cardbtn','doorzo'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}

func TestListingViewIncludesDoorzoURL(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")

	sid, err := e.st.CreateSearch(ctx, store.Search{
		UserID: u.ID, Source: "yahooauctions", Query: "q",
		Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.st.RecordListing(ctx, sid, "yahooauctions", source.Listing{
		ExternalID: "1238998047", Title: "t", Price: 1200, Currency: "JPY",
		URL: "https://auctions.yahoo.co.jp/jp/auction/1238998047",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	listings, _, err := e.st.ListingsPage(ctx, u.ID, "", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	views := e.srv.listingViews(listings, "EUR")
	if len(views) != 1 {
		t.Fatalf("got %d views", len(views))
	}
	if views[0].DoorzoURL != "" {
		t.Errorf("doorzo does not support yahoo auctions, got %q", views[0].DoorzoURL)
	}
	if views[0].BuyeeURL != "https://buyee.jp/item/yahoo/auction/1238998047" {
		t.Errorf("buyee url = %q", views[0].BuyeeURL)
	}
}

func TestYahooAndPayPaySupportCategoryFilter(t *testing.T) {
	for _, id := range []string{"yahooauctions", "paypayfleamarket", "mercari", "rakuma", "ebay"} {
		if !source.SupportsCategoryFilter(id) {
			t.Errorf("%s should be reported as supporting category exclusion", id)
		}
	}
	for _, id := range []string{"shpock", "vinted", "snkrdunk", "surugaya"} {
		if source.SupportsCategoryFilter(id) {
			t.Errorf("%s does not expose categories and must not claim support", id)
		}
	}
}

func TestBuyeeURLOnlyForYahoo(t *testing.T) {
	if got := source.BuyeeURL("yahooauctions", "1238998047"); got != "https://buyee.jp/item/yahoo/auction/1238998047" {
		t.Errorf("yahoo buyee url = %q", got)
	}
	for _, id := range []string{"mercari", "paypayfleamarket", "rakuma", "ebay"} {
		if got := source.BuyeeURL(id, "x1"); got != "" {
			t.Errorf("%s must not get a buyee url, got %q", id, got)
		}
	}
	if got := source.BuyeeURL("yahooauctions", ""); got != "" {
		t.Error("a blank external id must not produce a buyee url")
	}
}

func TestDoorzoNoLongerClaimsYahoo(t *testing.T) {
	if got := source.DoorzoURL("yahooauctions", "https://auctions.yahoo.co.jp/jp/auction/k1", "k1"); got != "" {
		t.Errorf("doorzo does not support yahoo auctions, got %q", got)
	}
	if got := source.DoorzoURL("paypayfleamarket", "", "z1"); got == "" {
		t.Error("paypay should still get a doorzo url")
	}
}

func TestCardLayoutElementsRender(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/", nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)
	for _, want := range []string{
		".card .aucrow", ".card .cardmeta", "function copyLink",
		"item.buyeeUrl", "'cardbtn','buyee'", "PayPay Flea Market and Yahoo! Auctions",
		"m.doorzoUrl || m.buyeeUrl",
		"open in buyee",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}

func TestMonitorViewYahooGetsBuyeeNotDoorzo(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")

	for _, m := range []store.MonitoredItem{
		{UserID: u.ID, Source: "yahooauctions", ExternalID: "1238998047",
			URL: "https://auctions.yahoo.co.jp/jp/auction/1238998047"},
		{UserID: u.ID, Source: "paypayfleamarket", ExternalID: "z651582616",
			URL: "https://buyee.jp/paypayfleamarket/item/z651582616"},
		{UserID: u.ID, Source: "ebay", ExternalID: "e1", URL: "https://ebay.com/itm/e1"},
	} {
		m.Title, m.Interval, m.Enabled, m.Status = "t", time.Hour, true, "active"
		if _, err := e.st.AddMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	views, err := e.srv.monitorViews(ctx, u.ID, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]monitorView{}
	for _, v := range views {
		by[v.Source] = v
	}

	y := by["yahooauctions"]
	if y.DoorzoURL != "" {
		t.Errorf("yahoo monitor must not offer doorzo, got %q", y.DoorzoURL)
	}
	if y.BuyeeURL != "https://buyee.jp/item/yahoo/auction/1238998047" {
		t.Errorf("yahoo monitor buyee url = %q", y.BuyeeURL)
	}

	p := by["paypayfleamarket"]
	if p.DoorzoURL == "" {
		t.Error("paypay monitor should still offer doorzo")
	}
	if p.BuyeeURL != "" {
		t.Errorf("paypay has no buyee item path, got %q", p.BuyeeURL)
	}

	if eb := by["ebay"]; eb.DoorzoURL != "" || eb.BuyeeURL != "" {
		t.Errorf("ebay should offer neither proxy, got doorzo=%q buyee=%q", eb.DoorzoURL, eb.BuyeeURL)
	}
}
