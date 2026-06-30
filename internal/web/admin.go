package web

import (
	"net/http"
	"strings"

	"github.com/Swarsel/shopservatory/internal/auth"
)

type meView struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"isAdmin"`
}

type adminUserView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	IsAdmin     bool   `json:"isAdmin"`
	HasPassword bool   `json:"hasPassword"`
	OIDC        bool   `json:"oidc"`
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.store.IsAdmin(r.Context(), auth.UserID(r.Context())) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func formBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = email
	}
	hash := ""
	if pw := r.FormValue("password"); strings.TrimSpace(pw) != "" {
		h, err := auth.HashPassword(pw)
		if err != nil {
			s.fail(w, "hash password", err)
			return
		}
		hash = h
	}
	if _, err := s.store.CreateUser(r.Context(), name, email, hash, formBool(r.FormValue("admin"))); err != nil {
		s.fail(w, "create user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	target, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = email
	}
	admin := formBool(r.FormValue("admin"))
	if target.IsAdmin && !admin {
		if n, _ := s.store.CountAdmins(r.Context()); n <= 1 {
			http.Error(w, "cannot remove the last admin", http.StatusBadRequest)
			return
		}
	}
	if err := s.store.UpdateUser(r.Context(), id, name, email, admin); err != nil {
		s.fail(w, "update user", err)
		return
	}
	if pw := r.FormValue("password"); strings.TrimSpace(pw) != "" {
		h, herr := auth.HashPassword(pw)
		if herr != nil {
			s.fail(w, "hash password", herr)
			return
		}
		if err := s.store.SetUserPassword(r.Context(), id, h); err != nil {
			s.fail(w, "set password", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == auth.UserID(r.Context()) {
		http.Error(w, "you cannot delete your own account", http.StatusBadRequest)
		return
	}
	target, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if target.IsAdmin {
		if n, _ := s.store.CountAdmins(r.Context()); n <= 1 {
			http.Error(w, "cannot delete the last admin", http.StatusBadRequest)
			return
		}
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.fail(w, "delete user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	u, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		s.fail(w, "load user", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, errBadForm.Error(), http.StatusBadRequest)
		return
	}
	newPw := r.FormValue("new_password")
	if len(newPw) < 8 {
		http.Error(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if u.PasswordHash != "" {
		if _, err := s.auth.Login(r.Context(), u.Email, r.FormValue("current_password")); err != nil {
			http.Error(w, "current password is incorrect", http.StatusUnauthorized)
			return
		}
	}
	h, err := auth.HashPassword(newPw)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}
	if err := s.store.SetUserPassword(r.Context(), userID, h); err != nil {
		s.fail(w, "set password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
