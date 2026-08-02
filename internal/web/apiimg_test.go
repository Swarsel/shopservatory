package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAPIImageProxyAcceptsBearerToken(t *testing.T) {
	e := newPatchEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer upstream.Close()
	e.srv.images = &http.Client{Transport: rewriteHost{upstream.URL}}

	target := "https://auc-pctr.c.yimg.jp/i/x.jpg"
	req, _ := http.NewRequest(http.MethodGet,
		e.ts.URL+"/api/v1/img?u="+url.QueryEscape(target), nil)
	req.Header.Set("Authorization", "Bearer "+e.session.Value)

	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if string(body) != "jpeg-bytes" {
		t.Fatalf("body=%q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content-type=%q", ct)
	}
}

func TestAPIImageProxyRejectsUnauthenticated(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet,
		e.ts.URL+"/api/v1/img?u="+url.QueryEscape("https://example.com/x.jpg"), nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("unauthenticated image proxy access must not succeed")
	}
}

func TestCookieImageProxyStillWorks(t *testing.T) {
	e := newPatchEnv(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer upstream.Close()
	e.srv.images = &http.Client{Transport: rewriteHost{upstream.URL}}

	req, _ := http.NewRequest(http.MethodGet,
		e.ts.URL+"/img?u="+url.QueryEscape("https://static.mercdn.net/a.png"), nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the browser image proxy must keep working: status=%d", resp.StatusCode)
	}
}
