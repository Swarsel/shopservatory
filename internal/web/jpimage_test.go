package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJPImageHostMatching(t *testing.T) {
	for _, h := range []string{
		"auc-pctr.c.yimg.jp",
		"yimg.jp",
		"item-shopping.c.yimg.jp",
		"auctions.yahoo.co.jp",
		"paypayfleamarket.yahoo.co.jp",
	} {
		if !jpImageHost(h) {
			t.Errorf("%q should route through the Japan proxy", h)
		}
	}
	for _, h := range []string{
		"static.mercdn.net",
		"img.fril.jp",
		"jmty.jp",
		"example.com",
		"notyimg.jp.evil.com",
	} {
		if jpImageHost(h) {
			t.Errorf("%q must NOT be routed through the Japan proxy", h)
		}
	}
}

func TestJPImageProxyRoutingPicksCorrectClient(t *testing.T) {
	var jpHits, plainHits int
	jp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jpHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-from-japan"))
	}))
	defer jp.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-direct"))
	}))
	defer plain.Close()

	s := &Server{
		images:   &http.Client{Transport: rewriteHost{plain.URL}},
		imagesJP: &http.Client{Transport: rewriteHost{jp.URL}},
	}

	req := httptest.NewRequest(http.MethodGet, "/img", nil)

	resp, err := s.fetchImage(req, "https://auc-pctr.c.yimg.jp/i/x.jpg")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if jpHits != 1 || plainHits != 0 {
		t.Fatalf("a yimg.jp image must use the Japan client: jp=%d plain=%d", jpHits, plainHits)
	}

	resp, err = s.fetchImage(req, "https://static.mercdn.net/item/detail/orig/photos/x.jpg")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if plainHits != 1 {
		t.Fatalf("a non-Japan image must use the normal client: jp=%d plain=%d", jpHits, plainHits)
	}
}

func TestJPImageProxyUnsetFallsBackToNormalClient(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	s := &Server{images: &http.Client{Transport: rewriteHost{srv.URL}}}
	resp, err := s.fetchImage(httptest.NewRequest(http.MethodGet, "/img", nil),
		"https://auc-pctr.c.yimg.jp/i/x.jpg")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits != 1 {
		t.Fatal("with no Japan proxy configured the normal client must still be used")
	}
}

func TestSetJPImageProxyIgnoresEmpty(t *testing.T) {
	s := &Server{}
	s.SetJPImageProxy("")
	if s.imagesJP != nil {
		t.Fatal("an empty proxy url must leave the Japan client unset")
	}
	s.SetJPImageProxy("socks5://127.0.0.1:1081")
	if s.imagesJP == nil {
		t.Fatal("a configured proxy url must create the Japan client")
	}
}

type rewriteHost struct{ base string }

func (rt rewriteHost) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(rt.base, "http://")
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return http.DefaultTransport.RoundTrip(r2)
}
