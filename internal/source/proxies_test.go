package source

import "testing"

func TestDoorzoURLMatchesKnownExample(t *testing.T) {
	got := DoorzoURL("mercari", "https://jp.mercari.com/item/m99996350472", "m99996350472")
	want := "https://www.doorzo.com/en/mall/mercari/detail/" +
		"68747470733a2f2f6a702e6d6572636172692e636f6d2f6974656d2f6d3939393936333530343732"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestDoorzoMallPerSource(t *testing.T) {
	cases := map[string]string{
		"mercari":          "/mall/mercari/detail/",
		"surugaya":         "/mall/surugaya/detail/",
		"paypayfleamarket": "/mall/paypay/detail/",
		"rakuma":           "/mall/rakuma/detail/",
		"snkrdunk":         "/mall/snkrdunk/detail/",
	}
	for src, want := range cases {
		got := DoorzoURL(src, "https://example.com/item/1", "id1")
		if got == "" {
			t.Errorf("%s: expected a link", src)
			continue
		}
		if !contains(got, want) {
			t.Errorf("%s: %s missing %s", src, got, want)
		}
	}
}

func TestYahooAuctionsUsesBuyeeNotDoorzo(t *testing.T) {
	if got := DoorzoURL("yahooauctions", "https://example.com/item/1", "id1"); got != "" {
		t.Errorf("doorzo has no yahoo auctions mall, got %s", got)
	}
	if got := BuyeeURL("yahooauctions", "id1"); got != "https://buyee.jp/item/yahoo/auction/id1" {
		t.Errorf("buyee url = %s", got)
	}
}

func TestDoorzoUnsupportedSources(t *testing.T) {
	for _, src := range []string{"ebay", "willhaben", "vinted", "jmty", "magi", "auctionet"} {
		if got := DoorzoURL(src, "https://example.com/x", "1"); got != "" {
			t.Errorf("%s should have no doorzo link, got %s", src, got)
		}
	}
}

func TestNativeURLRebuiltForProxiedSources(t *testing.T) {
	pp := NativeItemURL("paypayfleamarket", "https://buyee.jp/paypayfleamarket/item/z1?x=1", "z651582616")
	if pp != "https://paypayfleamarket.yahoo.co.jp/item/z651582616" {
		t.Fatalf("paypay: %s", pp)
	}
	ya := NativeItemURL("yahooauctions", "https://zenmarket.jp/en/auction.aspx?itemCode=k1", "k1235221450")
	if ya != "https://page.auctions.yahoo.co.jp/jp/auction/k1235221450" {
		t.Fatalf("yahoo: %s", ya)
	}
	if dz := DoorzoURL("paypayfleamarket", "https://buyee.jp/x", "z1"); contains(dz, "6275796565") {
		t.Fatal("doorzo link must not encode the buyee proxy url")
	}
}

func TestNativeURLStripsQueryAndFragment(t *testing.T) {
	got := NativeItemURL("mercari", "https://jp.mercari.com/item/m1?utm=x#f", "m1")
	if got != "https://jp.mercari.com/item/m1" {
		t.Fatalf("got %s", got)
	}
}

func TestProxiedSourceWithoutIDGetsNoLink(t *testing.T) {
	if got := DoorzoURL("paypayfleamarket", "https://buyee.jp/paypayfleamarket/item/z1", ""); got != "" {
		t.Fatalf("expected no link, got %s", got)
	}
}

func TestExcludeTermsSplitting(t *testing.T) {
	spec := SearchSpec{Exclude: "ONE PIECE, reprint\n 未使用 ,,  "}
	got := spec.ExcludeTerms()
	want := []string{"one piece", "reprint", "未使用"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestTitleExcludedIsCaseInsensitiveSubstring(t *testing.T) {
	spec := SearchSpec{Exclude: "ONE PIECE"}
	if !spec.TitleExcluded("Rare one piece luffy card") {
		t.Fatal("should exclude a lowercase occurrence")
	}
	if spec.TitleExcluded("Pokemon Pikachu") {
		t.Fatal("should not exclude unrelated titles")
	}
	if (SearchSpec{}).TitleExcluded("anything") {
		t.Fatal("empty exclude must never exclude")
	}
}

func TestFilterExcludedDropsOnlyMatches(t *testing.T) {
	spec := SearchSpec{Exclude: "one piece"}
	in := []Listing{
		{Title: "Pikachu promo"},
		{Title: "ONE PIECE Luffy"},
		{Title: "Charizard"},
	}
	out := FilterExcluded(spec, in)
	if len(out) != 2 || out[0].Title != "Pikachu promo" || out[1].Title != "Charizard" {
		t.Fatalf("got %+v", out)
	}
	if len(FilterExcluded(SearchSpec{}, in)) != 3 {
		t.Fatal("no exclusions must pass everything through")
	}
}

func TestExcludedCategoryIDs(t *testing.T) {
	spec := SearchSpec{ExcludeCategories: "3088, 1328\n 23 "}
	got := spec.ExcludedCategoryIDs()
	if len(got) != 3 || got[0] != "3088" || got[1] != "1328" || got[2] != "23" {
		t.Fatalf("got %v", got)
	}
	if len((SearchSpec{}).ExcludedCategoryIDs()) != 0 {
		t.Fatal("empty must yield no ids")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
