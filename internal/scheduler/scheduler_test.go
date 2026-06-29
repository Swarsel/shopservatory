package scheduler

import (
	"strings"
	"testing"

	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

func TestMonitorNote(t *testing.T) {
	m := func(price float64, status string) store.MonitoredItem {
		return store.MonitoredItem{LastPrice: price, Status: status, Currency: "JPY"}
	}
	snap := func(price float64, status string) source.ItemSnapshot {
		return source.ItemSnapshot{Price: price, Status: status}
	}
	if got := monitorNote(m(100, "active"), snap(100, "sold")); !strings.Contains(got, "Sold") {
		t.Fatalf("sold: %q", got)
	}
	if got := monitorNote(m(100, "active"), snap(100, "removed")); !strings.Contains(got, "Removed") {
		t.Fatalf("removed: %q", got)
	}
	if got := monitorNote(m(100, "active"), snap(80, "active")); !strings.Contains(got, "dropped") {
		t.Fatalf("drop: %q", got)
	}
	if got := monitorNote(m(100, "active"), snap(120, "active")); !strings.Contains(got, "rose") {
		t.Fatalf("rise: %q", got)
	}
	if got := monitorNote(m(100, "active"), snap(100, "active")); got != "" {
		t.Fatalf("no change should be empty: %q", got)
	}
	if got := monitorNote(m(100, "sold"), snap(100, "sold")); got != "" {
		t.Fatalf("already sold should be empty: %q", got)
	}
}
