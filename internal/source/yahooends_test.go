package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const yahooItemHTML = `<html><head>
<script type="application/ld+json">{"@type":"Product","name":"card",
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
