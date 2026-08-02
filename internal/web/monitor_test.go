package web

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/source"
)

func testRegistry(t *testing.T) *source.Registry {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := source.NewClient(config.Scrape{}, log)
	if err != nil {
		t.Fatal(err)
	}
	return source.NewRegistry(config.Config{}, client, log)
}

func TestDetectMonitorSource(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := source.NewClient(config.Scrape{}, log)
	if err != nil {
		t.Fatal(err)
	}
	reg := source.NewRegistry(config.Config{}, client, log)

	cases := map[string]string{
		"https://buyee.jp/paypayfleamarket/item/z651582616?conversionType=service_page_search": "paypayfleamarket",
		"https://buyee.jp/item/jdirectitems/auction/k1235221450":                               "",
		"https://jp.mercari.com/item/m76345431606":                                             "mercari",
		"https://zenmarket.jp/en/auction.aspx?itemCode=x123":                                   "yahooauctions",
		"not a url": "",
	}
	for in, want := range cases {
		if got := detectMonitorSource(reg, in); got != want {
			t.Errorf("detectMonitorSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectMonitorSourceNativeYahooHosts(t *testing.T) {
	reg := testRegistry(t)
	cases := map[string]string{
		"https://auctions.yahoo.co.jp/jp/auction/1238998047":       "yahooauctions",
		"https://page.auctions.yahoo.co.jp/jp/auction/e1237529816": "yahooauctions",
		"https://auctions.yahoo.co.jp/search/search?p=x":           "yahooauctions",
		"https://paypayfleamarket.yahoo.co.jp/item/z651582616":     "paypayfleamarket",
		"https://zenmarket.jp/en/yahoo.aspx?itemCode=x":            "yahooauctions",
		"https://buyee.jp/paypayfleamarket/item/z1":                "paypayfleamarket",
	}
	for u, want := range cases {
		if got := detectMonitorSource(reg, u); got != want {
			t.Errorf("detectMonitorSource(%q) = %q want %q", u, got, want)
		}
	}
}

func TestDetectMonitorSourceRejectsOtherYahooProperties(t *testing.T) {
	reg := testRegistry(t)
	for _, u := range []string{
		"https://shopping.yahoo.co.jp/products/x",
		"https://news.yahoo.co.jp/articles/x",
		"https://auctions.yahoo.co.jp.evil.com/jp/auction/1",
	} {
		if got := detectMonitorSource(reg, u); got != "" {
			t.Errorf("detectMonitorSource(%q) = %q, want no match", u, got)
		}
	}
}
