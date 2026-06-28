package source

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestWithinPriceBounds(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		spec SearchSpec
		p    float64
		want bool
	}{
		{"no bounds", SearchSpec{}, 50, true},
		{"above min", SearchSpec{MinPrice: f(10)}, 50, true},
		{"below min", SearchSpec{MinPrice: f(100)}, 50, false},
		{"below max", SearchSpec{MaxPrice: f(100)}, 50, true},
		{"above max", SearchSpec{MaxPrice: f(10)}, 50, false},
		{"within range", SearchSpec{MinPrice: f(10), MaxPrice: f(100)}, 50, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withinPriceBounds(c.spec, c.p); got != c.want {
				t.Fatalf("withinPriceBounds(%v, %v) = %v, want %v", c.spec, c.p, got, c.want)
			}
		})
	}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		u := uuidV4()
		if !uuidRe.MatchString(u) {
			t.Fatalf("uuidV4() = %q, not a valid v4 UUID", u)
		}
		if seen[u] {
			t.Fatalf("uuidV4() produced a duplicate: %q", u)
		}
		seen[u] = true
	}
}

func TestMercariDPoP(t *testing.T) {
	const htu = "https://api.mercari.jp/v2/entities:search"
	tok, err := mercariDPoP("POST", htu)
	if err != nil {
		t.Fatalf("mercariDPoP: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("DPoP must have 3 segments, got %d", len(parts))
	}

	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var hdr struct {
		Typ string         `json:"typ"`
		Alg string         `json:"alg"`
		JWK map[string]any `json:"jwk"`
	}
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if hdr.Typ != "dpop+jwt" || hdr.Alg != "ES256" {
		t.Fatalf("unexpected header: %+v", hdr)
	}
	if hdr.JWK["crv"] != "P-256" || hdr.JWK["kty"] != "EC" {
		t.Fatalf("unexpected jwk: %+v", hdr.JWK)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		HTU string `json:"htu"`
		HTM string `json:"htm"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.HTU != htu || claims.HTM != "POST" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("ES256 signature must be 64 bytes, got %d", len(sig))
	}
}

func TestKleinanzeigenURL(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		spec SearchSpec
		want string
	}{
		{"keyword", SearchSpec{Query: "murakami"}, "https://www.kleinanzeigen.de/s-murakami/k0"},
		{"multiword", SearchSpec{Query: "Louis Vuitton"}, "https://www.kleinanzeigen.de/s-louis-vuitton/k0"},
		{"range", SearchSpec{Query: "murakami", MinPrice: f(5), MaxPrice: f(15)}, "https://www.kleinanzeigen.de/s-preis:5:15/murakami/k0"},
		{"min only", SearchSpec{Query: "murakami", MinPrice: f(5)}, "https://www.kleinanzeigen.de/s-preis:5:/murakami/k0"},
		{"max only", SearchSpec{Query: "murakami", MaxPrice: f(15)}, "https://www.kleinanzeigen.de/s-preis::15/murakami/k0"},
		{"full url", SearchSpec{Query: "https://www.kleinanzeigen.de/s-murakami/k0"}, "https://www.kleinanzeigen.de/s-murakami/k0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := kleinanzeigenURL(c.spec); got != c.want {
				t.Fatalf("kleinanzeigenURL(%v) = %q, want %q", c.spec, got, c.want)
			}
		})
	}
}

func TestParseKleinanzeigenPrice(t *testing.T) {
	cases := map[string]float64{
		"10 €":           10,
		"15 € VB":        15,
		"1.234 €":        1234,
		"VB":             0,
		"Zu verschenken": 0,
		"":               0,
		"99,50 €":        99.5,
	}
	for in, want := range cases {
		if got := parseKleinanzeigenPrice(in); got != want {
			t.Fatalf("parseKleinanzeigenPrice(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBazarURL(t *testing.T) {
	t.Run("keyword", func(t *testing.T) {
		u, err := url.Parse(bazarURL(SearchSpec{Query: "rolex"}))
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/api/article/l/00-alle/h" {
			t.Fatalf("path = %q", u.Path)
		}
		q := u.Query()
		if q.Get("term") != "rolex" || q.Get("allShops") != "false" || q.Get("sort") != "sort.date,desc" {
			t.Fatalf("query = %v", q)
		}
	})
	t.Run("all_shops param", func(t *testing.T) {
		u, _ := url.Parse(bazarURL(SearchSpec{Query: "rolex", Params: map[string]string{"all_shops": "true"}}))
		if u.Query().Get("allShops") != "true" {
			t.Fatalf("allShops = %q", u.Query().Get("allShops"))
		}
	})
	t.Run("frontend url translated to api", func(t *testing.T) {
		got := bazarURL(SearchSpec{Query: "https://www.bazar.at/l/00-alle/h?term=rolex&page=0&size=20&sort=sort.date,desc"})
		if !strings.Contains(got, "/api/article/l/00-alle/h") || !strings.Contains(got, "term=rolex") {
			t.Fatalf("got %q", got)
		}
	})
}

func TestShpockURL(t *testing.T) {
	cases := []struct {
		name string
		spec SearchSpec
		want string
	}{
		{"keyword", SearchSpec{Query: "pokemon"}, "https://www.shpock.com/de-at/results?q=pokemon"},
		{"locale param", SearchSpec{Query: "pokemon", Params: map[string]string{"locale": "de-de"}}, "https://www.shpock.com/de-de/results?q=pokemon"},
		{"full url", SearchSpec{Query: "https://www.shpock.com/de-at/results?q=pokemon"}, "https://www.shpock.com/de-at/results?q=pokemon"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shpockURL(c.spec); got != c.want {
				t.Fatalf("shpockURL(%v) = %q, want %q", c.spec, got, c.want)
			}
		})
	}
}

func TestCraigslistTarget(t *testing.T) {
	cases := []struct {
		name    string
		spec    SearchSpec
		sub, q  string
		wantErr bool
	}{
		{"region param", SearchSpec{Query: "pokemon", Params: map[string]string{"region": "sfbay"}}, "sfbay", "pokemon", false},
		{"no region", SearchSpec{Query: "pokemon"}, "", "", true},
		{"legacy url", SearchSpec{Query: "https://sfbay.craigslist.org/search/sss?query=pokemon"}, "sfbay", "pokemon", false},
		{"new url", SearchSpec{Query: "https://www.craigslist.org/search/area/newyork?query=rolex"}, "newyork", "rolex", false},
		{"new url q param", SearchSpec{Query: "https://www.craigslist.org/search/area/seattle?q=bike"}, "seattle", "bike", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub, q, err := clTarget(c.spec)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && (sub != c.sub || q != c.q) {
				t.Fatalf("got (%q,%q), want (%q,%q)", sub, q, c.sub, c.q)
			}
		})
	}
}

func TestCraigslistURL(t *testing.T) {
	got := clURL("sfbay", "sby", "tag", "san-jose-pokemon", 7942946311)
	want := "https://sfbay.craigslist.org/sby/tag/d/san-jose-pokemon/7942946311.html"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := clURL("sfbay", "", "tag", "x", 1); got != "https://sfbay.craigslist.org/tag/d/x/1.html" {
		t.Fatalf("no-subarea form: %q", got)
	}
	if got := clURL("sfbay", "sby", "", "", 1); got != "https://sfbay.craigslist.org" {
		t.Fatalf("missing cat/slug should fall back to base: %q", got)
	}
}

func TestCraigslistImage(t *testing.T) {
	if got := clImage("3:00606_3AhkAUqtv1l_0CI0Bu"); got != "https://images.craigslist.org/00606_3AhkAUqtv1l_0CI0Bu_600x450.jpg" {
		t.Fatalf("got %q", got)
	}
	if got := clImage(""); got != "" {
		t.Fatalf("empty token should yield empty url, got %q", got)
	}
}

func TestCraigslistPrice(t *testing.T) {
	for in, want := range map[string]float64{"$99": 99, "$1,234": 1234, "": 0, "$10.50": 10.5} {
		if got := clPrice(in); got != want {
			t.Fatalf("clPrice(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCraigslistDecode(t *testing.T) {
	body := []byte(`{"data":{"items":[
		[37348350,2638039,132,1,"1:1~37.3~-121.8","0CI0Bu",[13,"abc"],[4,"3:img_x"],[6,"san-jose-pokemon-vintage"],[10,"$25"],"Pokemon vintage"],
		[1,2,96,1,"3:3:2~37.5~-121.9","0Cz0t2",[6,"fremont-switch"],[10,"$500"],"Nintendo Switch"]
	],"decode":{"minPostingId":7905597961,"locations":[0,[1,"sfbay","sby"],[1,"sfbay","pen"],[1,"sfbay","eby"]]}}}`)
	ls, err := clDecode(body, "sfbay", SearchSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 {
		t.Fatalf("want 2 listings, got %d", len(ls))
	}
	a := ls[0]
	if a.ExternalID != "7942946311" || a.Title != "Pokemon vintage" || a.Price != 25 || a.Currency != "USD" {
		t.Fatalf("listing[0] = %+v", a)
	}
	if a.ImageURL != "https://images.craigslist.org/img_x_600x450.jpg" {
		t.Fatalf("listing[0].ImageURL = %q", a.ImageURL)
	}
	if a.URL != "https://sfbay.craigslist.org/sby/tag/d/san-jose-pokemon-vintage/7942946311.html" {
		t.Fatalf("listing[0].URL = %q", a.URL)
	}
	if ls[1].URL != "https://sfbay.craigslist.org/eby/ele/d/fremont-switch/7905597962.html" {
		t.Fatalf("listing[1].URL = %q", ls[1].URL)
	}
	max := 100.0
	ls, _ = clDecode(body, "sfbay", SearchSpec{MaxPrice: &max})
	if len(ls) != 1 {
		t.Fatalf("price filter: want 1, got %d", len(ls))
	}
}

func TestEbayPriceFilter(t *testing.T) {
	e := &ebay{}
	f := func(v float64) *float64 { return &v }

	if got := e.priceFilter(SearchSpec{}); got != "" {
		t.Fatalf("empty spec: got %q", got)
	}
	got := e.priceFilter(SearchSpec{MinPrice: f(10), MaxPrice: f(100)})
	if got != "price:[10..100]" {
		t.Fatalf("range: got %q", got)
	}
	got = e.priceFilter(SearchSpec{
		MaxPrice: f(50),
		Params:   map[string]string{"filter": "buyingOptions:{FIXED_PRICE}"},
	})
	if got != "price:[..50],buyingOptions:{FIXED_PRICE}" {
		t.Fatalf("combined: got %q", got)
	}
}
