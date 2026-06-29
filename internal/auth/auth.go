package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"github.com/Swarsel/shopservatory/internal/store"
)

type ctxKey int

const userIDKey ctxKey = 0

const (
	sessionCookie = "shopservatory_session"
	oauthCookie   = "shopservatory_oauth"
)

var ErrInvalidLogin = errors.New("invalid email or password")

type Options struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	OIDCName      string
	BaseURL       string
	SessionTTL    time.Duration
	DefaultUserID int64
}

type Authenticator struct {
	store         *store.Store
	log           *slog.Logger
	sessionTTL    time.Duration
	secureCookies bool
	defaultUserID int64

	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	oidcName string
}

func New(ctx context.Context, st *store.Store, opts Options, log *slog.Logger) (*Authenticator, error) {
	a := &Authenticator{
		store:         st,
		log:           log,
		sessionTTL:    opts.SessionTTL,
		defaultUserID: opts.DefaultUserID,
		secureCookies: strings.HasPrefix(opts.BaseURL, "https://"),
		oidcName:      opts.OIDCName,
	}
	if a.sessionTTL <= 0 {
		a.sessionTTL = 30 * 24 * time.Hour
	}
	if opts.Issuer != "" && opts.ClientID != "" {
		provider, err := oidc.NewProvider(ctx, opts.Issuer)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery for %q: %w", opts.Issuer, err)
		}
		a.provider = provider
		a.verifier = provider.Verifier(&oidc.Config{ClientID: opts.ClientID})
		a.oauth = &oauth2.Config{
			ClientID:     opts.ClientID,
			ClientSecret: opts.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  strings.TrimRight(opts.BaseURL, "/") + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		}
	}
	return a, nil
}

func (a *Authenticator) OIDCEnabled() bool { return a.oauth != nil }

func (a *Authenticator) OIDCName() string {
	if a.oidcName != "" {
		return a.oidcName
	}
	return "SSO"
}

func (a *Authenticator) Login(ctx context.Context, email, password string) (store.User, error) {
	u, err := a.store.UserByEmail(ctx, strings.TrimSpace(strings.ToLower(email)))
	if err != nil || u.PasswordHash == "" {
		return store.User{}, ErrInvalidLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return store.User{}, ErrInvalidLogin
	}
	return u, nil
}

func (a *Authenticator) StartSession(ctx context.Context, w http.ResponseWriter, userID int64) error {
	token, err := a.store.CreateSession(ctx, userID, a.sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(a.sessionTTL),
		MaxAge:   int(a.sessionTTL / time.Second),
	})
	return nil
}

func (a *Authenticator) EndSession(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.store.DeleteSession(ctx, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (a *Authenticator) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if uid, ok := a.store.SessionUserID(r.Context(), c.Value); ok {
				a.serveAs(next, w, r, uid)
				return
			}
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (a *Authenticator) APIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.verifier == nil {
			a.serveAs(next, w, r, a.defaultUserID)
			return
		}
		raw := bearerToken(r)
		if raw == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		tok, err := a.verifier.Verify(r.Context(), raw)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		u, err := a.userFromToken(r.Context(), tok)
		if err != nil {
			a.log.Error("auth: resolve user", "err", err)
			http.Error(w, "user resolution failed", http.StatusInternalServerError)
			return
		}
		a.serveAs(next, w, r, u.ID)
	})
}

func (a *Authenticator) OIDCStart(w http.ResponseWriter, r *http.Request) {
	state := randToken()
	verifier := oauth2.GenerateVerifier()
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state + "|" + verifier, Path: "/",
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	url := a.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (a *Authenticator) OIDCCallback(ctx context.Context, w http.ResponseWriter, r *http.Request) (store.User, error) {
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		return store.User{}, errors.New("missing oauth state")
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode})

	state, verifier, ok := strings.Cut(c.Value, "|")
	if !ok || state == "" || r.URL.Query().Get("state") != state {
		return store.User{}, errors.New("oauth state mismatch")
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		return store.User{}, errors.New("missing authorization code")
	}
	tok, err := a.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return store.User{}, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return store.User{}, errors.New("no id_token in response")
	}
	idTok, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return store.User{}, fmt.Errorf("verify id_token: %w", err)
	}
	return a.userFromToken(ctx, idTok)
}

func (a *Authenticator) userFromToken(ctx context.Context, tok *oidc.IDToken) (store.User, error) {
	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = tok.Claims(&claims)
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	if name == "" {
		name = claims.Email
	}
	return a.store.UserFromIdentity(ctx, tok.Subject, claims.Email, name)
}

func (a *Authenticator) serveAs(next http.Handler, w http.ResponseWriter, r *http.Request, userID int64) {
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
}

func UserID(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDKey).(int64); ok {
		return v
	}
	return 0
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func randToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
