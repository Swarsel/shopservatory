package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const yahooItemHTML = `<html><head>
<script type="application/ld+json">{"@type":"Product","name":"card",
"image":["https://auctions.c.yimg.jp/img/i-img900x1200-1.jpg"],
"offers":{"priceCurrency":"JPY","price":"770000","priceValidUntil":"2026-08-08T16:00:52+09:00","availability":"https://schema.org/InStock"}}</script>
<script>window.__PRELOADED__={"saleCampaign":{"items":[{"campaignType":"FIRST_BID_CAMPAIGN","endTime":"2026-08-31T23:59:59+09:00"}]},
"startTime":"2026-08-01T11:25:02+09:00","endTime":"2026-08-08T16:00:52+09:00","leftTime":538013.777}</script>
</head><body>x</body></html>`

func TestYahooEndTimeUsesPriceValidUntil(t *testing.T) {
	got, ok := yahooEndTime([]byte(yahooItemHTML))
	if !ok {
		t.Fatal("end time should be found")
	}
	want := time.Date(2026, 8, 8, 16, 0, 52, 0, time.FixedZone("JST", 9*3600))
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
	if got.UTC().Format(time.RFC3339) != "2026-08-08T07:00:52Z" {
		t.Errorf("utc form = %s", got.UTC().Format(time.RFC3339))
	}
}

func TestYahooEndTimeIgnoresCampaignEndTime(t *testing.T) {
	got, ok := yahooEndTime([]byte(yahooItemHTML))
	if !ok {
		t.Fatal("expected a match")
	}
	if got.UTC().Format(time.RFC3339) == "2026-08-31T14:59:59Z" {
		t.Fatal("must not pick up the campaign endTime")
	}
}

func TestYahooEndTimeMissingOrInvalid(t *testing.T) {
	if _, ok := yahooEndTime([]byte(`<html>no json here</html>`)); ok {
		t.Error("no end time should be reported when absent")
	}
	if _, ok := yahooEndTime([]byte(`{"priceValidUntil":"not-a-date"}`)); ok {
		t.Error("an unparseable date must not be reported as found")
	}
}

func TestYahooNativeSnapshotIncludesTitleAndImage(t *testing.T) {
	snap, ok := yahooNativeSnapshot([]byte(yahooItemHTML))
	if !ok {
		t.Fatal("native snapshot should parse")
	}
	if snap.Title == "" {
		t.Error("a monitored yahoo item must get a title or the row renders blank")
	}
	if snap.ImageURL == "" {
		t.Error("a monitored yahoo item must get an image url or no thumbnail appears")
	}
	if snap.Title != "card" {
		t.Errorf("title = %q", snap.Title)
	}
	if snap.ImageURL != "https://auctions.c.yimg.jp/img/i-img900x1200-1.jpg" {
		t.Errorf("imageUrl = %q", snap.ImageURL)
	}
}

func TestYahooNativeSnapshotReadsOffersPrice(t *testing.T) {
	snap, ok := yahooNativeSnapshot([]byte(yahooItemHTML))
	if !ok {
		t.Fatal("native snapshot should parse")
	}
	if snap.Price != 770000 {
		t.Errorf("price = %v want 770000", snap.Price)
	}
	if snap.Currency != "JPY" || snap.SaleType != "auction" || snap.Status != "active" {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
	if snap.EndsAt.UTC().Format(time.RFC3339) != "2026-08-08T07:00:52Z" {
		t.Errorf("endsAt = %s", snap.EndsAt)
	}
}

func TestYahooNativeSnapshotRejectsPartialData(t *testing.T) {
	if _, ok := yahooNativeSnapshot([]byte(`<html>{"offers":{"price":"500"}}</html>`)); ok {
		t.Error("without an end time the native path must not claim success")
	}
	if _, ok := yahooNativeSnapshot([]byte(`{"priceValidUntil":"2026-08-08T16:00:52+09:00"}`)); ok {
		t.Error("without a price the native path must not claim success")
	}
}

func TestYahooEnrichListingReturnsEnds(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(yahooItemHTML))
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	price, saleType, extra, ok := y.EnrichListing(context.Background(), "e1237529816")
	if !ok {
		t.Fatal("enrichment should succeed")
	}
	if gotPath != "/jp/auction/e1237529816" {
		t.Errorf("path = %q", gotPath)
	}
	if saleType != "auction" {
		t.Errorf("saleType = %q", saleType)
	}
	if price != 0 {
		t.Errorf("price should be left alone by enrichment, got %v", price)
	}
	if extra["ends"] != "2026-08-08T07:00:52Z" {
		t.Errorf("ends = %q", extra["ends"])
	}
}

