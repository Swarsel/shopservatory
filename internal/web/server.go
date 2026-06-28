package web

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/fx"
	"github.com/Swarsel/shopservatory/internal/scheduler"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type Server struct {
	store    *store.Store
	registry *source.Registry
	sched    *scheduler.Scheduler
	fx       *fx.Converter
	log      *slog.Logger
	userID   int64
	tmpl     *template.Template
	images   *http.Client
}

func New(st *store.Store, reg *source.Registry, sched *scheduler.Scheduler, conv *fx.Converter, log *slog.Logger, userID int64) *Server {
	return &Server{
		store:    st,
		registry: reg,
		sched:    sched,
		fx:       conv,
		log:      log,
		userID:   userID,
		tmpl:     template.Must(template.New("page").Parse(pageTemplate)),
		images:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /img", s.handleImageProxy)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /searches", s.handleCreate)
	mux.HandleFunc("POST /searches/{id}/update", s.handleUpdate)
	mux.HandleFunc("POST /searches/{id}/delete", s.handleDelete)
	mux.HandleFunc("POST /searches/{id}/toggle", s.handleToggle)
	mux.HandleFunc("POST /searches/{id}/run", s.handleRun)
	return mux
}

type indexData struct {
	Sources  []sourceOption
	Currency string
	Now      time.Time
}

type sourceOption struct{ ID, Name string }

type searchView struct {
	ID       int64             `json:"id"`
	Source   string            `json:"source"`
	Query    string            `json:"query"`
	Interval string            `json:"interval"`
	Enabled  bool              `json:"enabled"`
	LastRun  string            `json:"lastRun"`
	MinPrice string            `json:"minPrice"`
	MaxPrice string            `json:"maxPrice"`
	Params   map[string]string `json:"params"`
}

type listingView struct {
	Source      string `json:"source"`
	SearchID    int64  `json:"searchId"`
	Title       string `json:"title"`
	Price       string `json:"price"`
	PriceApprox string `json:"priceApprox"`
	URL         string `json:"url"`
	ImageURL    string `json:"imageUrl"`
	Seen        string `json:"seen"`
}

type stateData struct {
	Searches []searchView  `json:"searches"`
	Listings []listingView `json:"listings"`
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	data := indexData{Now: time.Now(), Currency: s.fx.Target()}
	for _, src := range s.registry.All() {
		data.Sources = append(data.Sources, sourceOption{ID: src.ID(), Name: src.DisplayName()})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		s.log.Error("render index", "err", err)
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	searches, err := s.searchViews(r.Context())
	if err != nil {
		s.fail(w, "list searches", err)
		return
	}
	listings, err := s.recentListingViews(r.Context())
	if err != nil {
		s.fail(w, "recent listings", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(stateData{Searches: searches, Listings: listings}); err != nil {
		s.log.Error("web: encode state", "err", err)
	}
}

const imageProxyUA = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	target, ok := safeImageURL(r.URL.Query().Get("u"))
	if !ok {
		http.Error(w, "bad image url", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", imageProxyUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")

	resp, err := s.images.Do(req)
	if err != nil {
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		http.Error(w, "not an image", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 10<<20))
}

func safeImageURL(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return "", false
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", false
		}
	}
	return u.String(), true
}

func (s *Server) searchViews(ctx context.Context) ([]searchView, error) {
	searches, err := s.store.ListSearches(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]searchView, 0, len(searches))
	for _, se := range searches {
		lr := "never"
		if se.LastRunAt != nil {
			lr = se.LastRunAt.Format("2006-01-02 15:04")
		}
		out = append(out, searchView{
			ID: se.ID, Source: se.Source, Query: se.Query,
			Interval: se.Interval.String(), Enabled: se.Enabled, LastRun: lr,
			MinPrice: floatStr(se.MinPrice), MaxPrice: floatStr(se.MaxPrice),
			Params: orEmptyMap(se.Params),
		})
	}
	return out, nil
}

func (s *Server) recentListingViews(ctx context.Context) ([]listingView, error) {
	listings, err := s.store.RecentListings(ctx, 100)
	if err != nil {
		return nil, err
	}
	out := make([]listingView, 0, len(listings))
	for _, l := range listings {

		when := l.FirstSeen
		if !l.ListedAt.IsZero() {
			when = l.ListedAt
		}
		out = append(out, listingView{
			Source: l.Source, SearchID: l.SearchID, Title: l.Title, URL: l.URL, ImageURL: l.ImageURL,
			Price:       priceString(l.Price, l.Currency),
			PriceApprox: s.fx.Format(l.Price, l.Currency),
			Seen:        when.Format("2006-01-02 15:04"),
		})
	}
	return out, nil
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	se, err := s.parseSearchForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateSearch(r.Context(), se); err != nil {
		s.fail(w, "create search", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	existing, err := s.store.GetSearch(r.Context(), id)
	if err != nil {
		s.fail(w, "get search", err)
		return
	}
	se, err := s.parseSearchForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	se.ID = id
	se.UserID = existing.UserID
	se.Enabled = existing.Enabled
	if err := s.store.UpdateSearch(r.Context(), se); err != nil {
		s.fail(w, "update search", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSearch(r.Context(), id); err != nil {
		s.fail(w, "delete search", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	se, err := s.store.GetSearch(r.Context(), id)
	if err != nil {
		s.fail(w, "get search", err)
		return
	}
	if err := s.store.SetSearchEnabled(r.Context(), id, !se.Enabled); err != nil {
		s.fail(w, "toggle search", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.sched.RunNow(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) parseSearchForm(r *http.Request) (store.Search, error) {
	if err := r.ParseForm(); err != nil {
		return store.Search{}, errBadForm
	}
	src := r.FormValue("source")
	if _, ok := s.registry.Get(src); !ok {
		return store.Search{}, errUnknownSource
	}
	query := strings.TrimSpace(r.FormValue("query"))
	if query == "" {
		return store.Search{}, errQueryRequired
	}
	interval := 5 * time.Minute
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	return store.Search{
		UserID:   s.userID,
		Source:   src,
		Query:    query,
		Params:   parseParams(r.FormValue("params")),
		MinPrice: parsePrice(r.FormValue("min_price")),
		MaxPrice: parsePrice(r.FormValue("max_price")),
		Interval: interval,
		Enabled:  true,
	}, nil
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error("web: "+what, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

var (
	errBadForm       = errString("bad form")
	errUnknownSource = errString("unknown source")
	errQueryRequired = errString("query required")
)

type errString string

func (e errString) Error() string { return string(e) }

func parseParams(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func parsePrice(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func floatStr(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func priceString(price float64, currency string) string {
	if price == 0 && currency == "" {
		return ""
	}
	if currency == "" {
		return strconv.FormatFloat(price, 'f', -1, 64)
	}
	return currency + " " + strconv.FormatFloat(price, 'f', -1, 64)
}

func Serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("dashboard listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
