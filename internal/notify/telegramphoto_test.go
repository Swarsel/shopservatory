package notify

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeoBlockedImageHosts(t *testing.T) {
	for _, h := range []string{
		"https://auc-pctr.c.yimg.jp/i/x.jpg",
		"https://auctions.c.yimg.jp/images.auctions.yahoo.co.jp/image/x.jpg",
		"https://yimg.jp/x.jpg",
		"https://paypayfleamarket.yahoo.co.jp/img/x.jpg",
	} {
		if !geoBlockedImage(h) {
			t.Errorf("%q should be uploaded rather than linked", h)
		}
	}
	for _, h := range []string{
		"https://static.mercdn.net/item/x.jpg",
		"https://img.fril.jp/x.jpg",
		"https://notyimg.jp.evil.com/x.jpg",
		"https://yahoo.co.jp.attacker.net/x.jpg",
		"not a url at all",
	} {
		if geoBlockedImage(h) {
			t.Errorf("%q must not be treated as geo-blocked", h)
		}
	}
}

func TestPhotoFilename(t *testing.T) {
	cases := map[string]string{
		"https://x.yimg.jp/a/b/i-img900x1200-123.jpg": "i-img900x1200-123.jpg",
		"https://x.yimg.jp/a/b/pic.png?w=1&h=2":       "pic.png",
		"https://x.yimg.jp/noextension":               "photo.jpg",
		"https://x.yimg.jp/":                          "photo.jpg",
		"::::":                                        "photo.jpg",
	}
	for in, want := range cases {
		if got := photoFilename(in); got != want {
			t.Errorf("photoFilename(%q) = %q want %q", in, got, want)
		}
	}
}

func TestUploadPhotoSendsMultipartBytes(t *testing.T) {
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("REALJPEGBYTES"))
	}))
	defer imgSrv.Close()

	var gotPhoto, gotCaption, gotChat, gotFilename string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		mt, params, err := mime.ParseMediaType(ct)
		if err != nil || !strings.HasPrefix(mt, "multipart/") {
			t.Errorf("expected multipart upload, got %q", ct)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "photo":
				gotPhoto = string(data)
				gotFilename = part.FileName()
			case "caption":
				gotCaption = string(data)
			case "chat_id":
				gotChat = string(data)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	tg := &Telegram{
		token:  "TOKEN",
		http:   &http.Client{Transport: redirectTo{api.URL}},
		images: &http.Client{Transport: redirectTo{imgSrv.URL}},
	}

	err := tg.uploadPhoto(context.Background(), "12345",
		"https://auc-pctr.c.yimg.jp/i/pic.jpg", "a caption")
	if err != nil {
		t.Fatal(err)
	}
	if gotPhoto != "REALJPEGBYTES" {
		t.Errorf("uploaded bytes = %q", gotPhoto)
	}
	if gotCaption != "a caption" {
		t.Errorf("caption = %q", gotCaption)
	}
	if gotChat != "12345" {
		t.Errorf("chat_id = %q", gotChat)
	}
	if gotFilename != "pic.jpg" {
		t.Errorf("filename = %q", gotFilename)
	}
}

func TestUploadPhotoRejectsNonImage(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>EEA block page</html>"))
	}))
	defer blocked.Close()

	tg := &Telegram{
		token:  "T",
		http:   &http.Client{},
		images: &http.Client{Transport: redirectTo{blocked.URL}},
	}
	err := tg.uploadPhoto(context.Background(), "1", "https://x.yimg.jp/a.jpg", "c")
	if err == nil {
		t.Fatal("an HTML block page must not be uploaded as a photo")
	}
	if !strings.Contains(err.Error(), "content-type") {
		t.Errorf("err = %v", err)
	}
}

func TestUploadPhotoRejectsErrorStatus(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer blocked.Close()

	tg := &Telegram{token: "T", http: &http.Client{},
		images: &http.Client{Transport: redirectTo{blocked.URL}}}
	if err := tg.uploadPhoto(context.Background(), "1", "https://x.yimg.jp/a.jpg", "c"); err == nil {
		t.Fatal("a 403 must be reported as an error")
	}
}

func TestSetImageProxyIgnoresEmptyAndBad(t *testing.T) {
	tg := &Telegram{}
	tg.SetImageProxy("")
	if tg.images != nil {
		t.Error("empty proxy url must leave the image client unset")
	}
	tg.SetImageProxy("socks5://127.0.0.1:1081")
	if tg.images == nil {
		t.Error("a valid proxy url must create the image client")
	}
}

type redirectTo struct{ base string }

func (rt redirectTo) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(rt.base, "http://")
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return http.DefaultTransport.RoundTrip(r2)
}