func TestYahooEnrichListingSkipsWithoutJPClient(t *testing.T) {
	y := newYahooAuctions(nil, nil, quietLog())
	if _, _, _, ok := y.EnrichListing(context.Background(), "e1"); ok {
		t.Fatal("without the Japan client enrichment must not run")
	}
}

func TestYahooEnrichListingHandlesFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	if _, _, _, ok := y.EnrichListing(context.Background(), "e1"); ok {
		t.Fatal("a failed fetch must report not-ok rather than empty data")
	}
}

func TestYahooEnrichListingRejectsBlankID(t *testing.T) {
	y := newYahooAuctions(nil, &Client{}, quietLog())
	if _, _, _, ok := y.EnrichListing(context.Background(), ""); ok {
		t.Fatal("a blank external id must not be fetched")
	}
}

func TestYahooNativeURLDetection(t *testing.T) {
	for _, u := range []string{
		"https://auctions.yahoo.co.jp/jp/auction/1238998047",
		"https://page.auctions.yahoo.co.jp/jp/auction/e1",
	} {
		if !yahooNativeURL(u) {
			t.Errorf("%q should be treated as a native yahoo url", u)
		}
	}
	for _, u := range []string{
		"https://zenmarket.jp/en/yahoo.aspx?itemCode=x",
		"https://auctions.yahoo.co.jp.evil.com/jp/auction/1",
		"::::",
	} {
		if yahooNativeURL(u) {
			t.Errorf("%q must not be treated as native", u)
		}
	}
}

func TestYahooSnapshotNativeNeedsJPClient(t *testing.T) {
	y := newYahooAuctions(&Client{}, nil, quietLog())
	_, err := y.Snapshot(context.Background(), "https://auctions.yahoo.co.jp/jp/auction/1238998047")
	if err == nil {
		t.Fatal("without the Japan proxy a native url must report a clear error")
	}
	if !errors.Is(err, errYahooNeedsJP) {
		t.Errorf("err = %v, want errYahooNeedsJP", err)
	}
}

func TestYahooSnapshotNativeUsesJPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(yahooItemHTML))
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	snap, err := y.Snapshot(context.Background(), "https://auctions.yahoo.co.jp/jp/auction/e1237529816")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Price != 770000 || snap.SaleType != "auction" {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
	if snap.EndsAt.UTC().Format(time.RFC3339) != "2026-08-08T07:00:52Z" {
		t.Errorf("endsAt = %s", snap.EndsAt)
	}
}

const yahooBreadcrumbHTML = `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[
{"@type":"ListItem","position":1,"item":{"@id":"https://auctions.yahoo.co.jp/","name":"top"}},
{"@type":"ListItem","position":2,"item":{"@id":"https://auctions.yahoo.co.jp/list4/25464-category.html","name":"toys"}},
{"@type":"ListItem","position":3,"item":{"@id":"https://auctions.yahoo.co.jp/list4/27727-category.html","name":"games"}},
{"@type":"ListItem","position":4,"item":{"@id":"https://auctions.yahoo.co.jp/category/list/2084317608","name":"single cards"}}]}</script>
<script type="application/ld+json">{"@type":"Product","name":"card","image":["https://x/1.jpg"],
"offers":{"priceCurrency":"JPY","price":"1200","priceValidUntil":"2026-08-03T22:05:18+09:00","availability":"https://schema.org/InStock"}}</script>
</head><body></body></html>`

