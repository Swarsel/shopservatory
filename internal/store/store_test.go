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
	if got, _ := st.RecentListings(ctx, alice.ID, 100); len(got) != 1 {
		t.Fatalf("alice feed should have 1, got %d", len(got))
	}
	if got, _ := st.RecentListings(ctx, bob.ID, 100); len(got) != 0 {
		t.Fatalf("bob feed must not see alice's listing, got %d", len(got))
	}
}
