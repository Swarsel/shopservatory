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