func TestYahooCategoryChainFromBreadcrumbs(t *testing.T) {
	got := yahooCategoryChain([]byte(yahooBreadcrumbHTML))
	want := []string{"25464", "27727", "2084317608"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestYahooCategoryChainSkipsNonCategoryCrumbs(t *testing.T) {
	got := yahooCategoryChain([]byte(yahooBreadcrumbHTML))
	for _, id := range got {
		if id == "" {
			t.Error("blank ids must be skipped")
		}
	}
	if len(yahooCategoryChain([]byte(`<html>nothing here</html>`))) != 0 {
		t.Error("a page without breadcrumbs must yield no categories")
	}
}

func TestYahooEnrichListingReturnsCategoryChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(yahooBreadcrumbHTML))
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	_, _, extra, ok := y.EnrichListing(context.Background(), "e1")
	if !ok {
		t.Fatal("enrichment should succeed")
	}
	if extra["category"] != "2084317608" {
		t.Errorf("leaf category = %q want the deepest crumb", extra["category"])
	}
	if extra["categories"] != "25464,27727,2084317608" {
		t.Errorf("chain = %q", extra["categories"])
	}
	if extra["ends"] == "" {
		t.Error("end time should still be captured alongside categories")
	}
}

func TestYahooEnrichWorksWithCategoriesButNoEndTime(t *testing.T) {
	body := `<html><head><script type="application/ld+json">{"@type":"BreadcrumbList","itemListElement":[
	{"@type":"ListItem","position":1,"item":{"@id":"https://auctions.yahoo.co.jp/list4/25464-category.html"}}]}</script></head></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	_, _, extra, ok := y.EnrichListing(context.Background(), "e1")
	if !ok {
		t.Fatal("a page with categories but no end time should still enrich")
	}
	if extra["categories"] != "25464" {
		t.Errorf("categories = %q", extra["categories"])
	}
}

const yahooList5HTML = `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[
{"@type":"ListItem","position":1,"item":{"@id":"https://auctions.yahoo.co.jp/","name":"top"}},
{"@type":"ListItem","position":2,"item":{"@id":"https://auctions.yahoo.co.jp/list5/2084043920-category.html","name":"tickets"}},
{"@type":"ListItem","position":3,"item":{"@id":"https://auctions.yahoo.co.jp/list5/2084007688-category.html","name":"prepaid"}},
{"@type":"ListItem","position":4,"item":{"@id":"https://auctions.yahoo.co.jp/category/list/2084007698","name":"bus card"}}]}</script>
</head><body></body></html>`

func TestYahooCategoryChainAcceptsAnyListDepth(t *testing.T) {
	got := yahooCategoryChain([]byte(yahooList5HTML))
	want := []string{"2084043920", "2084007688", "2084007698"}
	if len(got) != len(want) {
		t.Fatalf("list5 breadcrumbs must all parse: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestYahooCategoryChainHandlesMixedListPaths(t *testing.T) {
	mixed := `<html><head><script type="application/ld+json">{"@type":"BreadcrumbList","itemListElement":[
	{"@type":"ListItem","position":1,"item":{"@id":"https://auctions.yahoo.co.jp/list4/25464-category.html"}},
	{"@type":"ListItem","position":2,"item":{"@id":"https://auctions.yahoo.co.jp/list5/2084007688-category.html"}},
	{"@type":"ListItem","position":3,"item":{"@id":"https://auctions.yahoo.co.jp/list/12345-category.html"}},
	{"@type":"ListItem","position":4,"item":{"@id":"https://auctions.yahoo.co.jp/category/list/999"}}]}</script></head></html>`
	got := yahooCategoryChain([]byte(mixed))
	want := "25464,2084007688,12345,999"
	if strings.Join(got, ",") != want {
		t.Errorf("chain = %q want %q", strings.Join(got, ","), want)
	}
}
