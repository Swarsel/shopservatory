package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Swarsel/shopservatory/internal/notify"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type Scheduler struct {
	store    *store.Store
	registry *source.Registry
	notifier *notify.Manager
	log      *slog.Logger

	defaultInterval time.Duration
	tick            time.Duration
	maxConcurrent   int
}

type Options struct {
	DefaultInterval time.Duration

	Tick time.Duration

	MaxConcurrent int
}

func New(st *store.Store, reg *source.Registry, n *notify.Manager, log *slog.Logger, opts Options) *Scheduler {
	if opts.Tick <= 0 {
		opts.Tick = 30 * time.Second
	}
	if opts.DefaultInterval <= 0 {
		opts.DefaultInterval = 5 * time.Minute
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	return &Scheduler{
		store:           st,
		registry:        reg,
		notifier:        n,
		log:             log,
		defaultInterval: opts.DefaultInterval,
		tick:            opts.Tick,
		maxConcurrent:   opts.MaxConcurrent,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Info("scheduler started", "tick", s.tick, "default_interval", s.defaultInterval)
	t := time.NewTicker(s.tick)
	defer t.Stop()

	s.runDue(ctx)
	s.runDueMonitors(ctx)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler stopping")
			return ctx.Err()
		case <-t.C:
			s.runDue(ctx)
			s.runDueMonitors(ctx)
		}
	}
}

func (s *Scheduler) RunNow(searchID int64) {
	go func() {
		se, err := s.store.GetSearch(context.Background(), searchID)
		if err != nil {
			s.log.Error("scheduler: run-now: get search", "search", searchID, "err", err)
			return
		}
		s.log.Info("scheduler: manual run", "search", searchID, "source", se.Source)
		s.poll(context.Background(), se)
	}()
}

func (s *Scheduler) runDue(ctx context.Context) {
	searches, err := s.store.ListSearches(ctx, true)
	if err != nil {
		s.log.Error("scheduler: list searches", "err", err)
		return
	}

	now := time.Now()
	sem := make(chan struct{}, s.maxConcurrent)
	var wg sync.WaitGroup
	for _, se := range searches {
		if !s.due(se, now) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(se store.Search) {
			defer wg.Done()
			defer func() { <-sem }()
			s.poll(ctx, se)
		}(se)
	}
	wg.Wait()
}

func (s *Scheduler) RunMonitorNow(id int64) {
	go func() {
		m, err := s.store.GetMonitor(context.Background(), id)
		if err != nil {
			s.log.Error("scheduler: run-now: get monitor", "monitor", id, "err", err)
			return
		}
		s.pollMonitor(context.Background(), m)
	}()
}

func (s *Scheduler) runDueMonitors(ctx context.Context) {
	monitors, err := s.store.DueMonitors(ctx)
	if err != nil {
		s.log.Error("scheduler: list monitors", "err", err)
		return
	}
	now := time.Now()
	sem := make(chan struct{}, s.maxConcurrent)
	var wg sync.WaitGroup
	for _, m := range monitors {
		if !s.monitorDue(m, now) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(m store.MonitoredItem) {
			defer wg.Done()
			defer func() { <-sem }()
			s.pollMonitor(ctx, m)
		}(m)
	}
	wg.Wait()
}

func (s *Scheduler) monitorDue(m store.MonitoredItem, now time.Time) bool {
	if m.LastCheckedAt == nil {
		return true
	}
	interval := m.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	return now.Sub(*m.LastCheckedAt) >= interval
}

