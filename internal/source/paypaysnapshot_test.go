package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const payPayItemHTML = `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Product",
"name":"pokemon card","image":["https://auctions.c.yimg.jp/img/i-img900x1200-1.jpg"],
"offers":{"@type":"Offer","priceCurrency":"JPY","price":11000,
"availability":"https://schema.org/OutOfStock"}}</script>
<script id="__NEXT_DATA__" type="application/json">{"props":{"initialState":{"itemsState":
{"items":{"item":{"id":"z651582616","title":"pokemon card","price":11000,"status":"SOLD"}}}}}}</script>
</head><body>x</body></html>`

const payPayOpenItemHTML = `<html><head>
<script type="application/ld+json">{"@type":"Product","name":"open item",
"offers":{"priceCurrency":"JPY","price":2500,"availability":"https://schema.org/InStock"}}</script>
<script id="__NEXT_DATA__" type="application/json">{"props":{"x":{"item":{"status":"OPEN"}}}}</script>
</head><body>x</body></html>`

func TestPayPayItemID(t *testing.T) {
	cases := map[string]string{
		"https://paypayfleamarket.yahoo.co.jp/item/z651582616":                                 "z651582616",
		"https://buyee.jp/paypayfleamarket/item/z651582616?conversionType=service_page_search": "z651582616",
		"https://paypayfleamarket.yahoo.co.jp/item/z1/extra":                                   "z1",
		"https://example.com/nothing":                                                          "",
		"":                                                                                     "",
	}
	for in, want := range cases {
		if got := payPayItemID(in); got != want {
			t.Errorf("payPayItemID(%q) = %q want %q", in, got, want)
		}
	}
}

func TestPayPayNextStatusMapping(t *testing.T) {
	cases := map[string]string{
		`{"status":"SOLD"}`:     "sold",
		`{"status":"SOLD_OUT"}`: "sold",
		`{"status":"STOP"}`:     "sold",
		`{"status":"DELETED"}`:  "removed",
		`{"status":"OPEN"}`:     "active",
	}
	for body, want := range cases {
		got, ok := payPayNextStatus([]byte(body))
		if !ok || got != want {
			t.Errorf("payPayNextStatus(%s) = %q,%v want %q", body, got, ok, want)
		}
	}
	if _, ok := payPayNextStatus([]byte(`{"status":"WEIRD"}`)); ok {
		t.Error("an unknown status must not be reported as recognised")
	}
	if _, ok := payPayNextStatus([]byte(`no json`)); ok {
		t.Error("missing status must not be reported as found")
	}
}

func TestPayPaySnapshotDirectDetectsSold(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(payPayItemHTML))
	}))
	defer srv.Close()

	p := newPayPayFleaMarket(nil, clientTo(t, srv.URL), quietLog())
	snap, err := p.snapshotDirect(context.Background(),
		"https://buyee.jp/paypayfleamarket/item/z651582616?conversionType=service_page_search")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/item/z651582616" {
		t.Errorf("must fetch the NATIVE item url, got path %q", gotPath)
	}
	if snap.Price != 11000 {
		t.Errorf("price = %v want 11000", snap.Price)
	}
	if snap.Status != "sold" {
		t.Errorf("a SOLD item must be reported sold, got %q", snap.Status)
	}
	if snap.Currency != "JPY" {
		t.Errorf("currency = %q", snap.Currency)
	}
}

func TestPayPaySnapshotDirectDetectsActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payPayOpenItemHTML))
	}))
	defer srv.Close()

	p := newPayPayFleaMarket(nil, clientTo(t, srv.URL), quietLog())
	snap, err := p.snapshotDirect(context.Background(), "https://paypayfleamarket.yahoo.co.jp/item/z9")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "active" || snap.Price != 2500 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestPayPaySnapshotDirectRequiresItemID(t *testing.T) {
	p := newPayPayFleaMarket(nil, &Client{}, quietLog())
	if _, err := p.snapshotDirect(context.Background(), "https://example.com/x"); err == nil {
		t.Fatal("a url without an item id must error rather than fetch")
	}
}

func TestPayPaySnapshotDirectRejectsUnusableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>EEA block page, no json</html>`))
	}))
	defer srv.Close()

	p := newPayPayFleaMarket(nil, clientTo(t, srv.URL), quietLog())
	if _, err := p.snapshotDirect(context.Background(), "https://paypayfleamarket.yahoo.co.jp/item/z1"); err == nil {
		t.Fatal("a page without usable ld+json must error so the caller can fall back")
	}
}
