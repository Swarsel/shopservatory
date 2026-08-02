package source

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/config"
)

func configScrapeFor(string) config.Scrape {
	return config.Scrape{
		UserAgent: "test-agent",
		Timeout:   config.Duration{Duration: 10 * time.Second},
	}
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func clientTo(t *testing.T, base string) *Client {
	t.Helper()
	c, err := NewClient(configScrapeFor(base), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = rewriteTransport{base: base}
	return c
}

type rewriteTransport struct{ base string }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	target := strings.TrimPrefix(rt.base, "http://")
	u.Scheme = "http"
	u.Host = target
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return http.DefaultTransport.RoundTrip(r2)
}

const payPayAPIBody = `{"totalResultsAvailable":2,"items":[
{"id":"z111","title":"pikachu card","price":1500,"itemStatus":"OPEN","condition":"new",
 "openTime":"2026-08-02T08:23:54+09:00","thumbnailImageUrl":"https://img/1.jpg",
 "category":{"id":2420,"name":"TCG","path":[{"id":1,"name":"shopping"},{"id":2511,"name":"toys"},{"id":2420,"name":"TCG"}]}},
{"id":"z222","title":"charizard","price":9000,"itemStatus":"OPEN","condition":"used10",
 "openTime":"2026-08-02T09:00:00+09:00","thumbnailImageUrl":"https://img/2.jpg",
 "category":{"id":2420,"name":"TCG","path":[{"id":1,"name":"shopping"},{"id":2420,"name":"TCG"}]}}]}`

func TestPayPayUsesDirectAPIWhenJPClientPresent(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(payPayAPIBody))
	}))
	defer srv.Close()

	jp := clientTo(t, srv.URL)
	p := newPayPayFleaMarket(nil, jp, quietLog())

	got, err := p.Search(context.Background(), SearchSpec{Query: "pikachu"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/search" {
		t.Errorf("expected the JSON API path, got %q", gotPath)
	}
	if !strings.Contains(gotQuery, "order=DESC") {
		t.Errorf("order must be uppercase DESC or the API silently returns unsorted results: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "sort=openTime") {
		t.Errorf("expected newest-first sort, got %q", gotQuery)
	}
	if len(got) != 2 {
		t.Fatalf("got %d listings, want 2", len(got))
	}
	if got[0].ExternalID != "z111" || got[0].Price != 1500 || got[0].Currency != "JPY" {
		t.Errorf("bad first listing: %+v", got[0])
	}
	if got[0].URL != "https://paypayfleamarket.yahoo.co.jp/item/z111" {
		t.Errorf("URL must be the native item URL, got %q", got[0].URL)
	}
	if got[0].Extra["category"] != "2420" {
		t.Errorf("category = %q", got[0].Extra["category"])
	}
	if got[0].Extra["categories"] != "1,2511,2420" {
		t.Errorf("full ancestor chain expected, got %q", got[0].Extra["categories"])
	}
	if got[0].ListedAt.IsZero() {
		t.Error("openTime should parse into ListedAt")
	}
}

func TestPayPayFallsBackToBuyeeWhenVPNFails(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer dead.Close()

	var buyeeHit bool
	buyee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		buyeeHit = true
		_, _ = w.Write([]byte(`<html><body>
		  <li><a href="/paypayfleamarket/item/z999">x</a><h2 class="name">fallback item</h2><p class="price">2,000円</p></li>
		</body></html>`))
	}))
	defer buyee.Close()

	p := newPayPayFleaMarket(clientTo(t, buyee.URL), clientTo(t, dead.URL), quietLog())

	_, err := p.Search(context.Background(), SearchSpec{Query: "pikachu"})
	if buyeeHit {
		return
	}
	if err == nil {
		t.Fatal("expected the buyee path to be attempted after the VPN failed")
	}
	if !strings.Contains(err.Error(), "buyee") {
		t.Fatalf("a failing VPN must fall through to the buyee path, got: %v", err)
	}
}

