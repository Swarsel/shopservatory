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
		{"yahooauctions", "1238998047", "https://auctions.yahoo.co.jp/jp/auction/1238998047", true},
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

	if dz := bySource["yahooauctions"].DoorzoURL; dz != "" {
		if want := "https://www.doorzo.com/en/mall/yahoo/detail/"; len(dz) <= len(want) || dz[:len(want)] != want {
			t.Errorf("yahoo doorzo url = %q", dz)
		}
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
	if views[0].DoorzoURL == "" {
		t.Fatal("a yahoo listing should expose a doorzo url in the feed")
	}
	if !strings.HasPrefix(views[0].DoorzoURL, "https://www.doorzo.com/en/mall/yahoo/detail/") {
		t.Errorf("doorzo url = %q", views[0].DoorzoURL)
	}
}
