package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/fhak/pelagicsociety/internal/auth"
)

func (s *Server) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	// Refresh the user from DB so any form edits we just made are visible.
	u := auth.UserFrom(r.Context())
	if fresh, err := s.auth.GetUserByID(r.Context(), u.ID); err == nil {
		u = fresh
	}
	p := s.pageFor(r, "Account — Pelagic Society", "/account")
	p.User = u
	s.render(w, "account.html", p)
}

func (s *Server) handleAccountName(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad submission.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) > 100 {
		writeFormResult(w, http.StatusBadRequest, "error", "Name too long.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.auth.UpdateName(ctx, u.ID, name); err != nil {
		log.Printf("update name: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save.")
		return
	}
	writeFormResult(w, http.StatusOK, "ok", "Name updated.")
}

func (s *Server) handleAccountEmail(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad submission.")
		return
	}
	newEmail := strings.TrimSpace(r.FormValue("email"))
	currentPw := r.FormValue("current_password")
	if _, err := mail.ParseAddress(newEmail); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Invalid email.")
		return
	}
	if currentPw == "" {
		writeFormResult(w, http.StatusBadRequest, "error", "Current password required.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.auth.VerifyPassword(ctx, u.ID, currentPw); err != nil {
		writeFormResult(w, http.StatusUnauthorized, "error", "Current password is incorrect.")
		return
	}
	if err := s.auth.UpdateEmail(ctx, u.ID, newEmail); err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeFormResult(w, http.StatusConflict, "error", "That email is already in use.")
			return
		}
		log.Printf("update email: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save.")
		return
	}
	writeFormResult(w, http.StatusOK, "ok", "Email updated.")
}

func (s *Server) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad submission.")
		return
	}
	current := r.FormValue("current_password")
	newPw := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if len(newPw) < 12 {
		writeFormResult(w, http.StatusBadRequest, "error", "New password must be at least 12 characters.")
		return
	}
	if newPw != confirm {
		writeFormResult(w, http.StatusBadRequest, "error", "New password and confirmation don't match.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.auth.VerifyPassword(ctx, u.ID, current); err != nil {
		writeFormResult(w, http.StatusUnauthorized, "error", "Current password is incorrect.")
		return
	}
	if err := s.auth.SetPassword(ctx, u.ID, newPw); err != nil {
		log.Printf("set password: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save.")
		return
	}
	writeFormResult(w, http.StatusOK, "ok", "Password updated.")
}

// --- admin settings (placeholder) ---

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	s.render(w, "admin_settings.html", s.pageFor(r, "Admin Settings — Pelagic Society", "/admin/settings"))
}
