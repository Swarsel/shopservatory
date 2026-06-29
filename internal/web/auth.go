package web

import (
	"net/http"
)

type loginData struct {
	OIDCEnabled bool
	OIDCName    string
	Error       string
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	data := loginData{OIDCEnabled: s.auth.OIDCEnabled(), OIDCName: s.auth.OIDCName(), Error: errMsg}
	if err := s.loginTmpl.Execute(w, data); err != nil {
		s.log.Error("render login", "err", err)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("shopservatory_session"); err == nil {
		if _, ok := s.store.SessionUserID(r.Context(), c.Value); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	s.renderLogin(w, http.StatusOK, "")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, http.StatusBadRequest, "bad request")
		return
	}
	u, err := s.auth.Login(r.Context(), r.FormValue("email"), r.FormValue("password"))
	if err != nil {
		s.renderLogin(w, http.StatusUnauthorized, "Invalid email or password.")
		return
	}
	if err := s.auth.StartSession(r.Context(), w, u.ID); err != nil {
		s.fail(w, "start session", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.EndSession(r.Context(), w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OIDCEnabled() {
		http.NotFound(w, r)
		return
	}
	s.auth.OIDCStart(w, r)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OIDCEnabled() {
		http.NotFound(w, r)
		return
	}
	u, err := s.auth.OIDCCallback(r.Context(), w, r)
	if err != nil {
		s.log.Warn("oidc callback failed", "err", err)
		s.renderLogin(w, http.StatusUnauthorized, "Single sign-on failed. Please try again.")
		return
	}
	if err := s.auth.StartSession(r.Context(), w, u.ID); err != nil {
		s.fail(w, "start session", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

const loginTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in · shopservatory</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .login { width: 320px; max-width: 90vw; padding: 1.5rem; border: 1px solid #8884; border-radius: 10px; }
  h1 { font-size: 1.3rem; margin: 0 0 1rem; }
  label { display: block; font-size: .8rem; margin: .6rem 0 .2rem; }
  input { width: 100%; padding: .5rem; box-sizing: border-box; border: 1px solid #8886; border-radius: 6px; background: transparent; color: inherit; }
  button { width: 100%; padding: .55rem; margin-top: 1rem; cursor: pointer; border-radius: 6px; border: 1px solid #8886; font-size: .9rem; }
  .err { color: #c44; font-size: .85rem; margin: 0 0 .6rem; }
  .or { text-align: center; color: #8889; font-size: .75rem; margin: 1rem 0 .6rem; }
  .oidc { display: block; text-align: center; padding: .55rem; border: 1px solid #8886; border-radius: 6px; text-decoration: none; color: inherit; font-size: .9rem; }
</style>
</head>
<body>
  <div class="login">
    <h1>shopservatory</h1>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
    <form method="post" action="/login">
      <label for="email">Email</label>
      <input id="email" name="email" type="email" autocomplete="username" required autofocus>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Sign in</button>
    </form>
    {{if .OIDCEnabled}}
    <div class="or">or</div>
    <a class="oidc" href="/auth/oidc">Login with {{.OIDCName}}</a>
    {{end}}
  </div>
</body>
</html>`
