package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/fhak/pelagicsociety/internal/auth"
)

func (s *Server) handleSocialView(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r, "/social/", "")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	view, err := s.socialViewByID(r.Context(), id, s.auth.UserFromRequest(r))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.renderFragment(w, "social_view", view)
}

func (s *Server) handleSocialEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r, "/admin/social/", "/edit")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	view, err := s.socialViewByID(r.Context(), id, auth.UserFrom(r.Context()))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.renderFragment(w, "social_edit", view)
}

func (s *Server) handleSocialSave(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id, ok := parseIDPath(r, "/admin/social/", "")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	err := s.socials.UpdateFields(r.Context(), id,
		r.FormValue("label"),
		r.FormValue("username"),
		r.FormValue("sublabel"),
		strings.TrimSpace(r.FormValue("url")),
		u.ID,
	)
	if err != nil {
		log.Printf("social save: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	view, err := s.socialViewByID(r.Context(), id, u)
	if err != nil {
		http.Error(w, "refresh failed", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "social_view", view)
}

func (s *Server) handleSocialThumbnail(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id, ok := parseIDPath(r, "/admin/social/", "/thumbnail")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if !s.media.Enabled() {
		http.Error(w, "media storage not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		http.Error(w, "bad upload", http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("thumbnail")
	if err != nil {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploaded, err := s.media.UploadPhoto(r.Context(), file, fh.Filename, fh.Header.Get("Content-Type"))
	if err != nil {
		log.Printf("social thumb upload: %v", err)
		http.Error(w, "upload failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// We only keep the resized variant — social cards don't need the huge
	// original, and storing both doubles egress for no reason here.
	_ = s.media.Delete(r.Context(), uploaded.Key)

	oldKey, err := s.socials.SetThumbnail(r.Context(), id, uploaded.ThumbKey, u.ID)
	if err != nil {
		_ = s.media.Delete(r.Context(), uploaded.ThumbKey)
		log.Printf("social thumb db: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	if oldKey != "" && oldKey != uploaded.ThumbKey {
		_ = s.media.Delete(r.Context(), oldKey)
	}

	view, err := s.socialViewByID(r.Context(), id, u)
	if err != nil {
		http.Error(w, "refresh failed", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "social_view", view)
}

func (s *Server) handleSocialThumbnailClear(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id, ok := parseIDPath(r, "/admin/social/", "/thumbnail/clear")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	oldKey, err := s.socials.SetThumbnail(r.Context(), id, "", u.ID)
	if err != nil {
		log.Printf("social thumb clear: %v", err)
		http.Error(w, "clear failed", http.StatusInternalServerError)
		return
	}
	if oldKey != "" {
		_ = s.media.Delete(r.Context(), oldKey)
	}
	view, err := s.socialViewByID(r.Context(), id, u)
	if err != nil {
		http.Error(w, "refresh failed", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "social_view", view)
}
