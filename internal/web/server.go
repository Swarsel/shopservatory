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

	"github.com/Swarsel/shopservatory/internal/auth"
	"github.com/Swarsel/shopservatory/internal/fx"
	"github.com/Swarsel/shopservatory/internal/scheduler"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type Server struct {
	store       *store.Store
	registry    *source.Registry
	sched       *scheduler.Scheduler
	fx          *fx.Converter
	auth        *auth.Authenticator
	log         *slog.Logger
	tmpl        *template.Template
	loginTmpl   *template.Template
	images      *http.Client
	imagesProxy *http.Client

	searchInterval  time.Duration
	monitorInterval time.Duration
}

func New(st *store.Store, reg *source.Registry, sched *scheduler.Scheduler, conv *fx.Converter, authn *auth.Authenticator, searchInterval, monitorInterval time.Duration, imageProxyURL string, log *slog.Logger) *Server {
	if searchInterval <= 0 {
		searchInterval = 5 * time.Minute
	}
	if monitorInterval <= 0 {
		monitorInterval = time.Hour
	}
	return &Server{
		store:           st,
		registry:        reg,
		sched:           sched,
		fx:              conv,
		auth:            authn,
		log:             log,
		tmpl:            template.Must(template.New("page").Parse(pageTemplate)),
		loginTmpl:       template.Must(template.New("login").Parse(loginTemplate)),
		images:          imageClient(""),
		imagesProxy:     imageClient(imageProxyURL),
		searchInterval:  searchInterval,
		monitorInterval: monitorInterval,
	}
}

func imageClient(proxyURL string) *http.Client {
	tr := &http.Transport{}
	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}
}

var proxiedImageHosts = []string{"jmty.jp"}

func proxiedImageHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range proxiedImageHosts {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func (s *Server) searchDefault(ctx context.Context, userID int64) time.Duration {
	if st, err := s.store.UserSettings(ctx, userID); err == nil && st.SearchInterval > 0 {
		return st.SearchInterval
	}
	return s.searchInterval
}

func (s *Server) monitorDefault(ctx context.Context, userID int64) time.Duration {
	if st, err := s.store.UserSettings(ctx, userID); err == nil && st.MonitorInterval > 0 {
		return st.MonitorInterval
	}
	return s.monitorInterval
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /auth/oidc", s.handleOIDCStart)
	mux.HandleFunc("GET /auth/callback", s.handleOIDCCallback)

	b := s.auth.RequireSession
	mux.Handle("GET /{$}", b(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /img", b(http.HandlerFunc(s.handleImageProxy)))
	mux.Handle("GET /api/state", b(http.HandlerFunc(s.handleState)))
	mux.Handle("POST /searches", b(http.HandlerFunc(s.handleCreate)))
	mux.Handle("POST /searches/{id}/update", b(http.HandlerFunc(s.handleUpdate)))
	mux.Handle("POST /searches/{id}/delete", b(http.HandlerFunc(s.handleDelete)))
	mux.Handle("POST /searches/{id}/toggle", b(http.HandlerFunc(s.handleToggle)))
	mux.Handle("POST /searches/{id}/run", b(http.HandlerFunc(s.handleRun)))
	mux.Handle("POST /monitors", b(http.HandlerFunc(s.handleAddMonitor)))
	mux.Handle("POST /monitors/{id}/update", b(http.HandlerFunc(s.handleUpdateMonitor)))
	mux.Handle("POST /monitors/{id}/delete", b(http.HandlerFunc(s.handleDeleteMonitor)))
	mux.Handle("POST /monitors/{id}/run", b(http.HandlerFunc(s.handleRunMonitor)))
	mux.Handle("POST /settings", b(http.HandlerFunc(s.handleSettings)))

	a := s.auth.APIAuth
	mux.Handle("GET /api/v1/me", a(http.HandlerFunc(s.handleAPIMe)))
	mux.Handle("GET /api/v1/sources", a(http.HandlerFunc(s.handleAPISources)))
	mux.Handle("GET /api/v1/state", a(http.HandlerFunc(s.handleState)))
	mux.Handle("POST /api/v1/searches", a(http.HandlerFunc(s.handleAPICreate)))
	mux.Handle("POST /api/v1/searches/{id}/update", a(http.HandlerFunc(s.handleAPIUpdate)))
	mux.Handle("POST /api/v1/searches/{id}/delete", a(http.HandlerFunc(s.handleDelete)))
	mux.Handle("POST /api/v1/searches/{id}/toggle", a(http.HandlerFunc(s.handleToggle)))
	mux.Handle("POST /api/v1/searches/{id}/run", a(http.HandlerFunc(s.handleRun)))
	mux.Handle("POST /api/v1/settings", a(http.HandlerFunc(s.handleSettings)))
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
	Source      string  `json:"source"`
	SearchID    int64   `json:"searchId"`
	ExternalID  string  `json:"externalId"`
	Title       string  `json:"title"`
	Price       string  `json:"price"`
	PriceValue  float64 `json:"priceValue"`
	Currency    string  `json:"currency"`
	PriceApprox string  `json:"priceApprox"`
	URL         string  `json:"url"`
	ImageURL    string  `json:"imageUrl"`
	SaleType    string  `json:"saleType"`
	Seen        string  `json:"seen"`
}

type stateData struct {
	Searches []searchView  `json:"searches"`
	Listings []listingView `json:"listings"`
	Monitors []monitorView `json:"monitors"`
	Settings settingsView  `json:"settings"`
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	target := s.fx.Resolve(s.store.UserCurrency(r.Context(), auth.UserID(r.Context())))
	data := indexData{Now: time.Now(), Currency: target}
	for _, src := range s.registry.All() {
		data.Sources = append(data.Sources, sourceOption{ID: src.ID(), Name: src.DisplayName()})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		s.log.Error("render index", "err", err)
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	settings, _ := s.store.UserSettings(r.Context(), userID)
	target := s.fx.Resolve(settings.Currency)

	searches, err := s.searchViews(r.Context(), userID)
	if err != nil {
		s.fail(w, "list searches", err)
		return
	}
	limit := 100
	if r.URL.Query().Get("all") == "1" {
		limit = 1000000
	}
	listings, err := s.recentListingViews(r.Context(), userID, limit, target)
	if err != nil {
		s.fail(w, "recent listings", err)
		return
	}
	monitors, err := s.monitorViews(r.Context(), userID, target)
	if err != nil {
		s.fail(w, "list monitors", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := stateData{Searches: searches, Listings: listings, Monitors: monitors, Settings: settingsView{
		Currency:        settings.Currency,
		SearchInterval:  durStr(settings.SearchInterval),
		MonitorInterval: durStr(settings.MonitorInterval),
		TelegramChatID:  settings.TelegramChatID,
	}}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("web: encode state", "err", err)
	}
}

type settingsView struct {
	Currency        string `json:"currency"`
	SearchInterval  string `json:"searchInterval"`
	MonitorInterval string `json:"monitorInterval"`
	TelegramChatID  string `json:"telegramChatId"`
}

func durStr(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	parseDur := func(v string) time.Duration {
		v = strings.TrimSpace(v)
		if v == "" {
			return 0
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return 0
		}
		return d
	}
	currency := strings.ToUpper(strings.TrimSpace(r.FormValue("currency")))
	if err := s.store.UpdateUserSettings(r.Context(), userID, currency,
		parseDur(r.FormValue("search_interval")), parseDur(r.FormValue("monitor_interval"))); err != nil {
		s.fail(w, "update settings", err)
		return
	}
	if err := s.store.SetTelegramChatID(r.Context(), userID, r.FormValue("telegram_chat_id")); err != nil {
		s.fail(w, "update telegram", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

	client := s.images
	if u, err := url.Parse(target); err == nil {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
		if proxiedImageHost(u.Hostname()) {
			client = s.imagesProxy
		}
	}

	resp, err := client.Do(req)
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

func (s *Server) handleAPIMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"userId": auth.UserID(r.Context())})
}

func (s *Server) handleAPISources(w http.ResponseWriter, r *http.Request) {
	out := make([]sourceOption, 0)
	for _, src := range s.registry.All() {
		out = append(out, sourceOption{ID: src.ID(), Name: src.DisplayName()})
	}
	writeJSON(w, out)
}

func (s *Server) searchViews(ctx context.Context, userID int64) ([]searchView, error) {
	searches, err := s.store.ListSearchesForUser(ctx, userID)
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

func (s *Server) recentListingViews(ctx context.Context, userID int64, limit int, target string) ([]listingView, error) {
	listings, err := s.store.RecentListings(ctx, userID, limit)
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
			Source: l.Source, SearchID: l.SearchID, ExternalID: l.ExternalID,
			Title: l.Title, URL: l.URL, ImageURL: l.ImageURL, SaleType: l.SaleType,
			Price:       priceString(l.Price, l.Currency),
			PriceValue:  l.Price,
			Currency:    l.Currency,
			PriceApprox: s.fx.FormatFor(l.Price, l.Currency, target),
			Seen:        when.Format("2006-01-02 15:04"),
		})
	}
	return out, nil
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	base, err := s.parseCommonForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sources := s.validSources(r.PostForm["source"])
	if len(sources) == 0 {
		http.Error(w, errUnknownSource.Error(), http.StatusBadRequest)
		return
	}
	for _, src := range sources {
		se := base
		se.Source = src
		if _, err := s.store.CreateSearch(r.Context(), se); err != nil {
			s.fail(w, "create search", err)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) validSources(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, src := range raw {
		if seen[src] {
			continue
		}
		if _, ok := s.registry.Get(src); !ok {
			continue
		}
		seen[src] = true
		out = append(out, src)
	}
	return out
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, ok := s.ownedSearch(w, r, id)
	if !ok {
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
	if _, ok := s.ownedSearch(w, r, id); !ok {
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
	se, ok := s.ownedSearch(w, r, id)
	if !ok {
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
	if _, ok := s.ownedSearch(w, r, id); !ok {
		return
	}
	s.sched.RunNow(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ownedSearch(w http.ResponseWriter, r *http.Request, id int64) (store.Search, bool) {
	se, err := s.store.GetSearch(r.Context(), id)
	if err != nil || se.UserID != auth.UserID(r.Context()) {
		http.NotFound(w, r)
		return store.Search{}, false
	}
	return se, true
}

func (s *Server) handleAPICreate(w http.ResponseWriter, r *http.Request) {
	se, err := s.parseSearchJSON(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := s.store.CreateSearch(r.Context(), se)
	if err != nil {
		s.fail(w, "create search", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"id": id})
}

func (s *Server) handleAPIUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, ok := s.ownedSearch(w, r, id)
	if !ok {
		return
	}
	se, err := s.parseSearchJSON(r)
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
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) parseSearchJSON(r *http.Request) (store.Search, error) {
	var in struct {
		Source   string            `json:"source"`
		Query    string            `json:"query"`
		MinPrice *float64          `json:"minPrice"`
		MaxPrice *float64          `json:"maxPrice"`
		Interval string            `json:"interval"`
		Params   map[string]string `json:"params"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&in); err != nil {
		return store.Search{}, errBadForm
	}
	if _, ok := s.registry.Get(in.Source); !ok {
		return store.Search{}, errUnknownSource
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return store.Search{}, errQueryRequired
	}
	interval := s.searchDefault(r.Context(), auth.UserID(r.Context()))
	if in.Interval != "" {
		if d, err := time.ParseDuration(in.Interval); err == nil {
			interval = d
		}
	}
	if in.Params == nil {
		in.Params = map[string]string{}
	}
	return store.Search{
		UserID:   auth.UserID(r.Context()),
		Source:   in.Source,
		Query:    in.Query,
		Params:   in.Params,
		MinPrice: in.MinPrice,
		MaxPrice: in.MaxPrice,
		Interval: interval,
		Enabled:  true,
	}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) parseCommonForm(r *http.Request) (store.Search, error) {
	if err := r.ParseForm(); err != nil {
		return store.Search{}, errBadForm
	}
	query := strings.TrimSpace(r.FormValue("query"))
	if query == "" {
		return store.Search{}, errQueryRequired
	}
	interval := s.searchDefault(r.Context(), auth.UserID(r.Context()))
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	return store.Search{
		UserID:   auth.UserID(r.Context()),
		Query:    query,
		Params:   parseParams(r.FormValue("params")),
		MinPrice: parsePrice(r.FormValue("min_price")),
		MaxPrice: parsePrice(r.FormValue("max_price")),
		Interval: interval,
		Enabled:  true,
	}, nil
}

func (s *Server) parseSearchForm(r *http.Request) (store.Search, error) {
	se, err := s.parseCommonForm(r)
	if err != nil {
		return store.Search{}, err
	}
	src := r.FormValue("source")
	if _, ok := s.registry.Get(src); !ok {
		return store.Search{}, errUnknownSource
	}
	se.Source = src
	return se, nil
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
