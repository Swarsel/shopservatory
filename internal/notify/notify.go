package notify

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Swarsel/shopservatory/internal/store"
)

type Event struct {
	Search  store.Search
	Source  string
	Listing store.Listing
}

type Notifier interface {
	Kind() string

	Send(ctx context.Context, target store.NotificationTarget, ev Event) error
}

type Manager struct {
	notifiers map[string]Notifier
	log       *slog.Logger
}

func NewManager(log *slog.Logger, notifiers ...Notifier) *Manager {
	m := &Manager{notifiers: map[string]Notifier{}, log: log}
	for _, n := range notifiers {
		if n != nil {
			m.notifiers[n.Kind()] = n
		}
	}
	return m
}

func (m *Manager) Kinds() []string {
	out := make([]string, 0, len(m.notifiers))
	for k := range m.notifiers {
		out = append(out, k)
	}
	return out
}

func (m *Manager) Dispatch(ctx context.Context, targets []store.NotificationTarget, ev Event) {
	for _, t := range targets {
		n, ok := m.notifiers[t.Kind]
		if !ok {
			continue
		}
		if err := n.Send(ctx, t, ev); err != nil {
			m.log.Warn("notification delivery failed",
				"kind", t.Kind, "target", t.ID, "listing", ev.Listing.ExternalID, "err", err)
		}
	}
}

func formatPrice(price float64, currency string) string {
	if currency == "" {
		if price == 0 {
			return ""
		}
		return fmt.Sprintf("%.0f", price)
	}
	return fmt.Sprintf("%s %.0f", currency, price)
}
