package store

import (
	"context"
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
