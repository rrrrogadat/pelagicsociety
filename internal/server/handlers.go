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
	"github.com/fhak/pelagicsociety/internal/content"
	mailer "github.com/fhak/pelagicsociety/internal/mail"
	"github.com/fhak/pelagicsociety/internal/socials"
)

type pageData struct {
	Title       string
	Description string
	Path        string
	User        *auth.User
	Socials     []socials.Social
	Copyright   *content.BlockView // rendered in the shared footer
}

// pageFor builds the base page data for a request, pulling the current user
// from the session cookie if present and loading social links + footer
// content so the shared partials can render without handler plumbing.
func (s *Server) pageFor(r *http.Request, title, path string) pageData {
	u := s.auth.UserFromRequest(r)
	list, _ := s.socials.List(r.Context())
	for i := range list {
		if list[i].ThumbnailKey != "" {
			list[i].ThumbnailURL = s.media.URL(list[i].ThumbnailKey)
		}
	}
	return pageData{
		Title:     title,
		Path:      path,
		User:      u,
		Socials:   list,
		Copyright: s.blockView(r.Context(), "footer.copyright", "© 2026 Pelagic Society", "text-slate-400", u),
	}
}

// socialView enriches a single Social with the IsAdmin flag and thumbnail URL
// so fragment templates can render without threading extra context.
type socialView struct {
	Social  socials.Social
	IsAdmin bool
}

func (s *Server) socialViewByID(ctx context.Context, id int64, u *auth.User) (*socialView, error) {
	sc, err := s.socials.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc.ThumbnailKey != "" {
		sc.ThumbnailURL = s.media.URL(sc.ThumbnailKey)
	}
	return &socialView{Social: *sc, IsAdmin: u.IsAdmin()}, nil
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "Pelagic Society", "/")
	p.Description = "Spearfishing, freediving, and open water adventures."

	socialViews := make([]*socialView, 0, len(p.Socials))
	for i := range p.Socials {
		socialViews = append(socialViews, &socialView{Social: p.Socials[i], IsAdmin: p.User.IsAdmin()})
	}

	ctx := r.Context()
	ctaPrimaryClass := "inline-flex items-center justify-center px-6 py-3 rounded bg-sky-500 hover:bg-sky-400 text-slate-950 font-semibold transition"

	s.render(w, "home.html", homePageData{
		pageData:        p,
		HeroTagline:     s.blockView(ctx, "hero.tagline", "Spearfishing · Freediving · Open Water", "uppercase tracking-[0.3em] text-sky-400 text-sm", p.User),
		HeroHeading:     s.blockView(ctx, "hero.heading", "Life beyond the shoreline.", "text-5xl md:text-7xl font-bold tracking-tight max-w-3xl", p.User),
		HeroBody:        s.blockView(ctx, "hero.body", "Chasing pelagic fish and telling the stories of the open ocean. New videos every week.", "mt-6 text-lg text-slate-300 max-w-xl", p.User),
		HeroCTAPrimary:  s.linkView(ctx, "hero.cta_primary", "Watch on YouTube", "https://www.youtube.com/@pelagicsociety", ctaPrimaryClass, p.User),
		HeroCTASecond:   s.blockView(ctx, "hero.cta_secondary", "Business Inquiries", "inline-block", p.User),
		SocialsHeading:  s.blockView(ctx, "home.socials_heading", "Follow along", "text-3xl font-bold", p.User),
		MerchHeading:    s.blockView(ctx, "merch.heading", "Merch is coming.", "text-3xl font-bold", p.User),
		MerchBody:       s.blockView(ctx, "merch.body", "Pelagic Society drops are limited. Get on the list for first access.", "mt-3 text-slate-300 max-w-xl", p.User),
		SocialViews:     socialViews,
	})
}

