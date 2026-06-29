package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/Swarsel/shopservatory/internal/store"
)

type ctxKey int

const userIDKey ctxKey = 0

type Authenticator struct {
	store           *store.Store
	verifier        *oidc.IDTokenVerifier
	forwardedHeader string
	defaultUserID   int64
	log             *slog.Logger
}

func New(ctx context.Context, st *store.Store, issuer, clientID, forwardedHeader string, defaultUserID int64, log *slog.Logger) (*Authenticator, error) {
	a := &Authenticator{
		store:           st,
		forwardedHeader: forwardedHeader,
		defaultUserID:   defaultUserID,
		log:             log,
	}
	if issuer != "" {
		provider, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery for %q: %w", issuer, err)
		}
		a.verifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	}
	return a, nil
}

func (a *Authenticator) OIDCEnabled() bool { return a.verifier != nil }

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
		u, err := a.store.UserFromIdentity(r.Context(), tok.Subject, claims.Email, name)
		if err != nil {
			a.log.Error("auth: resolve user", "err", err)
			http.Error(w, "user resolution failed", http.StatusInternalServerError)
			return
		}
		a.serveAs(next, w, r, u.ID)
	})
}

func (a *Authenticator) BrowserAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := r.Header.Get(a.forwardedHeader)
		if email == "" {
			a.serveAs(next, w, r, a.defaultUserID)
			return
		}
		name := r.Header.Get("X-Forwarded-Preferred-Username")
		u, err := a.store.UserFromIdentity(r.Context(), "", email, name)
		if err != nil {
			a.log.Error("auth: resolve browser user", "err", err)
			http.Error(w, "user resolution failed", http.StatusInternalServerError)
			return
		}
		a.serveAs(next, w, r, u.ID)
	})
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

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
