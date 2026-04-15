package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fhak/pelagicsociety/internal/auth"
)

type loginPageData struct {
	pageData
	Next  string
	Error string
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, bounce to admin home.
	if u := s.auth.UserFromRequest(r); u != nil && u.Role == auth.RoleAdmin {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", loginPageData{
		pageData: s.pageFor(r, "Login — Pelagic Society", "/login"),
		Next:     safeNext(r.URL.Query().Get("next")),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad form submission.")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	user, err := s.auth.Authenticate(ctx, email, password)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			log.Printf("auth error: %v", err)
		}
		// Generic message — never reveal whether the email exists.
		writeFormResult(w, http.StatusUnauthorized, "error", "Invalid email or password.")
		return
	}
	if err := s.auth.SetSessionCookie(w, user); err != nil {
		log.Printf("auth cookie: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Login failed. Please try again.")
		return
	}

	redirect := next
	if redirect == "" {
		if user.Role == auth.RoleAdmin {
			redirect = "/admin"
		} else {
			redirect = "/"
		}
	}

	// HTMX request? Tell it to navigate. Plain form? Standard redirect.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirect)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.ClearSessionCookie(w)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// safeNext only allows relative paths starting with "/" — avoids open redirects.
func safeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") {
		return ""
	}
	return n
}
