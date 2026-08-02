package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUserFromIdentity(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	u1, err := st.UserFromIdentity(ctx, "sub-1", "a@example.com", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.UserFromIdentity(ctx, "sub-1", "a@example.com", "Alice")
	if err != nil || again.ID != u1.ID {
		t.Fatalf("same subject should return same user: %v id=%d want %d", err, again.ID, u1.ID)
	}
	byEmail, err := st.UserFromIdentity(ctx, "", "a@example.com", "")
	if err != nil || byEmail.ID != u1.ID {
		t.Fatalf("lookup by email should match: %v id=%d", err, byEmail.ID)
	}
	u2, err := st.UserFromIdentity(ctx, "sub-2", "b@example.com", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID == u1.ID {
		t.Fatal("different identities must be different users")
	}
}

func TestPerUserScoping(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	alice, _ := st.UserFromIdentity(ctx, "sub-a", "a@example.com", "Alice")
	bob, _ := st.UserFromIdentity(ctx, "sub-b", "b@example.com", "Bob")

	aSearch, err := st.CreateSearch(ctx, Search{UserID: alice.ID, Source: "mercari", Query: "a", Interval: time.Minute, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSearch(ctx, Search{UserID: bob.ID, Source: "mercari", Query: "b", Interval: time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if got, _ := st.ListSearchesForUser(ctx, alice.ID); len(got) != 1 || got[0].Query != "a" {
		t.Fatalf("alice should see only her search, got %+v", got)
	}
	if got, _ := st.ListSearchesForUser(ctx, bob.ID); len(got) != 1 || got[0].Query != "b" {
		t.Fatalf("bob should see only his search, got %+v", got)
	}

	if _, _, err := st.RecordListing(ctx, aSearch, "mercari", source.Listing{ExternalID: "x1", Title: "Item"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, total, _ := st.ListingsPage(ctx, alice.ID, "", nil, 100, 0); len(got) != 1 || total != 1 {
		t.Fatalf("alice feed should have 1, got %d (total %d)", len(got), total)
	}
	if got, total, _ := st.ListingsPage(ctx, bob.ID, "", nil, 100, 0); len(got) != 0 || total != 0 {
		t.Fatalf("bob feed must not see alice's listing, got %d (total %d)", len(got), total)
	}
	if _, total, _ := st.ListingsPage(ctx, alice.ID, "item", nil, 100, 0); total != 1 {
		t.Fatalf("title filter should match 1, got %d", total)
	}
	if _, total, _ := st.ListingsPage(ctx, alice.ID, "nomatch", nil, 100, 0); total != 0 {
		t.Fatalf("filter should match 0, got %d", total)
	}
	if _, total, _ := st.ListingsPage(ctx, alice.ID, "nomatch", []string{"mercari"}, 100, 0); total != 1 {
		t.Fatalf("source filter should match 1, got %d", total)
	}
}

func TestImageSearchRoundTrip(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	u, _ := st.UserFromIdentity(ctx, "sub-i", "i@example.com", "Ida")
	img := []byte{0xff, 0xd8, 0xff, 0xe0, 0x01, 0x02, 0x03}
	id, err := st.CreateSearch(ctx, Search{UserID: u.ID, Source: "mercari", Query: "image: card.png", Interval: time.Minute, Enabled: true, Image: img})
	if err != nil {
		t.Fatal(err)
	}
	se, err := st.GetSearch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(se.Image) != string(img) {
		t.Fatalf("image blob did not round-trip: %v", se.Image)
	}
	se.Query = "renamed"
	se.Interval = 2 * time.Minute
	if err := st.UpdateSearch(ctx, se); err != nil {
		t.Fatal(err)
	}
	se2, _ := st.GetSearch(ctx, id)
	if string(se2.Image) != string(img) || se2.Query != "renamed" {
		t.Fatalf("update must preserve image, got %q img %d bytes", se2.Query, len(se2.Image))
	}
	list, _ := st.ListSearches(ctx, true)
	found := false
	for _, s := range list {
		if s.ID == id && len(s.Image) == len(img) {
			found = true
		}
	}
	if !found {
		t.Fatal("scheduler listing must include the image")
	}
}

func TestDeleteSearches(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice, _ := st.UserFromIdentity(ctx, "sub-da", "da@example.com", "Alice")
	bob, _ := st.UserFromIdentity(ctx, "sub-db", "db@example.com", "Bob")

	a1, _ := st.CreateSearch(ctx, Search{UserID: alice.ID, Source: "mercari", Query: "x", Interval: time.Minute, Enabled: true})
	a2, _ := st.CreateSearch(ctx, Search{UserID: alice.ID, Source: "ebay", Query: "x", Interval: time.Minute, Enabled: true})
	b1, _ := st.CreateSearch(ctx, Search{UserID: bob.ID, Source: "mercari", Query: "y", Interval: time.Minute, Enabled: true})

	n, err := st.DeleteSearches(ctx, alice.ID, []int64{a1, a2, b1})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected to delete 2 of alice's searches, got %d", n)
	}
	if got, _ := st.ListSearchesForUser(ctx, alice.ID); len(got) != 0 {
		t.Fatalf("alice should have 0 searches, got %d", len(got))
	}
	if got, _ := st.ListSearchesForUser(ctx, bob.ID); len(got) != 1 {
		t.Fatalf("bob's search must survive alice's bulk delete, got %d", len(got))
	}
	if n, _ := st.DeleteSearches(ctx, alice.ID, nil); n != 0 {
		t.Fatalf("empty delete should be a no-op, got %d", n)
	}
}

func TestBulkEnableAndOwnership(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice, _ := st.UserFromIdentity(ctx, "sub-be-a", "bea@example.com", "Alice")
	bob, _ := st.UserFromIdentity(ctx, "sub-be-b", "beb@example.com", "Bob")

	a1, _ := st.CreateSearch(ctx, Search{UserID: alice.ID, Source: "mercari", Query: "x", Interval: time.Minute, Enabled: true})
	a2, _ := st.CreateSearch(ctx, Search{UserID: alice.ID, Source: "ebay", Query: "x", Interval: time.Minute, Enabled: true})
	b1, _ := st.CreateSearch(ctx, Search{UserID: bob.ID, Source: "mercari", Query: "y", Interval: time.Minute, Enabled: true})

	n, err := st.SetSearchesEnabled(ctx, alice.ID, []int64{a1, a2, b1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 of alice's searches paused, got %d", n)
	}
	got, _ := st.ListSearchesForUser(ctx, alice.ID)
	for _, se := range got {
		if se.Enabled {
			t.Fatalf("alice's search %d should be paused", se.ID)
		}
	}
	if bs, _ := st.ListSearchesForUser(ctx, bob.ID); !bs[0].Enabled {
		t.Fatal("bob's search must remain enabled")
	}

	owned, err := st.OwnedSearchIDs(ctx, alice.ID, []int64{a1, a2, b1})
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 2 {
		t.Fatalf("OwnedSearchIDs must exclude bob's id, got %v", owned)
	}
}

func TestMonitorPauseAndArchive(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-mon", "mon@example.com", "Mona")

	id, err := st.AddMonitor(ctx, MonitoredItem{
		UserID: u.ID, Source: "mercari", ExternalID: "m1", URL: "https://x/m1",
		Title: "Item", LastPrice: 100, Status: "active", Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	due, _ := st.DueMonitors(ctx)
	if len(due) != 1 {
		t.Fatalf("expected 1 due monitor, got %d", len(due))
	}

	if err := st.SetMonitorEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if due, _ = st.DueMonitors(ctx); len(due) != 0 {
		t.Fatalf("paused monitor must not be due, got %d", len(due))
	}
	if m, _ := st.GetMonitor(ctx, id); m.Enabled || m.Archived {
		t.Fatalf("expected paused+unarchived, got %+v", m)
	}

	if err := st.SetMonitorEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMonitorArchived(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if due, _ = st.DueMonitors(ctx); len(due) != 0 {
		t.Fatalf("archived monitor must not be due, got %d", len(due))
	}
	m, _ := st.GetMonitor(ctx, id)
	if !m.Archived || m.LastPrice != 100 {
		t.Fatalf("archive must keep the row and final price, got %+v", m)
	}
	if hist, _ := st.PriceHistory(ctx, id); len(hist) != 1 {
		t.Fatalf("price history must survive archiving, got %d points", len(hist))
	}
}

func TestExcludeFieldsRoundTrip(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-ex", "ex@example.com", "Ex")

	id, err := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "murakami", Interval: time.Minute, Enabled: true,
		Exclude: "ONE PIECE, reprint", ExcludeCategories: "3088",
	})
	if err != nil {
		t.Fatal(err)
	}
	se, err := st.GetSearch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if se.Exclude != "ONE PIECE, reprint" || se.ExcludeCategories != "3088" {
		t.Fatalf("round-trip failed: %q / %q", se.Exclude, se.ExcludeCategories)
	}
	if spec := se.Spec(); spec.Exclude != se.Exclude || spec.ExcludeCategories != se.ExcludeCategories {
		t.Fatal("Spec() must carry the exclusions to the sources")
	}

	se.Exclude = "only this"
	se.ExcludeCategories = ""
	if err := st.UpdateSearch(ctx, se); err != nil {
		t.Fatal(err)
	}
	se2, _ := st.GetSearch(ctx, id)
	if se2.Exclude != "only this" || se2.ExcludeCategories != "" {
		t.Fatalf("update failed: %q / %q", se2.Exclude, se2.ExcludeCategories)
	}
}

func TestSourceExclusionsMergeIntoSpec(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-gx", "gx@example.com", "Gx")

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", Exclude: "ONE PIECE", ExcludeCategories: "3088",
	}); err != nil {
		t.Fatal(err)
	}

	id, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "murakami", Interval: time.Minute, Enabled: true,
		Exclude: "reprint", ExcludeCategories: "1328",
	})
	se, _ := st.GetSearch(ctx, id)
	spec := st.EffectiveSpec(ctx, se)

	terms := spec.ExcludeTerms()
	if len(terms) != 2 || terms[0] != "one piece" || terms[1] != "reprint" {
		t.Fatalf("expected global then per-search terms, got %v", terms)
	}
	cats := spec.ExcludedCategoryIDs()
	if len(cats) != 2 || cats[0] != "3088" || cats[1] != "1328" {
		t.Fatalf("expected merged categories, got %v", cats)
	}

	other, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "ebay", Query: "x", Interval: time.Minute, Enabled: true,
	})
	oseSpec := st.EffectiveSpec(ctx, mustGet(t, st, other))
	if len(oseSpec.ExcludeTerms()) != 0 {
		t.Fatalf("mercari exclusions must not leak to ebay, got %v", oseSpec.ExcludeTerms())
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{Source: "mercari"}); err != nil {
		t.Fatal(err)
	}
	spec2 := st.EffectiveSpec(ctx, se)
	if len(spec2.ExcludeTerms()) != 1 || spec2.ExcludeTerms()[0] != "reprint" {
		t.Fatalf("clearing the global rule should leave only the search's own, got %v", spec2.ExcludeTerms())
	}
}

func mustGet(t *testing.T, st *Store, id int64) Search {
	t.Helper()
	se, err := st.GetSearch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return se
}

func TestFeedHidesExcludedListings(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fh", "fh@example.com", "Fh")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "murakami", Interval: time.Minute, Enabled: true,
	})

	titles := []string{
		"Murakami flower plush",
		"ONE PIECE Luffy figure",
		"one piece lowercase variant",
		"村上隆 ファッション scarf",
	}
	for i, ti := range titles {
		if _, _, err := st.RecordListing(ctx, sid, "mercari",
			source.Listing{ExternalID: string(rune('a' + i)), Title: ti}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0); total != 4 {
		t.Fatalf("baseline expected 4 listings, got %d", total)
	}

	se, _ := st.GetSearch(ctx, sid)
	se.Exclude = "ONE PIECE"
	if err := st.UpdateSearch(ctx, se); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0); total != 2 {
		t.Fatalf("a per-search term must hide both case variants, got %d", total)
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", Exclude: "ファッション",
	}); err != nil {
		t.Fatal(err)
	}
	got, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0)
	if total != 1 || got[0].Title != "Murakami flower plush" {
		t.Fatalf("a global term must also hide, got %d: %+v", total, got)
	}

	if _, total, _ := st.ListingsPage(ctx, u.ID, "plush", nil, 100, 0); total != 1 {
		t.Fatalf("hiding must compose with the text filter, got %d", total)
	}
}

