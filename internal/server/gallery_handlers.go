package server

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/fhak/pelagicsociety/internal/auth"
	"github.com/fhak/pelagicsociety/internal/gallery"
)

const maxMultipartMemory = 32 << 20 // 32 MiB in-memory; rest spooled to /tmp

func (s *Server) handleGalleryUpload(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())

	if !s.media.Enabled() {
		writeFormResult(w, http.StatusServiceUnavailable, "error", "Media storage not configured.")
		return
	}
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad upload.")
		return
	}
	file, fh, err := r.FormFile("photo")
	if err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "No file provided.")
		return
	}
	defer file.Close()

	mime := fh.Header.Get("Content-Type")
	uploaded, err := s.media.UploadPhoto(r.Context(), file, fh.Filename, mime)
	if err != nil {
		log.Printf("gallery upload: %v", err)
		writeFormResult(w, http.StatusBadRequest, "error", "Couldn't upload: "+err.Error())
		return
	}
	caption := strings.TrimSpace(r.FormValue("caption"))
	if _, err := s.gallery.AddPhoto(r.Context(), uploaded.Key, uploaded.ThumbKey, caption, uploaded.Width, uploaded.Height, u.ID); err != nil {
		// Best-effort cleanup if DB insert fails.
		_ = s.media.Delete(r.Context(), uploaded.Key)
		_ = s.media.Delete(r.Context(), uploaded.ThumbKey)
		log.Printf("gallery insert: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save item.")
		return
	}
	// Return refreshed gallery grid for HTMX out-of-band update.
	s.renderGalleryGrid(w, r)
}

func (s *Server) handleGalleryAddVideo(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad form.")
		return
	}
	urlRaw := r.FormValue("url")
	id := gallery.ParseYouTubeID(urlRaw)
	if id == "" {
		writeFormResult(w, http.StatusBadRequest, "error", "Unrecognized YouTube URL.")
		return
	}
	caption := strings.TrimSpace(r.FormValue("caption"))
	if _, err := s.gallery.AddVideo(r.Context(), id, caption, u.ID); err != nil {
		log.Printf("gallery video: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save video.")
		return
	}
	s.renderGalleryGrid(w, r)
}

func (s *Server) handleGalleryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r, "/admin/gallery/", "/delete")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	it, err := s.gallery.Get(r.Context(), id)
	if err == nil && it != nil {
		if it.S3Key != "" {
			_ = s.media.Delete(r.Context(), it.S3Key)
		}
		if it.S3KeyThumb != "" {
			_ = s.media.Delete(r.Context(), it.S3KeyThumb)
		}
	}
	if err := s.gallery.Delete(r.Context(), id); err != nil {
		log.Printf("gallery delete: %v", err)
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	s.renderGalleryGrid(w, r)
}

func (s *Server) handleGalleryMove(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r, "/admin/gallery/", "/move")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	dir := 0
	switch r.FormValue("direction") {
	case "up":
		dir = -1
	case "down":
		dir = 1
	default:
		http.Error(w, "direction must be up or down", http.StatusBadRequest)
		return
	}
	if err := s.gallery.Move(r.Context(), id, dir); err != nil {
		log.Printf("gallery move: %v", err)
		http.Error(w, "move failed", http.StatusInternalServerError)
		return
	}
	s.renderGalleryGrid(w, r)
}

func (s *Server) handleGalleryCaption(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r, "/admin/gallery/", "/caption")
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeFormResult(w, http.StatusBadRequest, "error", "Bad form.")
		return
	}
	caption := strings.TrimSpace(r.FormValue("caption"))
	if err := s.gallery.UpdateCaption(r.Context(), id, caption); err != nil {
		log.Printf("caption save: %v", err)
		writeFormResult(w, http.StatusInternalServerError, "error", "Couldn't save.")
		return
	}
	writeFormResult(w, http.StatusOK, "ok", "Saved.")
}

// renderGalleryGrid rebuilds the entire grid + emits the fragment response.
// Simple and correct; fine for our scale.
func (s *Server) renderGalleryGrid(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	items, _ := s.gallery.List(r.Context())
	views := make([]galleryItemView, 0, len(items))
	for i, it := range items {
		v := galleryItemView{
			ID:          it.ID,
			Kind:        string(it.Kind),
			Caption:     it.Caption,
			CanMoveUp:   i > 0,
			CanMoveDown: i < len(items)-1,
			IsAdmin:     u.IsAdmin(),
		}
		switch it.Kind {
		case "photo":
			v.ThumbURL = s.media.URL(it.S3KeyThumb)
			v.FullURL = s.media.URL(it.S3Key)
			v.Width = it.Width
			v.Height = it.Height
		case "video":
			v.YouTubeID = it.YouTubeID
		}
		views = append(views, v)
	}
	s.renderFragment(w, "gallery_grid", struct {
		Items   []galleryItemView
		CanEdit bool
	}{Items: views, CanEdit: u.IsAdmin()})
}

// parseIDPath extracts a numeric id from /prefix/{id}/suffix.
func parseIDPath(r *http.Request, prefix, suffix string) (int64, bool) {
	p := r.URL.Path
	if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, suffix) {
		return 0, false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(p, prefix), suffix)
	id, err := strconv.ParseInt(middle, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// unused import guard for fmt (kept for future debugging)
var _ = fmt.Sprintf
