package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/fhak/pelagicsociety/internal/auth"
	"github.com/fhak/pelagicsociety/internal/content"
)

// handleContentView returns the read-only render of a block. Used by the
// "Cancel" button in the inline editor.
func (s *Server) handleContentView(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	class := r.URL.Query().Get("class")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	bv := s.blockView(r.Context(), key, "", class, s.auth.UserFromRequest(r))
	s.renderFragment(w, "block_view", bv)
}

// handleContentEdit returns the editable form for a block.
func (s *Server) handleContentEdit(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	class := r.URL.Query().Get("class")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	bv := s.blockView(r.Context(), key, "", class, auth.UserFrom(r.Context()))
	s.renderFragment(w, "block_edit", bv)
}

// ---- link blocks (label + url) ----

func (s *Server) handleLinkView(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	class := r.URL.Query().Get("class")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	lv := s.content.Link(r.Context(), key, "", "")
	lv.Class = class
	lv.IsAdmin = s.auth.UserFromRequest(r).IsAdmin()
	s.renderFragment(w, "link_view", lv)
}

func (s *Server) handleLinkEdit(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	class := r.URL.Query().Get("class")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	lv := s.content.Link(r.Context(), key, "", "")
	lv.Class = class
	lv.IsAdmin = true
	s.renderFragment(w, "link_edit", lv)
}

func (s *Server) handleLinkSave(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(r.FormValue("key"))
	class := r.FormValue("class")
	label := r.FormValue("label")
	url := strings.TrimSpace(r.FormValue("url"))
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := s.content.SetLink(r.Context(), key, label, url, u.ID); err != nil {
		log.Printf("link save: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	lv := s.content.Link(r.Context(), key, "", "")
	lv.Class = class
	lv.IsAdmin = true
	s.renderFragment(w, "link_view", lv)
}

// handleContentSave persists a block and returns the rendered view.
func (s *Server) handleContentSave(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(r.FormValue("key"))
	class := r.FormValue("class")
	value := r.FormValue("value")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := s.content.Set(r.Context(), key, value, u.ID); err != nil {
		log.Printf("content save: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "block_view", &content.BlockView{
		Key:     key,
		HTML:    s.content.Render(r.Context(), key, ""),
		Raw:     s.content.Raw(r.Context(), key, ""),
		Class:   class,
		IsAdmin: u.IsAdmin(),
	})
}