func TestFeedHidesExcludedCategories(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fc", "fc@example.com", "Fc")
	msid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "murakami", Interval: time.Minute, Enabled: true,
	})
	esid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "ebay", Query: "murakami", Interval: time.Minute, Enabled: true,
	})

	add := func(sid int64, src, id, title, cat, chain string) {
		extra := map[string]string{"category": cat}
		if chain != "" {
			extra["categories"] = chain
		}
		if _, _, err := st.RecordListing(ctx, sid, src,
			source.Listing{ExternalID: id, Title: title, Extra: extra}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	add(msid, "mercari", "m1", "fashion scarf", "23", "23,1,3088")
	add(msid, "mercari", "m2", "trading card", "1328", "1328,82")
	add(msid, "mercari", "m3", "no chain item", "3088", "")
	add(esid, "ebay", "e1", "ebay item in cat 23", "23", "23,220")

	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0); total != 4 {
		t.Fatalf("baseline expected 4, got %d", total)
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", ExcludeCategories: "3088",
	}); err != nil {
		t.Fatal(err)
	}
	got, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0)
	titles := map[string]bool{}
	for _, l := range got {
		titles[l.Title] = true
	}
	if titles["fashion scarf"] {
		t.Error("a parent id must hide an item via its stored ancestor chain")
	}
	if titles["no chain item"] {
		t.Error("an exact leaf match must still hide")
	}
	if !titles["trading card"] {
		t.Error("an unrelated mercari category must be kept")
	}
	if !titles["ebay item in cat 23"] {
		t.Error("a mercari exclusion must not affect ebay")
	}
	if total != 2 {
		t.Fatalf("expected 2 remaining, got %d: %v", total, titles)
	}
}

