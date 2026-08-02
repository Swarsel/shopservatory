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
	imagesJP    *http.Client

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

var jpImageHosts = []string{"yimg.jp", "yahoo.co.jp"}

func hostMatches(host string, suffixes []string) bool {
	host = strings.ToLower(host)
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func proxiedImageHost(host string) bool { return hostMatches(host, proxiedImageHosts) }

func jpImageHost(host string) bool { return hostMatches(host, jpImageHosts) }

func (s *Server) SetJPImageProxy(proxyURL string) {
	if proxyURL == "" {
		return
	}
	s.imagesJP = imageClient(proxyURL)
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
	mux.Handle("POST /searches/delete", b(http.HandlerFunc(s.handleBulkDelete)))
	mux.Handle("POST /searches/enable", b(http.HandlerFunc(s.handleBulkEnable)))
	mux.Handle("POST /searches/run", b(http.HandlerFunc(s.handleBulkRun)))
	mux.Handle("POST /searches/repopulate", b(http.HandlerFunc(s.handleBulkRepopulate)))
	mux.Handle("POST /searches/patch", b(http.HandlerFunc(s.handleBulkPatch)))
	mux.Handle("POST /searches/{id}/repopulate", b(http.HandlerFunc(s.handleRepopulate)))
	mux.Handle("POST /searches/{id}/toggle", b(http.HandlerFunc(s.handleToggle)))
	mux.Handle("POST /searches/{id}/run", b(http.HandlerFunc(s.handleRun)))
	mux.Handle("POST /image_search", b(http.HandlerFunc(s.handleImageSearch)))
	mux.Handle("POST /searches/image", b(http.HandlerFunc(s.handleCreateImageSearch)))
	mux.Handle("POST /listings/hide", b(http.HandlerFunc(s.handleHideListing)))
	mux.Handle("POST /monitors", b(http.HandlerFunc(s.handleAddMonitor)))
	mux.Handle("POST /monitors/{id}/update", b(http.HandlerFunc(s.handleUpdateMonitor)))
	mux.Handle("POST /monitors/{id}/delete", b(http.HandlerFunc(s.handleDeleteMonitor)))
	mux.Handle("POST /monitors/{id}/run", b(http.HandlerFunc(s.handleRunMonitor)))
	mux.Handle("POST /monitors/{id}/toggle", b(http.HandlerFunc(s.handleToggleMonitor)))
	mux.Handle("POST /monitors/{id}/archive", b(http.HandlerFunc(s.handleArchiveMonitor)))
	mux.Handle("POST /settings", b(http.HandlerFunc(s.handleSettings)))
	mux.Handle("POST /settings/source", b(http.HandlerFunc(s.handleSourceExclusions)))
	mux.Handle("POST /settings/source/pause", b(http.HandlerFunc(s.handleSourcePause)))
	mux.Handle("POST /password", b(http.HandlerFunc(s.handlePassword)))
	mux.Handle("POST /admin/users", b(s.requireAdmin(http.HandlerFunc(s.handleAdminCreateUser))))
	mux.Handle("POST /admin/users/{id}/update", b(s.requireAdmin(http.HandlerFunc(s.handleAdminUpdateUser))))
	mux.Handle("POST /admin/users/{id}/delete", b(s.requireAdmin(http.HandlerFunc(s.handleAdminDeleteUser))))

	a := s.auth.APIAuth
	mux.Handle("GET /api/v1/img", a(http.HandlerFunc(s.handleImageProxy)))
	mux.Handle("GET /api/v1/me", a(http.HandlerFunc(s.handleAPIMe)))
	mux.Handle("GET /api/v1/sources", a(http.HandlerFunc(s.handleAPISources)))
	mux.Handle("GET /api/v1/state", a(http.HandlerFunc(s.handleState)))
	mux.Handle("POST /api/v1/searches", a(http.HandlerFunc(s.handleAPICreate)))
	mux.Handle("POST /api/v1/searches/{id}/update", a(http.HandlerFunc(s.handleAPIUpdate)))
	mux.Handle("POST /api/v1/searches/{id}/delete", a(http.HandlerFunc(s.handleDelete)))
	mux.Handle("POST /api/v1/searches/{id}/toggle", a(http.HandlerFunc(s.handleToggle)))
	mux.Handle("POST /api/v1/searches/{id}/run", a(http.HandlerFunc(s.handleRun)))
	mux.Handle("POST /api/v1/settings", a(http.HandlerFunc(s.handleSettings)))
	mux.Handle("POST /api/v1/image_search", a(http.HandlerFunc(s.handleImageSearch)))
	mux.Handle("POST /api/v1/searches/image", a(http.HandlerFunc(s.handleCreateImageSearch)))
	return mux
}

func (s *Server) handleCreateImageSearch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		http.Error(w, "expected multipart form with an image", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 12<<20))
	if err != nil {
		http.Error(w, "could not read image", http.StatusBadRequest)
		return
	}
	img, err := source.NormalizeSearchImage(raw)
	if err != nil {
		http.Error(w, "could not process image: "+err.Error(), http.StatusBadRequest)
		return
	}

	srcID := r.FormValue("source")
	if srcID == "" {
		srcID = "mercari"
	}
	srcObj, ok := s.registry.Get(srcID)
	if !ok {
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}
	if _, ok := srcObj.(source.ImageSearcher); !ok {
		http.Error(w, "this source does not support image search", http.StatusBadRequest)
		return
	}

	query := strings.TrimSpace(r.FormValue("query"))
	if query == "" {
		query = "image: " + header.Filename
	}
	interval := s.searchDefault(r.Context(), auth.UserID(r.Context()))
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	var params map[string]string
	if d := strings.TrimSpace(r.FormValue("domain")); d != "" {
		params = map[string]string{"domain": d}
	}
	se := store.Search{
		UserID:   auth.UserID(r.Context()),
		Source:   srcID,
		Query:    query,
		Params:   params,
		MinPrice: parsePrice(r.FormValue("min_price")),
		MaxPrice: parsePrice(r.FormValue("max_price")),
		Interval: interval,
		Enabled:  true,
		Image:    img,
	}
	id, err := s.store.CreateSearch(r.Context(), se)
	if err != nil {
		s.fail(w, "create image search", err)
		return
	}
	s.sched.RunNow(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleImageSearch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		http.Error(w, "expected multipart form with an image", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	img, err := io.ReadAll(io.LimitReader(file, 12<<20))
	if err != nil {
		http.Error(w, "could not read image", http.StatusBadRequest)
		return
	}

	srcID := r.FormValue("source")
	if srcID == "" {
		srcID = "mercari"
	}
	src, ok := s.registry.Get(srcID)
	if !ok {
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}
	searcher, ok := src.(source.ImageSearcher)
	if !ok {
		http.Error(w, "this source does not support image search", http.StatusBadRequest)
		return
	}

	spec := source.SearchSpec{Params: map[string]string{"status": r.FormValue("status"), "domain": r.FormValue("domain")}}
	if v, err := strconv.ParseFloat(r.FormValue("min"), 64); err == nil && v > 0 {
		spec.MinPrice = &v
	}
	if v, err := strconv.ParseFloat(r.FormValue("max"), 64); err == nil && v > 0 {
		spec.MaxPrice = &v
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	listings, err := searcher.SearchByImage(ctx, img, spec)
	if err != nil {
		s.log.Warn("web: image search", "source", srcID, "err", err)
		msg := "image search failed: " + err.Error()
		if strings.Contains(err.Error(), "status 400") {
			msg = "the source could not process this image — try cropping to just the item"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}

	userID := auth.UserID(r.Context())
	target := s.fx.Resolve(s.store.UserCurrency(r.Context(), userID))
	views := make([]listingView, 0, len(listings))
	for _, l := range listings {
		views = append(views, listingView{
			Source: srcID, ExternalID: l.ExternalID, Title: l.Title,
			Price:       priceString(l.Price, l.Currency),
			PriceValue:  l.Price,
			Currency:    l.Currency,
			PriceApprox: s.fx.FormatFor(l.Price, l.Currency, target),
			URL:         l.URL, ImageURL: l.ImageURL, SaleType: l.SaleType,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]any{"listings": views}); err != nil {
		s.log.Error("web: encode image search", "err", err)
	}
}

type indexData struct {
	Sources  []sourceOption
	Currency string
	Now      time.Time
}

type sourceOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Images     bool   `json:"images"`
	Categories bool   `json:"categories"`
}

type searchView struct {
	ID                int64             `json:"id"`
	Source            string            `json:"source"`
	Query             string            `json:"query"`
	Interval          string            `json:"interval"`
	Enabled           bool              `json:"enabled"`
	LastRun           string            `json:"lastRun"`
	MinPrice          string            `json:"minPrice"`
	MaxPrice          string            `json:"maxPrice"`
	Params            map[string]string `json:"params"`
	Exclude           string            `json:"exclude"`
	ExcludeCategories string            `json:"excludeCategories"`
	IsImage           bool              `json:"isImage"`
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
	Ends        string  `json:"ends,omitempty"`
	Category    string  `json:"category,omitempty"`
	DoorzoURL   string  `json:"doorzoUrl,omitempty"`
	Seen        string  `json:"seen"`
}

type stateData struct {
	Searches      []searchView    `json:"searches"`
	Listings      []listingView   `json:"listings"`
	ListingsTotal int             `json:"listingsTotal"`
	ListingsPage  int             `json:"listingsPage"`
	ListingsPages int             `json:"listingsPages"`
	Hidden        []listingView   `json:"hidden,omitempty"`
	HiddenTotal   int             `json:"hiddenTotal"`
	HiddenPage    int             `json:"hiddenPage"`
	HiddenPages   int             `json:"hiddenPages"`
	Monitors      []monitorView   `json:"monitors"`
	Settings      settingsView    `json:"settings"`
	Me            meView          `json:"me"`
	Users         []adminUserView `json:"users"`
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	target := s.fx.Resolve(s.store.UserCurrency(r.Context(), auth.UserID(r.Context())))
	data := indexData{Now: time.Now(), Currency: target}
	for _, src := range s.registry.All() {
		_, images := src.(source.ImageSearcher)
		data.Sources = append(data.Sources, sourceOption{
			ID: src.ID(), Name: src.DisplayName(), Images: images,
			Categories: source.SupportsCategoryFilter(src.ID()),
		})
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
	const pageSize = 100
	filter := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	var filterSources []string
	if filter != "" {
		needle := strings.ToLower(filter)
		for _, src := range s.registry.All() {
			if strings.Contains(strings.ToLower(src.DisplayName()), needle) {
				filterSources = append(filterSources, src.ID())
			}
		}
	}
	listings, total, err := s.store.ListingsPage(r.Context(), userID, filter, filterSources, pageSize, (page-1)*pageSize)
	if err != nil {
		s.fail(w, "list listings", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
		if listings, total, err = s.store.ListingsPage(r.Context(), userID, filter, filterSources, pageSize, (page-1)*pageSize); err != nil {
			s.fail(w, "list listings", err)
			return
		}
	}
	var hiddenViews []listingView
	hiddenTotal, hiddenPage, hiddenPages := 0, 1, 1
	if r.URL.Query().Get("hidden") == "1" {
		hq := strings.TrimSpace(r.URL.Query().Get("hq"))
		var hSources []string
		if hq != "" {
			needle := strings.ToLower(hq)
			for _, src := range s.registry.All() {
				if strings.Contains(strings.ToLower(src.DisplayName()), needle) {
					hSources = append(hSources, src.ID())
				}
			}
		}
		hiddenPage = 1
		if v, err := strconv.Atoi(r.URL.Query().Get("hpage")); err == nil && v > 0 {
			hiddenPage = v
		}
		list, total, err := s.store.HiddenListingsPage(r.Context(), userID, hq, hSources, pageSize, (hiddenPage-1)*pageSize)
		if err == nil {
			hiddenPages = (total + pageSize - 1) / pageSize
			if hiddenPages < 1 {
				hiddenPages = 1
			}
			if hiddenPage > hiddenPages {
				hiddenPage = hiddenPages
				list, total, _ = s.store.HiddenListingsPage(r.Context(), userID, hq, hSources, pageSize, (hiddenPage-1)*pageSize)
			}
			hiddenViews = s.listingViews(list, target)
			hiddenTotal = total
		}
	} else if _, total, err := s.store.HiddenListingsPage(r.Context(), userID, "", nil, 1, 0); err == nil {
		hiddenTotal = total
	}

	monitors, err := s.monitorViews(r.Context(), userID, target)
	if err != nil {
		s.fail(w, "list monitors", err)
		return
	}

	var me meView
	if u, err := s.store.GetUser(r.Context(), userID); err == nil {
		me = meView{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin}
	}
	var users []adminUserView
	if me.IsAdmin {
		if list, err := s.store.ListUsers(r.Context()); err == nil {
			for _, u := range list {
				users = append(users, adminUserView{
					ID: u.ID, Name: u.Name, Email: u.Email, IsAdmin: u.IsAdmin,
					HasPassword: u.PasswordHash != "", OIDC: u.OIDCSubject != "",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := stateData{
		Searches: searches, Listings: s.listingViews(listings, target),
		ListingsTotal: total, ListingsPage: page, ListingsPages: pages,
		Hidden: hiddenViews, HiddenTotal: hiddenTotal,
		HiddenPage: hiddenPage, HiddenPages: hiddenPages,
		Monitors: monitors, Me: me, Users: users, Settings: settingsView{
			Currency:        settings.Currency,
			SearchInterval:  durStr(settings.SearchInterval),
			MonitorInterval: durStr(settings.MonitorInterval),
			TelegramChatID:  settings.TelegramChatID,
			SourceExclude:   s.sourceExclusionViews(r.Context(), userID),
		}}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("web: encode state", "err", err)
	}
}

type settingsView struct {
	Currency        string                   `json:"currency"`
	SearchInterval  string                   `json:"searchInterval"`
	MonitorInterval string                   `json:"monitorInterval"`
	TelegramChatID  string                   `json:"telegramChatId"`
	SourceExclude   map[string]exclusionView `json:"sourceExclude"`
}

type exclusionView struct {
	Exclude           string `json:"exclude"`
	ExcludeCategories string `json:"excludeCategories"`
	Paused            bool   `json:"paused"`
}

func (s *Server) sourceExclusionViews(ctx context.Context, userID int64) map[string]exclusionView {
	out := map[string]exclusionView{}
	byID, err := s.store.SourceExclusions(ctx, userID)
	if err != nil {
		s.log.Error("web: source exclusions", "err", err)
		return out
	}
	for id, e := range byID {
		out[id] = exclusionView{Exclude: e.Exclude, ExcludeCategories: e.ExcludeCategories, Paused: e.Paused}
	}
	return out
}

func (s *Server) handleSourceExclusions(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	srcID := r.FormValue("source")
	if _, ok := s.registry.Get(srcID); !ok {
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}
	err := s.store.SetSourceExclusion(r.Context(), auth.UserID(r.Context()), store.SourceExclusion{
		Source:            srcID,
		Exclude:           r.FormValue("exclude"),
		ExcludeCategories: r.FormValue("exclude_categories"),
	})
	if err != nil {
		s.fail(w, "save source exclusions", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSourcePause(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	srcID := r.FormValue("source")
	if _, ok := s.registry.Get(srcID); !ok {
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}
	paused := r.FormValue("paused") == "1"
	if err := s.store.SetSourcePaused(r.Context(), auth.UserID(r.Context()), srcID, paused); err != nil {
		s.fail(w, "pause source", err)
		return
	}
	s.log.Info("web: source pause toggled", "source", srcID, "paused", paused)
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleHideListing(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	src := strings.TrimSpace(r.FormValue("source"))
	externalID := strings.TrimSpace(r.FormValue("external_id"))
	if src == "" || externalID == "" {
		http.Error(w, "source and external_id are required", http.StatusBadRequest)
		return
	}
	hide := r.FormValue("hidden") != "0"
	n, err := s.store.SetListingHidden(r.Context(), auth.UserID(r.Context()), src, externalID, hide)
	if err != nil {
		s.fail(w, "hide listing", err)
		return
	}
	if n == 0 {
		http.Error(w, "no such listing", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	target, ok := safeImageURL(r.URL.Query().Get("u"))
	if !ok {
		http.Error(w, "bad image url", http.StatusBadRequest)
		return
	}
	resp, err := s.fetchImage(r, target)
	if err == nil && resp != nil && !imageResponseOK(resp) {
		resp.Body.Close()
		resp = nil
		if alt := imageURLWithoutTransform(target); alt != "" {
			resp, err = s.fetchImage(r, alt)
		}
	}
	if err != nil || resp == nil {
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !imageResponseOK(resp) {
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

func imageResponseOK(resp *http.Response) bool {
	return resp.StatusCode == http.StatusOK &&
		strings.HasPrefix(resp.Header.Get("Content-Type"), "image/")
}

func imageURLWithoutTransform(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.RawQuery == "" {
		return ""
	}
	u.RawQuery = ""
	return u.String()
}

func (s *Server) fetchImage(r *http.Request, target string) (*http.Response, error) {
	const attempts = 2
	var resp *http.Response
	var err error
	for i := 0; i < attempts; i++ {
		req, reqErr := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", imageProxyUA)
		req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")

		client := s.images
		if u, uErr := url.Parse(target); uErr == nil {
			req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
			switch {
			case s.imagesJP != nil && jpImageHost(u.Hostname()):
				client = s.imagesJP
			case proxiedImageHost(u.Hostname()):
				client = s.imagesProxy
			}
		}

		resp, err = client.Do(req)
		if err == nil && imageResponseOK(resp) {
			return resp, nil
		}
		if resp != nil && i < attempts-1 {
			resp.Body.Close()
			resp = nil
		}
		if r.Context().Err() != nil {
			break
		}
	}
	return resp, err
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
		_, images := src.(source.ImageSearcher)
		out = append(out, sourceOption{
			ID: src.ID(), Name: src.DisplayName(), Images: images,
			Categories: source.SupportsCategoryFilter(src.ID()),
		})
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
			Params:  orEmptyMap(se.Params),
			Exclude: se.Exclude, ExcludeCategories: se.ExcludeCategories, IsImage: len(se.Image) > 0,
		})
	}
	return out, nil
}

func (s *Server) listingViews(listings []store.Listing, target string) []listingView {
	out := make([]listingView, 0, len(listings))
	for _, l := range listings {

		when := l.FirstSeen
		if !l.ListedAt.IsZero() {
			when = l.ListedAt
		}
		out = append(out, listingView{
			Source: l.Source, SearchID: l.SearchID, ExternalID: l.ExternalID,
			Title: l.Title, URL: l.URL, ImageURL: l.ImageURL, SaleType: l.SaleType,
			Ends:        l.Extra["ends"],
			Category:    l.Extra["category"],
			DoorzoURL:   source.DoorzoURL(l.Source, l.URL, l.ExternalID),
			Price:       priceString(l.Price, l.Currency),
			PriceValue:  l.Price,
			Currency:    l.Currency,
			PriceApprox: s.fx.FormatFor(l.Price, l.Currency, target),
			Seen:        when.Format("2006-01-02 15:04"),
		})
	}
	return out
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

func (s *Server) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, v := range r.Form["id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		http.Error(w, "no search ids provided", http.StatusBadRequest)
		return
	}
	if _, err := s.store.DeleteSearches(r.Context(), auth.UserID(r.Context()), ids); err != nil {
		s.fail(w, "bulk delete searches", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bulkSearchIDs(r *http.Request) ([]int64, error) {
	if err := r.ParseForm(); err != nil {
		return nil, errBadForm
	}
	var ids []int64
	for _, v := range r.Form["id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Server) handleBulkEnable(w http.ResponseWriter, r *http.Request) {
	ids, err := bulkSearchIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "no search ids provided", http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("enabled") == "1"
	if _, err := s.store.SetSearchesEnabled(r.Context(), auth.UserID(r.Context()), ids, enabled); err != nil {
		s.fail(w, "bulk toggle searches", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBulkRun(w http.ResponseWriter, r *http.Request) {
	ids, err := bulkSearchIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "no search ids provided", http.StatusBadRequest)
		return
	}
	owned, err := s.store.OwnedSearchIDs(r.Context(), auth.UserID(r.Context()), ids)
	if err != nil {
		s.fail(w, "bulk run searches", err)
		return
	}
	for _, id := range owned {
		s.sched.RunNow(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBulkPatch(w http.ResponseWriter, r *http.Request) {
	ids, err := bulkSearchIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "no search ids provided", http.StatusBadRequest)
		return
	}

	var p store.SearchPatch
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			http.Error(w, "invalid interval (e.g. 30m, 1h, 6h)", http.StatusBadRequest)
			return
		}
		p.Interval = &d
	}
	if v := strings.TrimSpace(r.FormValue("min_price")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			http.Error(w, "invalid min price", http.StatusBadRequest)
			return
		}
		p.MinPrice = &f
	}
	if v := strings.TrimSpace(r.FormValue("max_price")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			http.Error(w, "invalid max price", http.StatusBadRequest)
			return
		}
		p.MaxPrice = &f
	}
	if r.Form.Has("exclude") {
		v := strings.TrimSpace(r.FormValue("exclude"))
		p.Exclude = &v
	}
	if r.Form.Has("exclude_categories") {
		v := strings.TrimSpace(r.FormValue("exclude_categories"))
		p.ExcludeCategories = &v
	}
	if r.Form.Has("params") {
		p.Params = parseParams(r.FormValue("params"))
		p.ReplaceParams = r.FormValue("params_mode") == "replace"
		if len(p.Params) == 0 && !p.ReplaceParams {
			http.Error(w, "no parameters given — use replace mode to clear them all", http.StatusBadRequest)
			return
		}
	}

	if p.Empty() {
		http.Error(w, "nothing to change — fill in at least one field", http.StatusBadRequest)
		return
	}

	n, err := s.store.PatchSearches(r.Context(), auth.UserID(r.Context()), ids, p)
	if err != nil {
		s.fail(w, "bulk edit searches", err)
		return
	}
	s.log.Info("web: bulk edited searches", "count", n)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRepopulate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownedSearch(w, r, id); !ok {
		return
	}
	removed, err := s.store.RepopulateSearch(r.Context(), id)
	if err != nil {
		s.fail(w, "repopulate search", err)
		return
	}
	s.log.Info("web: repopulating search", "search", id, "removed", removed)
	s.sched.RunNow(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBulkRepopulate(w http.ResponseWriter, r *http.Request) {
	ids, err := bulkSearchIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "no search ids provided", http.StatusBadRequest)
		return
	}
	owned, err := s.store.OwnedSearchIDs(r.Context(), auth.UserID(r.Context()), ids)
	if err != nil {
		s.fail(w, "repopulate searches", err)
		return
	}
	for _, id := range owned {
		if _, err := s.store.RepopulateSearch(r.Context(), id); err != nil {
			s.fail(w, "repopulate search", err)
			return
		}
		s.sched.RunNow(id)
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
		UserID:            auth.UserID(r.Context()),
		Query:             query,
		Params:            parseParams(r.FormValue("params")),
		MinPrice:          parsePrice(r.FormValue("min_price")),
		MaxPrice:          parsePrice(r.FormValue("max_price")),
		Interval:          interval,
		Enabled:           true,
		Exclude:           strings.TrimSpace(r.FormValue("exclude")),
		ExcludeCategories: strings.TrimSpace(r.FormValue("exclude_categories")),
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
	if price <= 0 {
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