func TestPayPayUsesBuyeeWhenNoJPClient(t *testing.T) {
	var buyeeHit bool
	buyee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		buyeeHit = true
		_, _ = w.Write([]byte(`<html></html>`))
	}))
	defer buyee.Close()
	_ = buyeeHit

	p := newPayPayFleaMarket(clientTo(t, buyee.URL), nil, quietLog())
	_, err := p.Search(context.Background(), SearchSpec{Query: "x"})
	if buyeeHit {
		return
	}
	if err == nil || !strings.Contains(err.Error(), "buyee") {
		t.Fatalf("with no JP client the buyee path must be used, got: %v", err)
	}
}

const yahooHTML = `<html><body>
<li><a class="Product__titleLink" href="https://auctions.yahoo.co.jp/jp/auction/k123">pokemon lot</a>
    <span class="Product__priceValue">4,900円</span><span class="Product__bid">3</span></li>
<li><a class="Product__titleLink" href="https://auctions.yahoo.co.jp/jp/auction/m456">charizard</a>
    <span class="Product__priceValue">12,000円</span><span class="Product__bid">7</span></li>
</body></html>`

func TestYahooUsesDirectWhenJPClientPresent(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(yahooHTML))
	}))
	defer srv.Close()

	y := newYahooAuctions(nil, clientTo(t, srv.URL), quietLog())
	got, err := y.Search(context.Background(), SearchSpec{Query: "pokemon"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "s1=new") || !strings.Contains(gotQuery, "o1=d") {
		t.Errorf("expected newest-first params, got %q", gotQuery)
	}
	if len(got) != 2 {
		t.Fatalf("got %d listings, want 2", len(got))
	}
	if got[0].ExternalID != "k123" {
		t.Errorf("ExternalID = %q, want k123", got[0].ExternalID)
	}
	if got[0].Price != 4900 {
		t.Errorf("price = %v, want 4900", got[0].Price)
	}
	if got[0].URL != "https://auctions.yahoo.co.jp/jp/auction/k123" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if got[0].SaleType != "auction" {
		t.Errorf("saleType = %q", got[0].SaleType)
	}
	if got[0].Extra["bids"] != "3" {
		t.Errorf("bids = %q", got[0].Extra["bids"])
	}
}

func TestYahooFallsBackToZenmarketWhenVPNFails(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer dead.Close()

	var zenHit bool
	zen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		zenHit = true
		_, _ = w.Write([]byte(`<html></html>`))
	}))
	defer zen.Close()

	y := newYahooAuctions(clientTo(t, zen.URL), clientTo(t, dead.URL), quietLog())
	_, _ = y.Search(context.Background(), SearchSpec{Query: "pokemon"})
	if !zenHit {
		t.Fatal("a failing VPN must fall back to zenmarket")
	}
}

func TestYahooEmptyDirectResultFallsBack(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>no results</body></html>`))
	}))
	defer empty.Close()

	var zenHit bool
	zen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		zenHit = true
		_, _ = w.Write([]byte(`<html></html>`))
	}))
	defer zen.Close()

	y := newYahooAuctions(clientTo(t, zen.URL), clientTo(t, empty.URL), quietLog())
	_, _ = y.Search(context.Background(), SearchSpec{Query: "pokemon"})
	if !zenHit {
		t.Fatal("an empty direct page (a geo-block interstitial) must fall back")
	}
}

func TestPayPayCategoryChain(t *testing.T) {
	c := payPayCategory{ID: 2420, Path: []payPayCategoryNode{{ID: 1}, {ID: 2511}, {ID: 2420}}}
	if got := c.chain(); got != "1,2511,2420" {
		t.Errorf("chain = %q", got)
	}
	c2 := payPayCategory{ID: 99, Path: []payPayCategoryNode{{ID: 1}}}
	if got := c2.chain(); got != "1,99" {
		t.Errorf("leaf should be appended when absent from path, got %q", got)
	}
	if got := (payPayCategory{}).chain(); got != "" {
		t.Errorf("empty category should yield empty chain, got %q", got)
	}
}