func TestFeedCategoryExclusionKeepsUncategorizedListings(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-nc", "nc@example.com", "Nc")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "x", Interval: time.Minute, Enabled: true,
	})

	add := func(id, title string, extra map[string]string) {
		if _, _, err := st.RecordListing(ctx, sid, "mercari",
			source.Listing{ExternalID: id, Title: title, Extra: extra}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	add("a", "fashion scarf", map[string]string{"category": "23", "categories": "23,1,3088"})
	add("b", "trading card", map[string]string{"category": "741", "categories": "741,82,1328"})
	add("c", "leaf equals excluded", map[string]string{"category": "3088"})
	add("d", "legacy no category at all", nil)
	add("e", "other extra keys only", map[string]string{"location": "Tokyo"})

	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0); total != 5 {
		t.Fatalf("baseline expected 5, got %d", total)
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", ExcludeCategories: "3088",
	}); err != nil {
		t.Fatal(err)
	}
	got, total, err := st.ListingsPage(ctx, u.ID, "", nil, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, l := range got {
		kept[l.Title] = true
	}
	if kept["fashion scarf"] {
		t.Error("an item whose ancestor chain contains 3088 must be hidden")
	}
	if kept["leaf equals excluded"] {
		t.Error("an item whose leaf category is 3088 must be hidden")
	}
	// The regression: NULL json_extract made the whole clause NULL, so rows
	// with no category were silently filtered out along with the fashion ones.
	if !kept["legacy no category at all"] {
		t.Error("a listing with no category must never be hidden by a category rule")
	}
	if !kept["other extra keys only"] {
		t.Error("a listing whose extra lacks a category key must never be hidden")
	}
	if !kept["trading card"] {
		t.Error("an unrelated category must be kept")
	}
	if total != 3 {
		t.Fatalf("expected 3 kept, got %d: %v", total, kept)
	}
}

func TestFeedCategoryExclusionMatchesParentViaChainPerSource(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-pc", "pc@example.com", "Pc")
	esid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "ebay", Query: "x", Interval: time.Minute, Enabled: true,
	})
	msid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "x", Interval: time.Minute, Enabled: true,
	})

	add := func(sid int64, src, id, title string, extra map[string]string) {
		if _, _, err := st.RecordListing(ctx, sid, src,
			source.Listing{ExternalID: id, Title: title, Extra: extra}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	add(esid, "ebay", "e1", "nike shirt", map[string]string{
		"category": "15687", "categories": "15687,1059,11450,185100,260012",
	})
	add(esid, "ebay", "e2", "pokemon card", map[string]string{
		"category": "183454", "categories": "183454,2536,220",
	})
	add(esid, "ebay", "e3", "legacy ebay", nil)
	add(msid, "mercari", "m1", "mercari item in 11450-ish", map[string]string{
		"category": "11450", "categories": "11450",
	})

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "ebay", ExcludeCategories: "11450",
	}); err != nil {
		t.Fatal(err)
	}
	got, total, err := st.ListingsPage(ctx, u.ID, "", nil, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, l := range got {
		kept[l.Title] = true
	}
	if kept["nike shirt"] {
		t.Error("an ebay parent id in the chain must hide the listing")
	}
	if !kept["pokemon card"] {
		t.Error("an unrelated ebay category must be kept")
	}
	if !kept["legacy ebay"] {
		t.Error("an ebay listing with no category must never be hidden")
	}
	if !kept["mercari item in 11450-ish"] {
		t.Error("an ebay rule must not hide mercari listings sharing the id")
	}
	if total != 3 {
		t.Fatalf("expected 3 kept, got %d: %v", total, kept)
	}
}