type homePageData struct {
	pageData
	HeroTagline    *content.BlockView
	HeroHeading    *content.BlockView
	HeroBody       *content.BlockView
	HeroCTAPrimary *content.LinkBlockView
	HeroCTASecond  *content.BlockView
	SocialsHeading *content.BlockView
	MerchHeading   *content.BlockView
	MerchBody      *content.BlockView
	SocialViews    []*socialView
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "About — Pelagic Society", "/about")
	s.render(w, "about.html", aboutPageData{
		pageData: p,
		Heading:  s.blockView(r.Context(), "about.heading", "About", "text-5xl font-bold tracking-tight", p.User),
		Body:     s.blockView(r.Context(), "about.body", "Pelagic Society is a chronicle of life spent in and under the open ocean — spearfishing, freediving, and the people and places that shape it.", "prose prose-invert max-w-none text-lg text-slate-300 leading-relaxed", p.User),
	})
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "Gallery — Pelagic Society", "/gallery")
	items, _ := s.gallery.List(r.Context())
	views := make([]galleryItemView, 0, len(items))
	for i, it := range items {
		v := galleryItemView{
			ID:          it.ID,
			Kind:        string(it.Kind),
			Caption:     it.Caption,
			CanMoveUp:   i > 0,
			CanMoveDown: i < len(items)-1,
			IsAdmin:     p.User.IsAdmin(),
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
	s.render(w, "gallery.html", galleryPageData{
		pageData: p,
		Heading:  s.blockView(r.Context(), "gallery.heading", "Gallery", "text-5xl font-bold tracking-tight", p.User),
		Intro:    s.blockView(r.Context(), "gallery.intro", "A selection of favorite frames from the water.", "mt-4 text-slate-400", p.User),
		Items:    views,
		CanEdit:  p.User.IsAdmin(),
	})
}

type aboutPageData struct {
	pageData
	Heading *content.BlockView
	Body    *content.BlockView
}

type galleryPageData struct {
	pageData
	Heading *content.BlockView
	Intro   *content.BlockView
	Items   []galleryItemView
	CanEdit bool
}

type galleryItemView struct {
	ID          int64
	Kind        string
	Caption     string
	ThumbURL    string
	FullURL     string
	YouTubeID   string
	Width       int
	Height      int
	CanMoveUp   bool
	CanMoveDown bool
	IsAdmin     bool
}

func (s *Server) blockView(ctx context.Context, key, fallback, class string, u *auth.User) *content.BlockView {
	return &content.BlockView{
		Key:     key,
		HTML:    s.content.Render(ctx, key, fallback),
		Raw:     s.content.Raw(ctx, key, fallback),
		Class:   class,
		IsAdmin: u.IsAdmin(),
	}
}

func (s *Server) linkView(ctx context.Context, key, fallbackLabel, fallbackURL, class string, u *auth.User) *content.LinkBlockView {
	lv := s.content.Link(ctx, key, fallbackLabel, fallbackURL)
	lv.Class = class
	lv.IsAdmin = u.IsAdmin()
	return &lv
}

func (s *Server) handleShop(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "Shop — Pelagic Society", "/shop")
	s.render(w, "shop.html", shopPageData{
		pageData: p,
		Tagline:  s.blockView(r.Context(), "shop.tagline", "Shop", "uppercase tracking-[0.3em] text-sky-400 text-sm", p.User),
		Heading:  s.blockView(r.Context(), "shop.heading", "Coming soon.", "mt-4 text-5xl md:text-6xl font-bold tracking-tight", p.User),
		Body:     s.blockView(r.Context(), "shop.body", "Apparel and gear, built for the water. Limited drops only.", "mt-6 text-lg text-slate-300", p.User),
	})
}

func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor(r, "Contact — Pelagic Society", "/contact")
	s.render(w, "contact.html", contactPageData{
		pageData: p,
		Heading:  s.blockView(r.Context(), "contact.heading", "Contact", "text-5xl font-bold tracking-tight", p.User),
		Intro:    s.blockView(r.Context(), "contact.intro", "Sponsorships, collaborations, press, or just saying hi.", "mt-4 text-slate-400", p.User),
	})
}

type shopPageData struct {
	pageData
	Tagline *content.BlockView
	Heading *content.BlockView
	Body    *content.BlockView
}

type contactPageData struct {
	pageData
	Heading *content.BlockView
	Intro   *content.BlockView
}

func validateEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return "", errors.New("invalid email")
	}
	return addr.Address, nil
}

// writeFormResult renders a small HTMX-friendly fragment with an icon.
func (s *Server) writeFormResult(w http.ResponseWriter, status int, tone, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	name := "form_ok"
	if tone == "error" {
		name = "form_error"
	}
	if err := s.fragments.ExecuteTemplate(w, name, msg); err != nil {
		// Fallback so we never send zero-byte on error.
		fmt.Fprintf(w, "<p>%s</p>", msg)
	}
}

func (s *Server) handleContactSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeFormResult(w, http.StatusBadRequest, "error", "Bad form submission.")
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
		s.writeFormResult(w, http.StatusBadRequest, "error", "Please fill out all fields with a valid email.")
		return
	}
	if len(message) > 10000 || len(name) > 200 {
		s.writeFormResult(w, http.StatusBadRequest, "error", "Message too long.")
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
		s.writeFormResult(w, http.StatusInternalServerError, "error", "Something went wrong. Please try again.")
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
	s.writeFormResult(w, http.StatusOK, "ok", "Thanks — message received. We'll be in touch.")
}

func (s *Server) handleWaitlist(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeFormResult(w, http.StatusBadRequest, "error", "Bad submission.")
		return
	}

	email, err := validateEmail(r.FormValue("email"))
	if err != nil {
		s.writeFormResult(w, http.StatusBadRequest, "error", "Enter a valid email.")
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
		s.writeFormResult(w, http.StatusInternalServerError, "error", "Something went wrong. Please try again.")
		return
	}

	log.Printf("waitlist: email=%q source=%q", email, source)
	s.writeFormResult(w, http.StatusOK, "ok", "You're on the list.")
}
