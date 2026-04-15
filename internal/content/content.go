package content

import (
	"bytes"
	"context"
	"database/sql"
	"html/template"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Service reads and writes editable content blocks. Values are markdown; the
// Render helpers return sanitized HTML. Small in-memory cache keeps hot-path
// page renders off the DB.
type Service struct {
	db *sql.DB
	md goldmark.Markdown

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	rawValue  string
	htmlValue template.HTML
	expiresAt time.Time
}

const cacheTTL = 30 * time.Second

// BlockView is what templates render — already-escaped HTML plus the raw
// markdown for the editor and an IsAdmin flag so the partial can toggle the
// edit pencil without crossing contexts.
type BlockView struct {
	Key     string
	HTML    template.HTML
	Raw     string
	Class   string
	IsAdmin bool
}

func New(db *sql.DB) *Service {
	md := goldmark.New(
		goldmark.WithExtensions(extension.Linkify, extension.Strikethrough),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			// Safe by default: goldmark does not render raw HTML.
		),
	)
	return &Service{db: db, md: md, cache: map[string]cacheEntry{}}
}

// Raw returns the raw markdown for a key, falling back to fallback if unset.
func (s *Service) Raw(ctx context.Context, key, fallback string) string {
	if e, ok := s.getCache(key); ok {
		return e.rawValue
	}
	raw := s.load(ctx, key)
	if raw == "" {
		return fallback
	}
	return raw
}

// Render returns the block's HTML rendering (markdown → HTML). Falls back to
// fallback (rendered as markdown) if the key is unset.
func (s *Service) Render(ctx context.Context, key, fallback string) template.HTML {
	if e, ok := s.getCache(key); ok {
		return e.htmlValue
	}
	raw := s.load(ctx, key)
	if raw == "" {
		raw = fallback
	}
	var buf bytes.Buffer
	if err := s.md.Convert([]byte(raw), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(raw))
	}
	s.putCache(key, raw, template.HTML(buf.String()))
	return template.HTML(buf.String())
}

// Set upserts a block and invalidates the cache entry.
func (s *Service) Set(ctx context.Context, key, value string, userID int64) error {
	value = strings.TrimSpace(value)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO content_blocks(key, value, updated_at, updated_by)
		VALUES(?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(key) DO UPDATE SET
		  value = excluded.value,
		  updated_at = CURRENT_TIMESTAMP,
		  updated_by = excluded.updated_by`,
		key, value, userID,
	)
	if err != nil {
		return err
	}
	s.invalidate(key)
	return nil
}

// ---- cache ----

func (s *Service) getCache(key string) (cacheEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[key]
	if !ok || time.Now().After(e.expiresAt) {
		return cacheEntry{}, false
	}
	return e, true
}

func (s *Service) putCache(key, raw string, html template.HTML) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = cacheEntry{rawValue: raw, htmlValue: html, expiresAt: time.Now().Add(cacheTTL)}
}

func (s *Service) invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, key)
}

func (s *Service) load(ctx context.Context, key string) string {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM content_blocks WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}