func TestRepopulateSearch(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-rp", "rp@example.com", "Rp")

	a, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "rakuma", Query: "a", Interval: time.Minute, Enabled: true,
	})
	b, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "rakuma", Query: "b", Interval: time.Minute, Enabled: true,
	})
	for i, sid := range []int64{a, a, b} {
		if _, _, err := st.RecordListing(ctx, sid, "rakuma",
			source.Listing{
				ExternalID: string(rune('a' + i)),
				Title:      "item",
				Extra:      map[string]string{"category": "735"},
			}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.TouchSearchRun(ctx, a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if se, _ := st.GetSearch(ctx, a); se.LastRunAt == nil {
		t.Fatal("precondition: search a should have run")
	}

	removed, err := st.RepopulateSearch(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 listings removed, got %d", removed)
	}

	se, _ := st.GetSearch(ctx, a)
	if se.LastRunAt != nil {
		t.Error("last_run_at must be cleared so the next poll re-seeds silently")
	}
	if se.Query != "a" || !se.Enabled {
		t.Error("the search itself must survive repopulation")
	}

	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 100, 0); total != 1 {
		t.Fatalf("only search b's listing should remain, got %d", total)
	}
	if seB, _ := st.GetSearch(ctx, b); seB.LastRunAt != nil {
		t.Error("repopulating one search must not touch another")
	}
}

func TestRepopulateLeavesMonitorsIntact(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-rm", "rm@example.com", "Rm")

	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "rakuma", Query: "q", Interval: time.Minute, Enabled: true,
	})
	if _, _, err := st.RecordListing(ctx, sid, "rakuma",
		source.Listing{ExternalID: "x1", Title: "a find"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	mid, err := st.AddMonitor(ctx, MonitoredItem{
		UserID: u.ID, Source: "rakuma", ExternalID: "x1", URL: "https://item.fril.jp/x1",
		Title: "watched item", LastPrice: 4200, Status: "active",
		Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMonitorCheck(ctx, mid, source.ItemSnapshot{
		Price: 4000, Status: "active", Currency: "JPY",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	before, _ := st.PriceHistory(ctx, mid)
	if len(before) < 2 {
		t.Fatalf("precondition: expected price history, got %d", len(before))
	}

	if _, err := st.RepopulateSearch(ctx, sid); err != nil {
		t.Fatal(err)
	}

	mons, _ := st.ListMonitors(ctx, u.ID)
	if len(mons) != 1 {
		t.Fatalf("repopulating a search must not delete monitors, got %d", len(mons))
	}
	m := mons[0]
	if m.Title != "watched item" || m.LastPrice != 4000 || !m.Enabled {
		t.Fatalf("monitor was altered: %+v", m)
	}
	after, _ := st.PriceHistory(ctx, mid)
	if len(after) != len(before) {
		t.Fatalf("price history must survive: had %d, now %d", len(before), len(after))
	}
}

func TestPatchSearchesOnlyChangesGivenFields(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-pt", "pt@example.com", "Pt")
	other, _ := st.UserFromIdentity(ctx, "sub-pt2", "pt2@example.com", "Pt2")

	min := 10.0
	a, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "keep me", Interval: time.Minute,
		Enabled: true, MinPrice: &min, Exclude: "old", ExcludeCategories: "3088",
		Params: map[string]string{"sort": "newest", "status": "all"},
	})
	b, _ := st.CreateSearch(ctx, Search{
		UserID: other.ID, Source: "mercari", Query: "not mine", Interval: time.Minute, Enabled: true,
	})

	d := 2 * time.Hour
	n, err := st.PatchSearches(ctx, u.ID, []int64{a, b}, SearchPatch{Interval: &d})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("must only patch the caller's searches, got %d", n)
	}

	se, _ := st.GetSearch(ctx, a)
	if se.Interval != d {
		t.Errorf("interval not applied: %v", se.Interval)
	}
	if se.Query != "keep me" {
		t.Errorf("query must be untouched, got %q", se.Query)
	}
	if se.MinPrice == nil || *se.MinPrice != 10 {
		t.Error("min price must be untouched")
	}
	if se.Exclude != "old" || se.ExcludeCategories != "3088" {
		t.Error("exclusions must be untouched when not in the patch")
	}
	if se.Params["sort"] != "newest" || se.Params["status"] != "all" {
		t.Errorf("params must be untouched, got %v", se.Params)
	}

	if seB, _ := st.GetSearch(ctx, b); seB.Interval != time.Minute {
		t.Error("another user's search must never be patched")
	}
}

func TestPatchSearchesParamsMergeAndReplace(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-pp", "pp@example.com", "Pp")
	id, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
		Params: map[string]string{"sort": "newest", "status": "all"},
	})

	if _, err := st.PatchSearches(ctx, u.ID, []int64{id}, SearchPatch{
		Params: map[string]string{"sort": "price_asc", "domain": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	se, _ := st.GetSearch(ctx, id)
	if se.Params["sort"] != "price_asc" || se.Params["status"] != "all" || se.Params["domain"] != "x" {
		t.Fatalf("merge should overwrite and add, keeping others: %v", se.Params)
	}

	if _, err := st.PatchSearches(ctx, u.ID, []int64{id}, SearchPatch{
		Params: map[string]string{"status": ""},
	}); err != nil {
		t.Fatal(err)
	}
	se, _ = st.GetSearch(ctx, id)
	if _, still := se.Params["status"]; still {
		t.Fatalf("an empty value should remove a param: %v", se.Params)
	}

	if _, err := st.PatchSearches(ctx, u.ID, []int64{id}, SearchPatch{
		Params: map[string]string{"only": "this"}, ReplaceParams: true,
	}); err != nil {
		t.Fatal(err)
	}
	se, _ = st.GetSearch(ctx, id)
	if len(se.Params) != 1 || se.Params["only"] != "this" {
		t.Fatalf("replace should drop everything else: %v", se.Params)
	}
}

func TestPatchSearchesClearsPricesWithNegative(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-pc2", "pc2@example.com", "Pc")
	min, max := 5.0, 50.0
	id, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
		MinPrice: &min, MaxPrice: &max,
	})

	clear := -1.0
	if _, err := st.PatchSearches(ctx, u.ID, []int64{id}, SearchPatch{MinPrice: &clear}); err != nil {
		t.Fatal(err)
	}
	se, _ := st.GetSearch(ctx, id)
	if se.MinPrice != nil {
		t.Errorf("a negative min should clear the bound, got %v", *se.MinPrice)
	}
	if se.MaxPrice == nil || *se.MaxPrice != 50 {
		t.Error("max must be untouched")
	}
}

func TestPatchSearchesEmptyIsNoOp(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-pe", "pe@example.com", "Pe")
	id, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	n, err := st.PatchSearches(ctx, u.ID, []int64{id}, SearchPatch{})
	if err != nil || n != 0 {
		t.Fatalf("an empty patch must change nothing, got n=%d err=%v", n, err)
	}
}

func TestSourcePauseKeepsManualPauses(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-sp", "sp@example.com", "Sp")

	manual, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "manually paused", Interval: time.Minute, Enabled: false,
	})
	running, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "running", Interval: time.Minute, Enabled: true,
	})
	other, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "rakuma", Query: "other source", Interval: time.Minute, Enabled: true,
	})

	enabledIDs := func() map[int64]bool {
		t.Helper()
		list, err := st.ListSearches(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]bool{}
		for _, se := range list {
			got[se.ID] = true
		}
		return got
	}

	if got := enabledIDs(); !got[running] || !got[other] || got[manual] {
		t.Fatalf("baseline wrong: %v", got)
	}

	if err := st.SetSourcePaused(ctx, u.ID, "mercari", true); err != nil {
		t.Fatal(err)
	}
	if got := enabledIDs(); got[running] || got[manual] || !got[other] {
		t.Fatalf("pausing mercari must hide only mercari searches: %v", got)
	}

	for _, id := range []int64{manual, running} {
		se, _ := st.GetSearch(ctx, id)
		if id == running && !se.Enabled {
			t.Error("a global pause must not clear the per-search enabled flag")
		}
		if id == manual && se.Enabled {
			t.Error("a manually paused search must stay disabled")
		}
	}

	if err := st.SetSourcePaused(ctx, u.ID, "mercari", false); err != nil {
		t.Fatal(err)
	}
	got := enabledIDs()
	if !got[running] {
		t.Error("unpausing must bring back the search that was running")
	}
	if got[manual] {
		t.Error("unpausing must NOT resurrect a manually paused search")
	}
	if !got[other] {
		t.Error("the untouched source must still run")
	}
}

