package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

func (e *patchEnv) seedListing(t *testing.T, src, extID, title string) int64 {
	t.Helper()
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")
	sid, err := e.st.CreateSearch(ctx, store.Search{
		UserID: u.ID, Source: src, Query: "q " + extID,
		Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.st.RecordListing(ctx, sid, src, source.Listing{
		ExternalID: extID, Title: title, Price: 100, Currency: "JPY",
		URL: "https://example.com/" + extID, ImageURL: "https://img/" + extID + ".jpg",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return sid
}

func (e *patchEnv) stateWith(t *testing.T, query string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/api/state?"+query, nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHideListingEndpointMovesItemToHidden(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "keepme", "keep this")
	e.seedListing(t, "mercari", "hideme", "hide this")

	st := e.stateWith(t, "page=1")
	if n := len(st["listings"].([]any)); n != 2 {
		t.Fatalf("baseline feed has %d listings, want 2", n)
	}

	code, body := e.post(t, "/listings/hide", url.Values{
		"source": {"mercari"}, "external_id": {"hideme"}, "hidden": {"1"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("hide status=%d body=%q", code, body)
	}

	st = e.stateWith(t, "page=1")
	listings := st["listings"].([]any)
	if len(listings) != 1 {
		t.Fatalf("feed should have 1 listing after hiding, got %d", len(listings))
	}
	if got := listings[0].(map[string]any)["externalId"]; got != "keepme" {
		t.Errorf("remaining listing = %v", got)
	}
	if tot := st["hiddenTotal"].(float64); tot != 1 {
		t.Errorf("hiddenTotal = %v want 1", tot)
	}
	if _, present := st["hidden"]; present {
		t.Error("hidden items must not be sent unless the section is open")
	}

	st = e.stateWith(t, "page=1&hidden=1")
	hidden := st["hidden"].([]any)
	if len(hidden) != 1 || hidden[0].(map[string]any)["externalId"] != "hideme" {
		t.Fatalf("hidden list = %v", hidden)
	}
}

func TestHiddenItemsAreExcludedFromFiltering(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "pika1", "pikachu one")
	e.seedListing(t, "mercari", "pika2", "pikachu two")

	if code, _ := e.post(t, "/listings/hide", url.Values{
		"source": {"mercari"}, "external_id": {"pika1"}, "hidden": {"1"},
	}); code != http.StatusNoContent {
		t.Fatal("hide failed")
	}

	st := e.stateWith(t, "page=1&q=pikachu")
	listings := st["listings"].([]any)
	if len(listings) != 1 {
		t.Fatalf("a filter must not match hidden items, got %d", len(listings))
	}
	if got := listings[0].(map[string]any)["externalId"]; got != "pika2" {
		t.Errorf("matched = %v", got)
	}
}

func TestUnhideRestoresToFeed(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "back", "come back")

	for _, h := range []string{"1", "0"} {
		if code, body := e.post(t, "/listings/hide", url.Values{
			"source": {"mercari"}, "external_id": {"back"}, "hidden": {h},
		}); code != http.StatusNoContent {
			t.Fatalf("hidden=%s status=%d body=%q", h, code, body)
		}
	}
	st := e.stateWith(t, "page=1")
	if n := len(st["listings"].([]any)); n != 1 {
		t.Fatalf("unhidden item should be back in the feed, got %d", n)
	}
	if tot := st["hiddenTotal"].(float64); tot != 0 {
		t.Errorf("hiddenTotal = %v want 0", tot)
	}
}

func TestHideListingRejectsBadInput(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "x1", "x")

	cases := []struct {
		name string
		form url.Values
		want int
	}{
		{"missing source", url.Values{"external_id": {"x1"}}, http.StatusBadRequest},
		{"missing id", url.Values{"source": {"mercari"}}, http.StatusBadRequest},
		{"unknown item", url.Values{"source": {"mercari"}, "external_id": {"nope"}}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := e.post(t, "/listings/hide", tc.form)
			if code != tc.want {
				t.Errorf("status=%d want %d", code, tc.want)
			}
		})
	}
}

func TestHideListingRequiresAuth(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "auth1", "x")
	form := url.Values{"source": {"mercari"}, "external_id": {"auth1"}, "hidden": {"1"}}
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/listings/hide", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Fatal("unauthenticated hide must not succeed")
	}
}

func TestHiddenSectionRendersInDashboard(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/", nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)
	for _, want := range []string{
		`id="hidden-head"`, `id="hidden-feed"`, "Hidden finds", "hideItem", "/listings/hide", "hiddenOpen",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}

func TestParamsTextareasAutoGrow(t *testing.T) {
	e := newPatchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/", nil)
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)

	for _, want := range []string{
		`textarea.autogrow`,
		`id="f-params" class="autogrow" rows="1"`,
		`id="be-params" class="autogrow" rows="1"`,
		"function autoGrow",
		"initAutoGrow()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
	if strings.Contains(body, `id="f-params" rows="2"`) {
		t.Error("the params field should no longer be a fixed two-row box")
	}
}
