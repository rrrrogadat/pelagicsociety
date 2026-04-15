package server

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/fhak/pelagicsociety/internal/auth"
	"github.com/fhak/pelagicsociety/internal/content"
	"github.com/fhak/pelagicsociety/internal/gallery"
	"github.com/fhak/pelagicsociety/internal/mail"
	"github.com/fhak/pelagicsociety/internal/media"
	"github.com/fhak/pelagicsociety/web"
)

type Config struct {
	DB            *sql.DB
	Mailer        *mail.Mailer
	Auth          *auth.Auth
	Content       *content.Service
	Gallery       *gallery.Repo
	Media         *media.Store
	ContactToAddr string // where contact form submissions are delivered
}

type Server struct {
	mux       *http.ServeMux
	pages     map[string]*template.Template
	fragments *template.Template
	db        *sql.DB
	mailer    *mail.Mailer
	auth      *auth.Auth
	content   *content.Service
	gallery   *gallery.Repo
	media     *media.Store
	cfg       Config
}

func New(cfg Config) (*Server, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	fragments, err := template.ParseFS(web.Templates, "templates/fragments.html")
	if err != nil {
		return nil, fmt.Errorf("parse fragments: %w", err)
	}

	s := &Server{
		mux:       http.NewServeMux(),
		pages:     pages,
		fragments: fragments,
		db:        cfg.DB,
		mailer:    cfg.Mailer,
		auth:      cfg.Auth,
		content:   cfg.Content,
		gallery:   cfg.Gallery,
		media:     cfg.Media,
		cfg:       cfg,
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	staticFS, _ := fs.Sub(web.Static, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public pages
	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /about", s.handleAbout)
	s.mux.HandleFunc("GET /gallery", s.handleGallery)
	s.mux.HandleFunc("GET /shop", s.handleShop)
	s.mux.HandleFunc("GET /contact", s.handleContact)
	s.mux.HandleFunc("POST /contact", s.handleContactSubmit)
	s.mux.HandleFunc("POST /waitlist", s.handleWaitlist)

	// Auth
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)

	// Account — any authenticated user (admin or user)
	authed := s.auth.RequireAuth()
	s.mux.Handle("GET /account", authed(http.HandlerFunc(s.handleAccountPage)))
	s.mux.Handle("POST /account/name", authed(http.HandlerFunc(s.handleAccountName)))
	s.mux.Handle("POST /account/email", authed(http.HandlerFunc(s.handleAccountEmail)))
	s.mux.Handle("POST /account/password", authed(http.HandlerFunc(s.handleAccountPassword)))

	// Public content rendering (for "Cancel" in inline editor)
	s.mux.HandleFunc("GET /content/view", s.handleContentView)

	// Admin — gated by admin role
	adminOnly := s.auth.RequireRole(auth.RoleAdmin)
	s.mux.Handle("GET /admin", adminOnly(http.HandlerFunc(s.handleAdminHome)))
	s.mux.Handle("GET /admin/settings", adminOnly(http.HandlerFunc(s.handleAdminSettings)))

	// Admin: content inline editing
	s.mux.Handle("GET /admin/content/edit", adminOnly(http.HandlerFunc(s.handleContentEdit)))
	s.mux.Handle("POST /admin/content", adminOnly(http.HandlerFunc(s.handleContentSave)))

	// Admin: gallery
	s.mux.Handle("POST /admin/gallery/upload", adminOnly(http.HandlerFunc(s.handleGalleryUpload)))
	s.mux.Handle("POST /admin/gallery/video", adminOnly(http.HandlerFunc(s.handleGalleryAddVideo)))
	s.mux.Handle("POST /admin/gallery/{id}/delete", adminOnly(http.HandlerFunc(s.handleGalleryDelete)))
	s.mux.Handle("POST /admin/gallery/{id}/move", adminOnly(http.HandlerFunc(s.handleGalleryMove)))
	s.mux.Handle("POST /admin/gallery/{id}/caption", adminOnly(http.HandlerFunc(s.handleGalleryCaption)))

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
}

func parsePages() (map[string]*template.Template, error) {
	entries, err := fs.ReadDir(web.Templates, "templates")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".html") {
			continue
		}
		if name == "base.html" || name == "partials.html" || name == "fragments.html" {
			continue
		}
		t, err := template.ParseFS(web.Templates,
			"templates/base.html",
			"templates/partials.html",
			"templates/fragments.html",
			path.Join("templates", name),
		)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	t, ok := s.pages[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderFragment writes a named fragment template — used for HTMX partial
// responses (no base layout).
func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.fragments.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// clientIP pulls the best-guess source IP, preferring X-Forwarded-For from nginx.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if ra := r.RemoteAddr; ra != "" {
		if i := strings.LastIndex(ra, ":"); i > 0 {
			return ra[:i]
		}
		return ra
	}
	return ""
}