func TestSourcePauseIsPerUser(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.UserFromIdentity(ctx, "sub-pa", "pa@example.com", "A")
	b, _ := st.UserFromIdentity(ctx, "sub-pb", "pb@example.com", "B")

	sa, _ := st.CreateSearch(ctx, Search{UserID: a.ID, Source: "mercari", Query: "a", Interval: time.Minute, Enabled: true})
	sb, _ := st.CreateSearch(ctx, Search{UserID: b.ID, Source: "mercari", Query: "b", Interval: time.Minute, Enabled: true})

	if err := st.SetSourcePaused(ctx, a.ID, "mercari", true); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListSearches(ctx, true)
	seen := map[int64]bool{}
	for _, se := range list {
		seen[se.ID] = true
	}
	if seen[sa] {
		t.Error("user A's mercari search should be paused")
	}
	if !seen[sb] {
		t.Error("user B must be unaffected by user A's pause")
	}
}

func TestSourcePauseSurvivesExclusionEdits(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-se", "se@example.com", "Se")

	if err := st.SetSourcePaused(ctx, u.ID, "mercari", true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{Source: "mercari", Exclude: "ONE PIECE"}); err != nil {
		t.Fatal(err)
	}
	paused, _ := st.PausedSources(ctx, u.ID)
	if !paused["mercari"] {
		t.Fatal("setting exclusions must not clear the pause")
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{Source: "mercari"}); err != nil {
		t.Fatal(err)
	}
	paused, _ = st.PausedSources(ctx, u.ID)
	if !paused["mercari"] {
		t.Fatal("clearing exclusions must not clear the pause")
	}
	ex, _ := st.SourceExclusions(ctx, u.ID)
	if ex["mercari"].Exclude != "" {
		t.Fatalf("exclusions should be cleared, got %q", ex["mercari"].Exclude)
	}

	if err := st.SetSourcePaused(ctx, u.ID, "mercari", false); err != nil {
		t.Fatal(err)
	}
	ex, _ = st.SourceExclusions(ctx, u.ID)
	if len(ex) != 0 {
		t.Fatalf("a fully empty row should be cleaned up, got %v", ex)
	}
}

