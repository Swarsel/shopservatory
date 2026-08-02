package web

import (
	"context"
	"encoding/json"
	"fmt"
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
		`id="f-params" class="autogrow" rows="1" data-size-ref="f-interval"`,
		`id="be-params" class="autogrow" rows="1" data-size-ref="be-interval"`,
		"function autoGrow",
		"initAutoGrow()",
		"line-height: normal",
		"border-width: 2px",
		"dataset.sizeRef",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
	if strings.Contains(body, `id="f-params" rows="2"`) {
		t.Error("the params field should no longer be a fixed two-row box")
	}
}

func TestHiddenSectionPaginatesAndSearches(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")
	sid, err := e.st.CreateSearch(ctx, store.Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		title := "plain item"
		if i%3 == 0 {
			title = "pikachu special"
		}
		if _, _, err := e.st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: fmt.Sprintf("h%03d", i), Title: fmt.Sprintf("%s %d", title, i),
			Price: float64(100 + i), Currency: "JPY", URL: fmt.Sprintf("https://e/%d", i),
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.SetListingHidden(ctx, u.ID, "mercari", fmt.Sprintf("h%03d", i), true); err != nil {
			t.Fatal(err)
		}
	}

	st := e.stateWith(t, "hidden=1&hpage=1")
	if tot := int(st["hiddenTotal"].(float64)); tot != 150 {
		t.Fatalf("hiddenTotal = %d want 150", tot)
	}
	if n := len(st["hidden"].([]any)); n != 100 {
		t.Fatalf("page 1 should hold 100 hidden items, got %d", n)
	}
	if p := int(st["hiddenPages"].(float64)); p != 2 {
		t.Errorf("hiddenPages = %d want 2", p)
	}

	st = e.stateWith(t, "hidden=1&hpage=2")
	if n := len(st["hidden"].([]any)); n != 50 {
		t.Fatalf("page 2 should hold the remaining 50, got %d", n)
	}
	if p := int(st["hiddenPage"].(float64)); p != 2 {
		t.Errorf("hiddenPage = %d want 2", p)
	}

	st = e.stateWith(t, "hidden=1&hpage=1&hq=pikachu")
	tot := int(st["hiddenTotal"].(float64))
	if tot != 50 {
		t.Fatalf("searching hidden items should match 50, got %d", tot)
	}
	for _, raw := range st["hidden"].([]any) {
		title := raw.(map[string]any)["title"].(string)
		if !strings.Contains(title, "pikachu") {
			t.Fatalf("search returned a non-matching item: %q", title)
		}
	}
}

func TestHiddenPageBeyondEndClampsBack(t *testing.T) {
	e := newPatchEnv(t)
	e.seedListing(t, "mercari", "one", "only item")
	if code, _ := e.post(t, "/listings/hide", url.Values{
		"source": {"mercari"}, "external_id": {"one"}, "hidden": {"1"},
	}); code != http.StatusNoContent {
		t.Fatal("hide failed")
	}
	st := e.stateWith(t, "hidden=1&hpage=9")
	if p := int(st["hiddenPage"].(float64)); p != 1 {
		t.Errorf("an out-of-range page must clamp to the last page, got %d", p)
	}
	if n := len(st["hidden"].([]any)); n != 1 {
		t.Errorf("clamped page should still return the item, got %d", n)
	}
}

func TestHiddenControlsRenderInDashboard(t *testing.T) {
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
		`id="hidden-filter"`, `id="hidden-thumbs"`, `id="hidden-prev"`, `id="hidden-next"`,
		`id="hidden-pageinfo"`, "hiddenThumbs", "cardacts",
		"function card(item, isHidden, noThumb)",
		"card(item, true, !hiddenThumbs)",
		"card(item, false, false)",
		"hideItem(item, hb, !isHidden)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
	for _, bad := range []string{
		"hideItem(item, hb, !noThumb)",
		"noThumb ? 'Unhide",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the hide button must not depend on thumbnail state: found %q", bad)
		}
	}
	for _, want := range []string{
		"cardacts",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}
