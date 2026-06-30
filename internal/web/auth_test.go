package web

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/auth"
	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/fx"
	"github.com/Swarsel/shopservatory/internal/notify"
	"github.com/Swarsel/shopservatory/internal/scheduler"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

func TestLoginTemplateParses(t *testing.T) {
	if _, err := template.New("login").Parse(loginTemplate); err != nil {
		t.Fatal(err)
	}
}

func TestLoginFlow(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()
	st, err := store.Open(ctx, t.TempDir()+"/a.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := auth.HashPassword("hunter2")
	_, _, err = st.SeedUser(ctx, "Leon", "leon@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	authn, _ := auth.New(ctx, st, auth.Options{}, log)
	c, _ := source.NewClient(config.Default().Scrape, log)
	reg := source.NewRegistry(config.Default(), c, log)
	conv := fx.New("EUR", log)
	sched := scheduler.New(st, reg, notify.NewManager(log, conv), log, scheduler.Options{})
	srv := New(st, reg, sched, conv, authn, 5*time.Minute, time.Hour, "", log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, _ := client.Get(ts.URL + "/")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("unauth /: status=%d loc=%q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, _ = client.Get(ts.URL + "/login")
	if resp.StatusCode != http.StatusOK || !strings.Contains(readN(resp), "Sign in") {
		t.Fatalf("login page status=%d", resp.StatusCode)
	}

	resp, _ = client.PostForm(ts.URL+"/login", url.Values{"email": {"leon@example.com"}, "password": {"wrong"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d", resp.StatusCode)
	}

	resp, _ = client.PostForm(ts.URL+"/login", url.Values{"email": {"leon@example.com"}, "password": {"hunter2"}})
	var session *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "shopservatory_session" {
			session = ck
		}
	}
	if resp.StatusCode != http.StatusSeeOther || session == nil || session.Value == "" {
		t.Fatalf("login status=%d cookie=%v", resp.StatusCode, session)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.AddCookie(session)
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authed dashboard status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/logout", nil)
	req.AddCookie(session)
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status=%d", resp.StatusCode)
	}
	if _, ok := st.SessionUserID(ctx, session.Value); ok {
		t.Fatal("session not deleted after logout")
	}
}

func TestAPIRequiresAuth(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()
	st, err := store.Open(ctx, t.TempDir()+"/a.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := auth.HashPassword("hunter2")
	u, _, err := st.SeedUser(ctx, "Leon", "leon@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	authn, _ := auth.New(ctx, st, auth.Options{}, log)
	c, _ := source.NewClient(config.Default().Scrape, log)
	reg := source.NewRegistry(config.Default(), c, log)
	conv := fx.New("EUR", log)
	sched := scheduler.New(st, reg, notify.NewManager(log, conv), log, scheduler.Options{})
	srv := New(st, reg, sched, conv, authn, 5*time.Minute, time.Hour, "", log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/api/v1/state")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/v1/state: status=%d, want 401", resp.StatusCode)
	}

	token, err := st.CreateSession(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/state", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session-token /api/v1/state: status=%d, want 200", resp.StatusCode)
	}
}

func readN(r *http.Response) string {
	b := make([]byte, 4096)
	n, _ := r.Body.Read(b)
	r.Body.Close()
	return string(b[:n])
}
