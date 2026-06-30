package web

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/auth"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type monitorView struct {
	ID          int64            `json:"id"`
	Source      string           `json:"source"`
	Title       string           `json:"title"`
	URL         string           `json:"url"`
	ImageURL    string           `json:"imageUrl"`
	Price       string           `json:"price"`
	PriceApprox string           `json:"priceApprox"`
	Status      string           `json:"status"`
	SaleType    string           `json:"saleType"`
	Interval    string           `json:"interval"`
	LastChecked string           `json:"lastChecked"`
	History     []pricePointView `json:"history"`
}

type pricePointView struct {
	Price  float64 `json:"price"`
	Status string  `json:"status"`
	At     string  `json:"at"`
}

func (s *Server) monitorViews(ctx context.Context, userID int64, target string) ([]monitorView, error) {
	monitors, err := s.store.ListMonitors(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]monitorView, 0, len(monitors))
	for _, m := range monitors {
		history, err := s.store.PriceHistory(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		points := make([]pricePointView, 0, len(history))
		for _, p := range history {
			points = append(points, pricePointView{Price: p.Price, Status: p.Status, At: p.ObservedAt.Format("2006-01-02 15:04")})
		}
		checked := "never"
		if m.LastCheckedAt != nil {
			checked = m.LastCheckedAt.Format("2006-01-02 15:04")
		}
		out = append(out, monitorView{
			ID: m.ID, Source: m.Source, Title: m.Title, URL: m.URL, ImageURL: m.ImageURL,
			Price:       priceString(m.LastPrice, m.Currency),
			PriceApprox: s.fx.FormatFor(m.LastPrice, m.Currency, target),
			Status:      m.Status, SaleType: m.SaleType, Interval: m.Interval.String(), LastChecked: checked, History: points,
		})
	}
	return out, nil
}

func (s *Server) handleAddMonitor(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	rawURL := strings.TrimSpace(r.FormValue("url"))
	if rawURL == "" || !strings.HasPrefix(rawURL, "http") {
		http.Error(w, "a valid item url is required", http.StatusBadRequest)
		return
	}
	src := r.FormValue("source")
	if src == "" {
		src = detectMonitorSource(s.registry, rawURL)
	}
	srcObj, ok := s.registry.Get(src)
	if !ok {
		http.Error(w, "could not determine a supported source for that url", http.StatusBadRequest)
		return
	}
	if _, ok := srcObj.(source.ItemMonitor); !ok {
		http.Error(w, "this source does not support price monitoring", http.StatusBadRequest)
		return
	}

	interval := s.monitorDefault(r.Context(), auth.UserID(r.Context()))
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	price, _ := strconv.ParseFloat(r.FormValue("price"), 64)
	m := store.MonitoredItem{
		UserID:     auth.UserID(r.Context()),
		Source:     src,
		ExternalID: r.FormValue("external_id"),
		URL:        rawURL,
		Title:      r.FormValue("title"),
		ImageURL:   r.FormValue("image_url"),
		Currency:   r.FormValue("currency"),
		SaleType:   r.FormValue("sale_type"),
		LastPrice:  price,
		Status:     "active",
		Interval:   interval,
		Enabled:    true,
	}

	if mon, ok := srcObj.(source.ItemMonitor); ok && (m.Title == "" || r.FormValue("price") == "") {
		if snap, err := mon.Snapshot(r.Context(), rawURL); err == nil {
			if m.Title == "" {
				m.Title = snap.Title
			}
			if m.ImageURL == "" {
				m.ImageURL = snap.ImageURL
			}
			if m.Currency == "" {
				m.Currency = snap.Currency
			}
			if m.SaleType == "" {
				m.SaleType = snap.SaleType
			}
			if r.FormValue("price") == "" {
				m.LastPrice = snap.Price
			}
			if snap.Status != "" {
				m.Status = snap.Status
			}
		} else if m.Title == "" {
			http.Error(w, "could not fetch that item (it may be unsupported or removed)", http.StatusBadGateway)
			return
		}
	}
	if m.ExternalID == "" {
		if u, err := url.Parse(rawURL); err == nil {
			m.ExternalID = path.Base(u.Path)
		}
	}

	if _, err := s.store.AddMonitor(r.Context(), m); err != nil {
		s.fail(w, "add monitor", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownedMonitor(w, r, id); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	d, err := time.ParseDuration(strings.TrimSpace(r.FormValue("interval")))
	if err != nil || d <= 0 {
		http.Error(w, "invalid interval (e.g. 30m, 1h, 6h)", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateMonitorInterval(r.Context(), id, d); err != nil {
		s.fail(w, "update monitor", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownedMonitor(w, r, id); !ok {
		return
	}
	if err := s.store.DeleteMonitor(r.Context(), id); err != nil {
		s.fail(w, "delete monitor", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownedMonitor(w, r, id); !ok {
		return
	}
	s.sched.RunMonitorNow(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ownedMonitor(w http.ResponseWriter, r *http.Request, id int64) (store.MonitoredItem, bool) {
	m, err := s.store.GetMonitor(r.Context(), id)
	if err != nil || m.UserID != auth.UserID(r.Context()) {
		http.NotFound(w, r)
		return store.MonitoredItem{}, false
	}
	return m, true
}

func detectMonitorSource(reg *source.Registry, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	hostMatch := map[string]string{
		"mercari":       "mercari",
		"fril.jp":       "rakuma",
		"kleinanzeigen": "kleinanzeigen",
		"shpock":        "shpock",
		"willhaben":     "willhaben",
		"magi.camp":     "magi",
		"jmty.jp":       "jmty",
		"suruga-ya":     "surugaya",
		"snkrdunk":      "snkrdunk",
		"vinted":        "vinted",
		"bazar.at":      "bazar",
		"craigslist":    "craigslist",
		"zenmarket":     "yahooauctions",
		"ebay":          "ebay",
	}
	for needle, id := range hostMatch {
		if strings.Contains(host, needle) {
			if _, ok := reg.Get(id); ok {
				return id
			}
		}
	}
	return ""
}
