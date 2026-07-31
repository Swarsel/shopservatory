package web

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/source"
)

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
