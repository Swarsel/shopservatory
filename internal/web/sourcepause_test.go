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

	"github.com/Swarsel/shopservatory/internal/store"
)

func (e *patchEnv) post(t *testing.T, path string, form url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(e.session)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, strings.TrimSpace(string(body))
}

func (e *patchEnv) state(t *testing.T) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/api/state", nil)
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

func (e *patchEnv) sourcePaused(t *testing.T, src string) bool {
	t.Helper()
	st, _ := e.state(t)["settings"].(map[string]any)
	ex, _ := st["sourceExclude"].(map[string]any)
	row, _ := ex[src].(map[string]any)
	p, _ := row["paused"].(bool)
	return p
}

func TestSourcePauseEndpointTogglePreservesManualPause(t *testing.T) {
	e := newPatchEnv(t)
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "leon@example.com")

	manual, _ := e.st.CreateSearch(ctx, store.Search{
		UserID: u.ID, Source: "mercari", Query: "manual", Interval: time.Minute, Enabled: false,
	})
	running, _ := e.st.CreateSearch(ctx, store.Search{
		UserID: u.ID, Source: "mercari", Query: "running", Interval: time.Minute, Enabled: true,
	})

	scheduled := func() map[int64]bool {
		t.Helper()
		list, err := e.st.ListSearches(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, se := range list {
			out[se.ID] = true
		}
		return out
	}

	if code, body := e.post(t, "/settings/source/pause", url.Values{"source": {"mercari"}, "paused": {"1"}}); code != http.StatusNoContent {
		t.Fatalf("pause status=%d body=%q", code, body)
	}
	if !e.sourcePaused(t, "mercari") {
		t.Fatal("state should report mercari as paused")
	}
	if s := scheduled(); s[running] || s[manual] {
		t.Fatalf("no mercari search should be scheduled while paused: %v", s)
	}

	if code, body := e.post(t, "/settings/source/pause", url.Values{"source": {"mercari"}, "paused": {"0"}}); code != http.StatusNoContent {
		t.Fatalf("unpause status=%d body=%q", code, body)
	}
	if e.sourcePaused(t, "mercari") {
		t.Fatal("state should report mercari as running again")
	}
	s := scheduled()
	if !s[running] {
		t.Error("the previously running search must resume")
	}
	if s[manual] {
		t.Error("a manually paused search must stay paused after the global toggle cycle")
	}
}

func TestSourcePauseRejectsUnknownSource(t *testing.T) {
	e := newPatchEnv(t)
	code, body := e.post(t, "/settings/source/pause", url.Values{"source": {"nope"}, "paused": {"1"}})
	if code != http.StatusBadRequest || !strings.Contains(body, "unknown source") {
		t.Fatalf("status=%d body=%q", code, body)
	}
}

func TestSourcePauseRequiresAuth(t *testing.T) {
	e := newPatchEnv(t)
	form := url.Values{"source": {"mercari"}, "paused": {"1"}}
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/settings/source/pause", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Fatal("unauthenticated pause must not succeed")
	}
	paused, _ := e.st.PausedSources(context.Background(), 1)
	if paused["mercari"] {
		t.Fatal("unauthenticated request changed the pause state")
	}
}

func TestSourcePauseAndExclusionsCoexist(t *testing.T) {
	e := newPatchEnv(t)
	if code, _ := e.post(t, "/settings/source/pause", url.Values{"source": {"mercari"}, "paused": {"1"}}); code != http.StatusNoContent {
		t.Fatal("pause failed")
	}
	if code, _ := e.post(t, "/settings/source", url.Values{
		"source": {"mercari"}, "exclude": {"ONE PIECE"}, "exclude_categories": {""},
	}); code != http.StatusNoContent {
		t.Fatal("saving exclusions failed")
	}
	if !e.sourcePaused(t, "mercari") {
		t.Fatal("saving exclusions must not clear the pause")
	}

	if code, _ := e.post(t, "/settings/source", url.Values{
		"source": {"mercari"}, "exclude": {""}, "exclude_categories": {""},
	}); code != http.StatusNoContent {
		t.Fatal("clearing exclusions failed")
	}
	if !e.sourcePaused(t, "mercari") {
		t.Fatal("clearing exclusions must not clear the pause")
	}
}

func TestSourcePauseUIRenders(t *testing.T) {
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
	for _, want := range []string{"/settings/source/pause", "pausedSources", "source paused", "<th>Paused</th>"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}
