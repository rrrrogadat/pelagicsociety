package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/fhak/pelagicsociety/internal/auth"
	mailer "github.com/fhak/pelagicsociety/internal/mail"
)

type pageData struct {
	Title       string
	Description string
	Path        string
	User        *auth.User
}

// pageFor builds the base page data for a request, pulling the current user
// from the session cookie if present. Used by handlers so nav etc. can render
// auth state on public pages.
func (s *Server) pageFor(r *http.Request, title, path string) pageData {
	return pageData{
		Title: title,
		Path:  path,
		User:  s.auth.UserFromRequest(r),
	}
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "Pelagic Society", "/")
	p.Description = "Spearfishing, freediving, and open water adventures."
	s.render(w, "home.html", p)
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	s.render(w, "about.html", s.pageFor(r, "About — Pelagic Society", "/about"))
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	s.render(w, "gallery.html", s.pageFor(r, "Gallery — Pelagic Society", "/gallery"))
}

func (s *Server) handleShop(w http.ResponseWriter, r *http.Request) {
	s.render(w, "shop.html", s.pageFor(r, "Shop — Pelagic Society", "/shop"))
}

func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) {
	s.render(w, "contact.html", s.pageFor(r, "Contact — Pelagic Society", "/contact"))
}

func validateEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return "", errors.New("invalid email")
	}
	return addr.Address, nil
}

// writeFormResult renders a small HTMX-friendly fragment.
func writeFormResult(w http.ResponseWriter, status int, tone, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	cls := "text-emerald-400"
	if tone == "error" {
		cls = "text-red-400"
	}
	fmt.Fprintf(w, `<p class="%s">%s</p>`, cls, msg)
}

func (s *Server) handleContactSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad form submission.")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	emailRaw := r.FormValue("email")
	message := strings.TrimSpace(r.FormValue("message"))
	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = "other"
	}

	email, err := validateEmail(emailRaw)
	if err != nil || name == "" || message == "" {
		writeFormResult(w, http.StatusBadRequest, "error", "Please fill out all fields with a valid email.")
		return
	}
	if len(message) > 10000 || len(name) > 200 {
		writeFormResult(w, http.StatusBadRequest, "error", "Message too long.")
		return
	}

	ip := clientIP(r)
	ua := r.UserAgent()

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	_, dbErr := s.db.ExecContext(ctx,
		`INSERT INTO contact_submissions(kind, name, email, message, ip, user_agent) VALUES(?,?,?,?,?,?)`,
		kind, name, email, message, ip, ua,
	)
	if dbErr != nil {
		log.Printf("contact db insert: %v", dbErr)
		writeFormResult(w, http.StatusInternalServerError, "error", "Something went wrong. Please try again.")
		return
	}

	// Send notification email. Failure here doesn't fail the submission —
	// it's already persisted.
	if s.mailer != nil && s.cfg.ContactToAddr != "" {
		subject := fmt.Sprintf("[%s] New contact from %s", strings.ToUpper(kind), name)
		text := fmt.Sprintf("Kind:    %s\nName:    %s\nEmail:   %s\nIP:      %s\nUA:      %s\n\n%s\n",
			kind, name, email, ip, ua, message)
		err := s.mailer.Send(ctx, mailer.Message{
			To:      []string{s.cfg.ContactToAddr},
			Subject: subject,
			Text:    text,
			ReplyTo: email,
		})
		if err != nil {
			log.Printf("contact email send: %v", err)
		}
	}

	log.Printf("contact: kind=%q email=%q ip=%q", kind, email, ip)
	writeFormResult(w, http.StatusOK, "ok", "Thanks — message received. We'll be in touch.")
}

func (s *Server) handleWaitlist(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad submission.")
		return
	}

	email, err := validateEmail(r.FormValue("email"))
	if err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Enter a valid email.")
		return
	}

	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		source = r.Header.Get("Referer")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// ON CONFLICT DO NOTHING — idempotent signups, don't reveal duplicate status.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO waitlist(email, source, ip, user_agent) VALUES(?,?,?,?)
		 ON CONFLICT(email) DO NOTHING`,
		email, source, clientIP(r), r.UserAgent(),
	)
	if err != nil {
		log.Printf("waitlist insert: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Something went wrong. Please try again.")
		return
	}

	log.Printf("waitlist: email=%q source=%q", email, source)
	writeFormResult(w, http.StatusOK, "ok", "You're on the list.")
}
