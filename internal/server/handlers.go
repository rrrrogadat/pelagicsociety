package server

import (
	"log"
	"net/http"
	"strings"
)

type pageData struct {
	Title       string
	Description string
	Path        string
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, "home.html", pageData{
		Title:       "Pelagic Society",
		Description: "Spearfishing, freediving, and open water adventures.",
		Path:        "/",
	})
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	s.render(w, "about.html", pageData{Title: "About — Pelagic Society", Path: "/about"})
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	s.render(w, "gallery.html", pageData{Title: "Gallery — Pelagic Society", Path: "/gallery"})
}

func (s *Server) handleShop(w http.ResponseWriter, r *http.Request) {
	s.render(w, "shop.html", pageData{Title: "Shop — Pelagic Society", Path: "/shop"})
}

func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) {
	s.render(w, "contact.html", pageData{Title: "Contact — Pelagic Society", Path: "/contact"})
}

func (s *Server) handleContactSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := strings.TrimSpace(r.FormValue("message"))
	kind := strings.TrimSpace(r.FormValue("kind"))

	if name == "" || email == "" || message == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<p class="text-red-400">Please fill out all fields.</p>`))
		return
	}

	// TODO: send email via Resend/Postmark and/or persist to SQLite.
	log.Printf("contact: kind=%q name=%q email=%q msg_len=%d", kind, name, email, len(message))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<p class="text-emerald-400">Thanks — message received. We'll be in touch.</p>`))
}

func (s *Server) handleWaitlist(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" || !strings.Contains(email, "@") {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<p class="text-red-400">Enter a valid email.</p>`))
		return
	}
	// TODO: persist to SQLite + forward to newsletter provider.
	log.Printf("waitlist signup: %s", email)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<p class="text-emerald-400">You're on the list.</p>`))
}
