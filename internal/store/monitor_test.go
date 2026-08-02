package store

import (
	"context"
	"testing"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
)

func TestMonitorFlow(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir()+"/m.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.EnsureDefaultUser(ctx, "a", "a@x")
	if err != nil {
		t.Fatal(err)
	}

	m := MonitoredItem{UserID: u.ID, Source: "rakuma", ExternalID: "abc", URL: "https://item.fril.jp/abc",
		Title: "Thing", Currency: "JPY", LastPrice: 10000, Status: "active", Interval: time.Hour, Enabled: true}
	id, err := st.AddMonitor(ctx, m)
	if err != nil || id == 0 {
		t.Fatalf("add: id=%d err=%v", id, err)
	}
	if id2, err := st.AddMonitor(ctx, m); err != nil || id2 != id {
		t.Fatalf("dedup should return same id: %d vs %d", id2, id)
	}

	if err := st.RecordMonitorCheck(ctx, id, source.ItemSnapshot{Price: 9000, Status: "active", Currency: "JPY"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMonitorCheck(ctx, id, source.ItemSnapshot{Price: 9000, Status: "sold"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetMonitor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastPrice != 9000 || got.Status != "sold" {
		t.Fatalf("monitor state: %+v", got)
	}
	if hist, err := st.PriceHistory(ctx, id); err != nil || len(hist) != 3 {
		t.Fatalf("want 3 history points, got %d (%v)", len(hist), err)
	}
	if due, err := st.DueMonitors(ctx); err != nil || len(due) != 0 {
		t.Fatalf("sold monitor must not be due, got %d (%v)", len(due), err)
	}
	if err := st.DeleteMonitor(ctx, id); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListMonitors(ctx, u.ID); len(list) != 0 {
		t.Fatalf("after delete: %d", len(list))
	}
}

func TestExtendingAuctionClearsStaleEndTime(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-ext", "ext@example.com", "Ext")

	ends := time.Now().Add(2 * time.Minute).Truncate(time.Second)
	id, err := st.AddMonitor(ctx, MonitoredItem{
		UserID: u.ID, Source: "mercari", ExternalID: "m1",
		URL: "https://jp.mercari.com/item/m1", Title: "auction",
		SaleType: "auction", Interval: time.Hour, Enabled: true,
		Status: "active", EndsAt: &ends,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetMonitor(ctx, id); m.EndsAt == nil || !m.EndsAt.Equal(ends) {
		t.Fatalf("precondition: end time should be stored, got %v", m.EndsAt)
	}

	if err := st.RecordMonitorCheck(ctx, id, source.ItemSnapshot{
		Price: 81000, Currency: "JPY", Status: "active", SaleType: "auction",
		Extending: true,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m, _ := st.GetMonitor(ctx, id)
	if m.EndsAt != nil {
		t.Errorf("an extending auction must drop its stale end time, got %v", m.EndsAt)
	}
	if m.Status != "active" {
		t.Errorf("status = %q, want active", m.Status)
	}
	if m.LastPrice != 81000 {
		t.Errorf("price should still update, got %v", m.LastPrice)
	}
}

func TestNewEndTimeReplacesTheOldOne(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-ext2", "ext2@example.com", "Ext2")

	old := time.Now().Add(time.Minute).Truncate(time.Second)
	id, _ := st.AddMonitor(ctx, MonitoredItem{
		UserID: u.ID, Source: "mercari", ExternalID: "m2",
		URL: "https://jp.mercari.com/item/m2", SaleType: "auction",
		Interval: time.Hour, Enabled: true, Status: "active", EndsAt: &old,
	})

	extended := time.Now().Add(11 * time.Minute).Truncate(time.Second)
	if err := st.RecordMonitorCheck(ctx, id, source.ItemSnapshot{
		Price: 1, Currency: "JPY", Status: "active", SaleType: "auction", EndsAt: extended,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m, _ := st.GetMonitor(ctx, id)
	if m.EndsAt == nil || !m.EndsAt.Equal(extended) {
		t.Fatalf("a freshly published end time must win, got %v want %v", m.EndsAt, extended)
	}
}

func TestCheckWithoutEndInfoKeepsExistingEndTime(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.UserFromIdentity(ctx, "sub-ext3", "ext3@example.com", "Ext3")

	ends := time.Now().Add(time.Hour).Truncate(time.Second)
	id, _ := st.AddMonitor(ctx, MonitoredItem{
		UserID: u.ID, Source: "ebay", ExternalID: "e1",
		URL: "https://ebay.com/itm/e1", SaleType: "auction",
		Interval: time.Hour, Enabled: true, Status: "active", EndsAt: &ends,
	})

	if err := st.RecordMonitorCheck(ctx, id, source.ItemSnapshot{
		Price: 5, Currency: "USD", Status: "active", SaleType: "auction",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m, _ := st.GetMonitor(ctx, id)
	if m.EndsAt == nil || !m.EndsAt.Equal(ends) {
		t.Fatalf("a plain check must not wipe a known end time, got %v", m.EndsAt)
	}
}
