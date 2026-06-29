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
