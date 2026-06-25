package source

import (
	"encoding/base64"
	"encoding/json"
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