func TestSourceExclusionsReportPaused(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-sr", "sr@example.com", "Sr")

	if err := st.SetSourcePaused(ctx, u.ID, "yahoo", true); err != nil {
		t.Fatal(err)
	}
	ex, err := st.SourceExclusions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ex["yahoo"].Paused {
		t.Fatal("SourceExclusions must surface the paused flag for the UI")
	}
}

func TestPausedColumnMigratesOntoOldDB(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/old.db"

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT UNIQUE);
		CREATE TABLE source_exclusions (
		    user_id            INTEGER NOT NULL,
		    source             TEXT NOT NULL,
		    exclude            TEXT NOT NULL DEFAULT '',
		    exclude_categories TEXT NOT NULL DEFAULT '',
		    PRIMARY KEY (user_id, source)
		);
		INSERT INTO users (name, email) VALUES ('Old', 'old@example.com');
		INSERT INTO source_exclusions (user_id, source, exclude) VALUES (1, 'mercari', 'ONE PIECE');
	`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("opening a pre-existing db must migrate cleanly: %v", err)
	}
	defer st.Close()

	ex, err := st.SourceExclusions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ex["mercari"].Exclude != "ONE PIECE" {
		t.Fatalf("existing exclusions must survive: %v", ex)
	}
	if ex["mercari"].Paused {
		t.Fatal("pre-existing rows must default to not paused")
	}

	if err := st.SetSourcePaused(ctx, 1, "mercari", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.PausedSources(ctx, 1); !p["mercari"] {
		t.Fatal("pausing must work after migration")
	}
}

func TestHideListingRemovesItFromTheFeed(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-hd", "hd@example.com", "Hd")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	now := time.Now()
	for _, id := range []string{"keep1", "hideme", "keep2"} {
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: id, Title: "item " + id, Price: 100, Currency: "JPY",
			URL: "https://example.com/" + id,
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	feedIDs := func() []string {
		t.Helper()
		list, _, err := st.ListingsPage(ctx, u.ID, "", nil, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(list))
		for _, l := range list {
			out = append(out, l.ExternalID)
		}
		return out
	}
	hiddenIDs := func() []string {
		t.Helper()
		list, _, err := st.HiddenListingsPage(ctx, u.ID, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(list))
		for _, l := range list {
			out = append(out, l.ExternalID)
		}
		return out
	}

	if len(feedIDs()) != 3 || len(hiddenIDs()) != 0 {
		t.Fatalf("baseline wrong: feed=%v hidden=%v", feedIDs(), hiddenIDs())
	}

	n, err := st.SetListingHidden(ctx, u.ID, "mercari", "hideme", true)
	if err != nil || n != 1 {
		t.Fatalf("hide: n=%d err=%v", n, err)
	}

	feed := feedIDs()
	for _, id := range feed {
		if id == "hideme" {
			t.Error("a hidden item must not appear in the feed")
		}
	}
	if len(feed) != 2 {
		t.Errorf("feed = %v, want 2 items", feed)
	}
	if got := hiddenIDs(); len(got) != 1 || got[0] != "hideme" {
		t.Errorf("hidden = %v, want [hideme]", got)
	}

	if _, err := st.SetListingHidden(ctx, u.ID, "mercari", "hideme", false); err != nil {
		t.Fatal(err)
	}
	if len(feedIDs()) != 3 || len(hiddenIDs()) != 0 {
		t.Errorf("unhiding should restore it: feed=%v hidden=%v", feedIDs(), hiddenIDs())
	}
}

func TestHiddenListingsAreNotSearchable(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-hs", "hs@example.com", "Hs")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	now := time.Now()
	for _, id := range []string{"pika1", "pika2"} {
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: id, Title: "pikachu " + id, Price: 100, Currency: "JPY",
			URL: "https://example.com/" + id,
		}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetListingHidden(ctx, u.ID, "mercari", "pika1", true); err != nil {
		t.Fatal(err)
	}

	list, total, err := st.ListingsPage(ctx, u.ID, "pikachu", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 || list[0].ExternalID != "pika2" {
		t.Fatalf("a filtered search must skip hidden items: total=%d list=%v", total, list)
	}
}

func TestHidingAppliesAcrossDuplicateSearches(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-hdup", "hdup@example.com", "Hdup")
	now := time.Now()
	var sids []int64
	for i := 0; i < 2; i++ {
		sid, _ := st.CreateSearch(ctx, Search{
			UserID: u.ID, Source: "mercari", Query: fmt.Sprintf("q%d", i),
			Interval: time.Minute, Enabled: true,
		})
		sids = append(sids, sid)
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: "shared", Title: "shared item", Price: 100, Currency: "JPY",
			URL: "https://example.com/shared",
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.SetListingHidden(ctx, u.ID, "mercari", "shared", true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("hiding should affect every copy of the item, got %d rows", n)
	}
	list, _, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0)
	if len(list) != 0 {
		t.Fatalf("the item must be hidden even though a second search also found it: %v", list)
	}
	if h, _, _ := st.HiddenListingsPage(ctx, u.ID, 50, 0); len(h) != 1 {
		t.Fatalf("hidden section should show it once, got %d", len(h))
	}
	_ = sids
}

func TestHidingIsScopedToTheOwner(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.UserFromIdentity(ctx, "sub-ha", "ha@example.com", "A")
	b, _ := st.UserFromIdentity(ctx, "sub-hb", "hb@example.com", "B")
	now := time.Now()
	for _, u := range []User{a, b} {
		sid, _ := st.CreateSearch(ctx, Search{
			UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
		})
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: "same", Title: "same item", Price: 1, Currency: "JPY",
			URL: "https://example.com/same",
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := st.SetListingHidden(ctx, a.ID, "mercari", "same", true); err != nil {
		t.Fatal(err)
	}
	if list, _, _ := st.ListingsPage(ctx, a.ID, "", nil, 50, 0); len(list) != 0 {
		t.Error("user A should no longer see the item")
	}
	if list, _, _ := st.ListingsPage(ctx, b.ID, "", nil, 50, 0); len(list) != 1 {
		t.Error("user B must be unaffected by user A hiding an item")
	}
}

func TestHiddenColumnMigratesOntoOldListings(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/oldlist.db"

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT UNIQUE);
		CREATE TABLE searches (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, source TEXT,
		                       query TEXT, params TEXT DEFAULT '{}', interval_seconds INTEGER DEFAULT 300,
		                       enabled INTEGER DEFAULT 1, created_at INTEGER DEFAULT 0);
		CREATE TABLE listings (
		    id INTEGER PRIMARY KEY AUTOINCREMENT, search_id INTEGER NOT NULL,
		    source TEXT NOT NULL, external_id TEXT NOT NULL, title TEXT NOT NULL,
		    price REAL DEFAULT 0, currency TEXT DEFAULT '', url TEXT DEFAULT '',
		    image_url TEXT DEFAULT '', sale_type TEXT DEFAULT '', extra TEXT DEFAULT '{}',
		    first_seen INTEGER NOT NULL, notified INTEGER DEFAULT 0,
		    UNIQUE(search_id, external_id));
		INSERT INTO users (name, email) VALUES ('Old','old@example.com');
		INSERT INTO searches (id, user_id, source, query) VALUES (1, 1, 'mercari', 'q');
		INSERT INTO listings (search_id, source, external_id, title, first_seen)
		     VALUES (1, 'mercari', 'legacy1', 'legacy item', 1000);
	`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("opening a pre-existing db must migrate cleanly: %v", err)
	}
	defer st.Close()

	list, total, err := st.ListingsPage(ctx, 1, "", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 || list[0].ExternalID != "legacy1" {
		t.Fatalf("existing listings must stay visible: total=%d list=%v", total, list)
	}
	if h, ht, _ := st.HiddenListingsPage(ctx, 1, 10, 0); ht != 0 || len(h) != 0 {
		t.Fatal("pre-existing listings must default to not hidden")
	}
	if _, err := st.SetListingHidden(ctx, 1, "mercari", "legacy1", true); err != nil {
		t.Fatal(err)
	}
	if _, ht, _ := st.HiddenListingsPage(ctx, 1, 10, 0); ht != 1 {
		t.Fatal("hiding must work after migration")
	}
}

