package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

type patchEnv struct {
	ts      *httptest.Server
	st      *store.Store
	srv     *Server
	client  *http.Client
	session *http.Cookie
}

func newPatchEnv(t *testing.T) *patchEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	st, err := store.Open(ctx, t.TempDir()+"/p.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	hash, _ := auth.HashPassword("hunter2")
	if _, _, err := st.SeedUser(ctx, "Leon", "leon@example.com", hash); err != nil {
		t.Fatal(err)
	}
	authn, _ := auth.New(ctx, st, auth.Options{}, log)
	c, _ := source.NewClient(config.Default().Scrape, log)
	reg := source.NewRegistry(config.Default(), c, log)
	conv := fx.New("EUR", log)
	sched := scheduler.New(st, reg, notify.NewManager(log, conv), log, scheduler.Options{})
	srv := New(st, reg, sched, conv, authn, 5*time.Minute, time.Hour, "", log)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"email": {"leon@example.com"}, "password": {"hunter2"}})
	if err != nil {
		t.Fatal(err)
	}
	var session *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "shopservatory_session" {
			session = ck
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	return &patchEnv{ts: ts, st: st, srv: srv, client: client, session: session}
}

func (e *patchEnv) patch(t *testing.T, form url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/searches/patch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, strings.TrimSpace(readN(resp))
}

func (e *patchEnv) newSearch(t *testing.T, params map[string]string) int64 {
	t.Helper()
	u, err := e.st.UserByEmail(context.Background(), "leon@example.com")
	if err != nil {
		t.Fatal(err)
	}
	id, err := e.st.CreateSearch(context.Background(), store.Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute,
		Enabled: true, Params: params,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBulkPatchInterval(t *testing.T) {
	e := newPatchEnv(t)
	a := e.newSearch(t, nil)
	b := e.newSearch(t, nil)

	code, body := e.patch(t, url.Values{
		"id":       {strconv.FormatInt(a, 10), strconv.FormatInt(b, 10)},
		"interval": {"6h"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", code, body)
	}
	for _, id := range []int64{a, b} {
		se, _ := e.st.GetSearch(context.Background(), id)
		if se.Interval != 6*time.Hour {
			t.Errorf("search %d interval = %v", id, se.Interval)
		}
	}
}

func TestBulkPatchRejectsBadInput(t *testing.T) {
	e := newPatchEnv(t)
	id := strconv.FormatInt(e.newSearch(t, nil), 10)

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"no ids", url.Values{"interval": {"1h"}}, "no search ids"},
		{"bad interval", url.Values{"id": {id}, "interval": {"soon"}}, "invalid interval"},
		{"zero interval", url.Values{"id": {id}, "interval": {"0s"}}, "invalid interval"},
		{"bad min", url.Values{"id": {id}, "min_price": {"cheap"}}, "invalid min price"},
		{"bad max", url.Values{"id": {id}, "max_price": {"lots"}}, "invalid max price"},
		{"empty patch", url.Values{"id": {id}}, "nothing to change"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := e.patch(t, tc.form)
			if code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
				t.Fatalf("status=%d body=%q want %q", code, body, tc.want)
			}
		})
	}
}

func TestBulkPatchTickedEmptyExclusionClears(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	id := e.newSearch(t, nil)
	if _, err := e.st.PatchSearches(ctx, 1, []int64{id}, store.SearchPatch{
		Exclude: strptr("ONE PIECE"), ExcludeCategories: strptr("3088"),
	}); err != nil {
		t.Fatal(err)
	}

	code, body := e.patch(t, url.Values{
		"id":                 {strconv.FormatInt(id, 10)},
		"exclude":            {""},
		"exclude_categories": {""},
	})
	if code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", code, body)
	}
	se, _ := e.st.GetSearch(ctx, id)
	if se.Exclude != "" || se.ExcludeCategories != "" {
		t.Fatalf("ticking with a blank value must clear: exclude=%q cats=%q", se.Exclude, se.ExcludeCategories)
	}
}

func TestBulkPatchTickedEmptyParams(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()

	replaceID := e.newSearch(t, map[string]string{"sort": "newest"})
	code, body := e.patch(t, url.Values{
		"id":          {strconv.FormatInt(replaceID, 10)},
		"params":      {""},
		"params_mode": {"replace"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("replace-blank status=%d body=%q", code, body)
	}
	if se, _ := e.st.GetSearch(ctx, replaceID); len(se.Params) != 0 {
		t.Fatalf("replace with a blank box must clear params, got %v", se.Params)
	}

	mergeID := e.newSearch(t, map[string]string{"sort": "newest"})
	code, body = e.patch(t, url.Values{
		"id":          {strconv.FormatInt(mergeID, 10)},
		"params":      {""},
		"params_mode": {"merge"},
	})
	if code != http.StatusBadRequest || !strings.Contains(body, "use replace mode") {
		t.Fatalf("merge-blank should explain how to clear params, got status=%d body=%q", code, body)
	}
	if se, _ := e.st.GetSearch(ctx, mergeID); se.Params["sort"] != "newest" {
		t.Fatalf("merge-blank must not touch params, got %v", se.Params)
	}
}

func TestBulkPatchClearsPriceWithNegative(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	id := e.newSearch(t, nil)
	five := 5.0
	if _, err := e.st.PatchSearches(ctx, 1, []int64{id}, store.SearchPatch{MinPrice: &five}); err != nil {
		t.Fatal(err)
	}
	code, body := e.patch(t, url.Values{
		"id":        {strconv.FormatInt(id, 10)},
		"min_price": {"-1"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", code, body)
	}
	if se, _ := e.st.GetSearch(ctx, id); se.MinPrice != nil {
		t.Fatalf("-1 should clear min price, got %v", *se.MinPrice)
	}
}

func TestBulkPatchRequiresAuth(t *testing.T) {
	e := newPatchEnv(t)
	id := strconv.FormatInt(e.newSearch(t, nil), 10)
	form := url.Values{"id": {id}, "interval": {"6h"}}
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/searches/patch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusNoContent {
		t.Fatal("unauthenticated bulk patch must not succeed")
	}
	if se, _ := e.st.GetSearch(context.Background(), 1); se.Interval == 6*time.Hour {
		t.Fatal("unauthenticated request changed data")
	}
}

func TestBulkEditDialogRenders(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/", nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	body := string(raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	for _, want := range []string{
		`id="bulkedit"`, `id="be-interval"`, `id="be-params-mode"`,
		`id="be-apply"`, `id="bulk-edit"`, `Edit selected`, `edit all`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}

func strptr(s string) *string { return &s }

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
