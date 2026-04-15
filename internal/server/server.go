package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/fhak/pelagicsociety/web"
)

type Server struct {
	mux   *http.ServeMux
	pages map[string]*template.Template
}

func New() (*Server, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}

	s := &Server{
		mux:   http.NewServeMux(),
		pages: pages,
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

	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /about", s.handleAbout)
	s.mux.HandleFunc("GET /gallery", s.handleGallery)
	s.mux.HandleFunc("GET /shop", s.handleShop)
	s.mux.HandleFunc("GET /contact", s.handleContact)
	s.mux.HandleFunc("POST /contact", s.handleContactSubmit)
	s.mux.HandleFunc("POST /waitlist", s.handleWaitlist)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
}

// parsePages builds one template set per page, each composed with base.html +
// partials.html. This avoids the flat-namespace collision of {{define "content"}}
// blocks across multiple page files.
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
		if name == "base.html" || name == "partials.html" {
			continue
		}
		t, err := template.ParseFS(web.Templates,
			"templates/base.html",
			"templates/partials.html",
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