func (s *Scheduler) pollMonitor(ctx context.Context, m store.MonitoredItem) {
	src, ok := s.registry.Get(m.Source)
	if !ok {
		return
	}
	mon, ok := src.(source.ItemMonitor)
	if !ok {
		return
	}

	pollCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	snap, err := mon.Snapshot(pollCtx, m.URL)
	if err != nil {
		s.log.Warn("scheduler: monitor snapshot failed", "monitor", m.ID, "source", m.Source, "err", err)
		_ = s.store.TouchMonitorCheck(ctx, m.ID, time.Now())
		return
	}

	first := m.LastCheckedAt == nil
	note := monitorNote(m, snap)
	if err := s.store.RecordMonitorCheck(ctx, m.ID, snap, time.Now()); err != nil {
		s.log.Error("scheduler: record monitor check", "monitor", m.ID, "err", err)
		return
	}
	if first || note == "" {
		return
	}

	targets, err := s.store.ListTargets(ctx, m.UserID)
	if err != nil {
		s.log.Error("scheduler: list targets", "monitor", m.ID, "err", err)
		return
	}
	currency := m.Currency
	if snap.Currency != "" {
		currency = snap.Currency
	}
	image := m.ImageURL
	if snap.ImageURL != "" {
		image = snap.ImageURL
	}
	title := m.Title
	if snap.Title != "" {
		title = snap.Title
	}
	s.notifier.Dispatch(ctx, targets, notify.Event{
		Source: src.DisplayName(),
		Note:   note,
		Listing: store.Listing{
			Title: title, URL: m.URL, ImageURL: image, Price: snap.Price, Currency: currency,
		},
	})
}

func monitorNote(m store.MonitoredItem, snap source.ItemSnapshot) string {
	if m.Status == "active" && snap.Status == "sold" {
		return "🔴 Sold"
	}
	if m.Status == "active" && snap.Status == "removed" {
		return "⚪ Removed"
	}
	if m.Status == "active" && snap.Status == "active" && snap.Price > 0 && m.LastPrice > 0 && snap.Price != m.LastPrice {
		cur := m.Currency
		if snap.Currency != "" {
			cur = snap.Currency
		}
		arrow := "📉 Price dropped"
		if snap.Price > m.LastPrice {
			arrow = "📈 Price rose"
		}
		return fmt.Sprintf("%s: %s → %s", arrow, formatAmount(m.LastPrice, cur), formatAmount(snap.Price, cur))
	}
	return ""
}

func formatAmount(v float64, currency string) string {
	if currency == "" {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%s %.0f", currency, v)
}

func (s *Scheduler) due(se store.Search, now time.Time) bool {
	if se.LastRunAt == nil {
		return true
	}
	interval := se.Interval
	if interval <= 0 {
		interval = s.defaultInterval
	}
	return now.Sub(*se.LastRunAt) >= interval
}

func (s *Scheduler) poll(ctx context.Context, se store.Search) {
	firstRun := se.LastRunAt == nil

	src, ok := s.registry.Get(se.Source)
	if !ok {
		s.log.Warn("scheduler: no such source, skipping", "source", se.Source, "search", se.ID)
		return
	}

	defer func() {
		if err := s.store.TouchSearchRun(ctx, se.ID, time.Now()); err != nil {
			s.log.Error("scheduler: touch run", "search", se.ID, "err", err)
		}
	}()

	pollCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	listings, err := src.Search(pollCtx, se.Spec())
	if err != nil {
		s.log.Warn("scheduler: search failed", "source", se.Source, "search", se.ID, "err", err)
		return
	}

	var targets []store.NotificationTarget
	if !firstRun {
		if targets, err = s.store.ListTargets(ctx, se.UserID); err != nil {
			s.log.Error("scheduler: list targets", "search", se.ID, "err", err)
		}
	}

	seenAt := time.Now()
	newCount := 0
	for _, l := range listings {
		stored, isNew, err := s.store.RecordListing(ctx, se.ID, se.Source, l, seenAt)
		if err != nil {
			s.log.Error("scheduler: record listing", "search", se.ID, "err", err)
			continue
		}
		if !isNew {
			continue
		}
		newCount++
		if firstRun {
			continue
		}
		s.notifier.Dispatch(ctx, targets, notify.Event{
			Search:  se,
			Source:  src.DisplayName(),
			Listing: stored,
		})
		if err := s.store.MarkNotified(ctx, stored.ID); err != nil {
			s.log.Error("scheduler: mark notified", "listing", stored.ID, "err", err)
		}
	}

	switch {
	case firstRun:
		s.log.Info("scheduler: seeded search", "source", se.Source, "search", se.ID, "items", newCount)
	case newCount > 0:
		s.log.Info("scheduler: new items", "source", se.Source, "search", se.ID, "new", newCount)
	default:
		s.log.Debug("scheduler: no new items", "source", se.Source, "search", se.ID)
	}
}
