package server

import (
	"context"
	"net/http"
	"time"

	"github.com/fhak/pelagicsociety/internal/auth"
)

type adminHomeData struct {
	pageData
	User            *auth.User
	WaitlistCount   int
	ContactCount    int
	RecentContacts  []contactRow
	RecentWaitlist  []waitlistRow
}

type contactRow struct {
	ID        int64
	Kind      string
	Name      string
	Email     string
	CreatedAt time.Time
}

type waitlistRow struct {
	ID        int64
	Email     string
	Source    string
	CreatedAt time.Time
}

func (s *Server) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data := adminHomeData{
		pageData: pageData{Title: "Admin — Pelagic Society", Path: "/admin"},
		User:     u,
	}

	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM waitlist`).Scan(&data.WaitlistCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contact_submissions`).Scan(&data.ContactCount)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, name, email, created_at FROM contact_submissions ORDER BY id DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var c contactRow
			if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.Email, &c.CreatedAt); err == nil {
				data.RecentContacts = append(data.RecentContacts, c)
			}
		}
		rows.Close()
	}

	rows2, err := s.db.QueryContext(ctx,
		`SELECT id, email, COALESCE(source,''), created_at FROM waitlist ORDER BY id DESC LIMIT 10`)
	if err == nil {
		for rows2.Next() {
			var wl waitlistRow
			if err := rows2.Scan(&wl.ID, &wl.Email, &wl.Source, &wl.CreatedAt); err == nil {
				data.RecentWaitlist = append(data.RecentWaitlist, wl)
			}
		}
		rows2.Close()
	}

	s.render(w, "admin.html", data)
}