func feedItemCount(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM feed_items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFeedItemsStayConsistentWithListings(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fi", "fi@example.com", "Fi")
	now := time.Now()

	s1, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "a", Interval: time.Minute, Enabled: true,
	})
	s2, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "b", Interval: time.Minute, Enabled: true,
	})

	rec := func(sid int64, extID string, seen time.Time) {
		t.Helper()
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: extID, Title: "item " + extID, Price: 10, Currency: "JPY",
			URL: "https://example.com/" + extID,
		}, seen); err != nil {
			t.Fatal(err)
		}
	}

	rec(s1, "only1", now)
	rec(s1, "shared", now)
	rec(s2, "shared", now.Add(time.Hour))
	rec(s2, "only2", now)

	if got := feedItemCount(t, st); got != 3 {
		t.Fatalf("feed_items should hold one row per distinct item, got %d", got)
	}
	list, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0)
	if total != 3 || len(list) != 3 {
		t.Fatalf("feed should show 3 items, got total=%d len=%d", total, len(list))
	}

	if _, err := st.db.ExecContext(ctx,
		`DELETE FROM listings WHERE search_id = ? AND external_id = 'shared'`, s2); err != nil {
		t.Fatal(err)
	}
	if got := feedItemCount(t, st); got != 3 {
		t.Fatalf("removing one copy of a shared item must keep the item, got %d rows", got)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 3 {
		t.Errorf("shared item should still be visible via its other search, total=%d", total)
	}

	if _, err := st.db.ExecContext(ctx,
		`DELETE FROM listings WHERE search_id = ? AND external_id = 'shared'`, s1); err != nil {
		t.Fatal(err)
	}
	if got := feedItemCount(t, st); got != 2 {
		t.Fatalf("removing the last copy must drop the item, got %d rows", got)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 2 {
		t.Errorf("total after removing shared = %d, want 2", total)
	}
}

func TestFeedItemsFollowSearchDeletion(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fd", "fd@example.com", "Fd")
	now := time.Now()

	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	for _, id := range []string{"d1", "d2"} {
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: id, Title: "x", Price: 1, Currency: "JPY", URL: "https://e/" + id,
		}, now); err != nil {
			t.Fatal(err)
		}
	}
	if feedItemCount(t, st) != 2 {
		t.Fatal("baseline wrong")
	}

	if _, err := st.DeleteSearches(ctx, u.ID, []int64{sid}); err != nil {
		t.Fatal(err)
	}
	if got := feedItemCount(t, st); got != 0 {
		t.Fatalf("deleting a search must cascade into feed_items, got %d rows", got)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 0 {
		t.Errorf("feed should be empty, total=%d", total)
	}
}

func TestFeedItemsFollowRepopulate(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fr", "fr@example.com", "Fr")
	now := time.Now()

	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "r1", Title: "x", Price: 1, Currency: "JPY", URL: "https://e/r1",
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RepopulateSearch(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if got := feedItemCount(t, st); got != 0 {
		t.Fatalf("repopulate must clear feed_items too, got %d", got)
	}
	if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "r1", Title: "x again", Price: 2, Currency: "JPY", URL: "https://e/r1",
	}, now); err != nil {
		t.Fatal(err)
	}
	if got := feedItemCount(t, st); got != 1 {
		t.Fatalf("re-fetched item should reappear once, got %d", got)
	}
}

func TestFeedItemsHiddenSyncsThroughTrigger(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-fh", "fh@example.com", "Fh")
	now := time.Now()

	var sids []int64
	for i := 0; i < 2; i++ {
		sid, _ := st.CreateSearch(ctx, Search{
			UserID: u.ID, Source: "mercari", Query: fmt.Sprintf("q%d", i),
			Interval: time.Minute, Enabled: true,
		})
		sids = append(sids, sid)
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: "dup", Title: "shared", Price: 1, Currency: "JPY", URL: "https://e/dup",
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := st.SetListingHidden(ctx, u.ID, "mercari", "dup", true); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 0 {
		t.Errorf("hidden item must leave the feed, total=%d", total)
	}
	if _, total, _ := st.HiddenListingsPage(ctx, u.ID, 50, 0); total != 1 {
		t.Errorf("hidden section should show it once, total=%d", total)
	}

	if _, err := st.SetListingHidden(ctx, u.ID, "mercari", "dup", false); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 1 {
		t.Errorf("unhidden item must return to the feed, total=%d", total)
	}
}

func TestFeedItemsBackfillOnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/backfill.db"

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT UNIQUE);
		CREATE TABLE searches (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, source TEXT,
		                       query TEXT, params TEXT DEFAULT '{}', interval_seconds INTEGER DEFAULT 300,
		                       enabled INTEGER DEFAULT 1, created_at INTEGER DEFAULT 0);
		CREATE TABLE listings (
		    id INTEGER PRIMARY KEY AUTOINCREMENT, search_id INTEGER NOT NULL,
		    source TEXT NOT NULL, external_id TEXT NOT NULL, title TEXT NOT NULL,
		    price REAL DEFAULT 0, currency TEXT DEFAULT '', url TEXT DEFAULT '',
		    image_url TEXT DEFAULT '', sale_type TEXT DEFAULT '', extra TEXT DEFAULT '{}',
		    first_seen INTEGER NOT NULL, notified INTEGER DEFAULT 0,
		    UNIQUE(search_id, external_id));
		INSERT INTO users (name, email) VALUES ('Old','old@example.com');
		INSERT INTO searches (id, user_id, source, query) VALUES (1, 1, 'mercari', 'a'), (2, 1, 'mercari', 'b');
		INSERT INTO listings (search_id, source, external_id, title, first_seen) VALUES
		    (1, 'mercari', 'x1', 'one', 1000),
		    (1, 'mercari', 'dup', 'shared', 1100),
		    (2, 'mercari', 'dup', 'shared', 1200),
		    (2, 'mercari', 'x2', 'two', 1300);
	`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrating an existing db must succeed: %v", err)
	}
	defer st.Close()

	if got := feedItemCount(t, st); got != 3 {
		t.Fatalf("backfill should produce one row per distinct item, got %d", got)
	}
	list, total, err := st.ListingsPage(ctx, 1, "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("feed after backfill: total=%d len=%d want 3", total, len(list))
	}
	seen := map[string]int{}
	for _, l := range list {
		seen[l.ExternalID]++
	}
	if seen["dup"] != 1 {
		t.Errorf("the shared item must appear exactly once, got %d", seen["dup"])
	}
}

func TestFeedItemsCatPathFollowsEnrichment(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-cp", "cp@example.com", "Cp")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	rec, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "e1", Title: "item", Price: 10, Currency: "JPY", URL: "https://e/1",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", ExcludeCategories: "3088",
	}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 1 {
		t.Fatal("item with no category should be visible")
	}

	if err := st.UpdateListingMarket(ctx, rec.ID, 20, "", map[string]string{
		"category": "3088", "categories": "1,3088",
	}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 0 {
		t.Error("after enrichment adds an excluded category the item must drop out of the feed")
	}
}

func TestNewListingIsExcludedImmediately(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-nx", "nx@example.com", "Nx")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", Exclude: "ONE PIECE", ExcludeCategories: "3088",
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "ok1", Title: "pikachu card", Price: 1, Currency: "JPY", URL: "https://e/1",
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "bad1", Title: "ONE PIECE luffy", Price: 1, Currency: "JPY", URL: "https://e/2",
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
		ExternalID: "bad2", Title: "fashion thing", Price: 1, Currency: "JPY", URL: "https://e/3",
		Extra: map[string]string{"category": "3088"},
	}, now); err != nil {
		t.Fatal(err)
	}

	list, total, err := st.ListingsPage(ctx, u.ID, "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 || list[0].ExternalID != "ok1" {
		ids := make([]string, 0, len(list))
		for _, l := range list {
			ids = append(ids, l.ExternalID)
		}
		t.Fatalf("a newly recorded excluded listing must never reach the feed: total=%d ids=%v", total, ids)
	}
}

func TestExclusionChangesTakeEffectBothWays(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-eb", "eb@example.com", "Eb")
	sid, _ := st.CreateSearch(ctx, Search{
		UserID: u.ID, Source: "mercari", Query: "q", Interval: time.Minute, Enabled: true,
	})
	now := time.Now()
	for _, tc := range []struct{ id, title string }{
		{"a", "pikachu"}, {"b", "ONE PIECE luffy"},
	} {
		if _, _, err := st.RecordListing(ctx, sid, "mercari", source.Listing{
			ExternalID: tc.id, Title: tc.title, Price: 1, Currency: "JPY", URL: "https://e/" + tc.id,
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 2 {
		t.Fatal("baseline should show both")
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{
		Source: "mercari", Exclude: "ONE PIECE",
	}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 1 {
		t.Error("adding an exclusion must hide the matching item")
	}

	if err := st.SetSourceExclusion(ctx, u.ID, SourceExclusion{Source: "mercari"}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListingsPage(ctx, u.ID, "", nil, 50, 0); total != 2 {
		t.Error("removing an exclusion must bring the item back")
	}
}
