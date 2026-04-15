package socials

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Social struct {
	ID           int64
	Platform     string
	Label        string
	Username     string
	Sublabel     string
	URL          string
	ThumbnailKey string
	ThumbnailURL string // populated by the server layer from media.Store
	SortOrder    int
	UpdatedAt    time.Time
}

// IconName maps the platform slug to an icon name registered in
// internal/icons. Falls back to "external-link" for unknown platforms.
func (s Social) IconName() string {
	switch strings.ToLower(s.Platform) {
	case "youtube":
		return "youtube"
	case "instagram":
		return "instagram"
	case "tiktok":
		return "tiktok"
	case "mail", "email":
		return "mail"
	default:
		return "external-link"
	}
}

// GradientClass returns a Tailwind gradient that reads as the platform's
// brand-ish color, used as a fallback when no thumbnail is uploaded.
func (s Social) GradientClass() string {
	switch strings.ToLower(s.Platform) {
	case "youtube":
		return "from-red-500/20 to-red-700/20"
	case "instagram":
		return "from-pink-500/20 via-fuchsia-500/15 to-amber-400/20"
	case "tiktok":
		return "from-cyan-400/20 to-rose-500/20"
	default:
		return "from-slate-800 to-slate-900"
	}
}

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func (r *Repo) List(ctx context.Context) ([]Social, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, platform, label, username, sublabel, url,
		       COALESCE(thumbnail_s3_key,''), sort_order, updated_at
		FROM social_links
		ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Social
	for rows.Next() {
		var s Social
		if err := rows.Scan(&s.ID, &s.Platform, &s.Label, &s.Username, &s.Sublabel,
			&s.URL, &s.ThumbnailKey, &s.SortOrder, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, id int64) (*Social, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, platform, label, username, sublabel, url,
		       COALESCE(thumbnail_s3_key,''), sort_order, updated_at
		FROM social_links WHERE id = ?`, id)
	var s Social
	if err := row.Scan(&s.ID, &s.Platform, &s.Label, &s.Username, &s.Sublabel,
		&s.URL, &s.ThumbnailKey, &s.SortOrder, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) UpdateFields(ctx context.Context, id int64, label, username, sublabel, url string, userID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE social_links
		SET label = ?, username = ?, sublabel = ?, url = ?,
		    updated_at = CURRENT_TIMESTAMP, updated_by = ?
		WHERE id = ?`,
		strings.TrimSpace(label),
		strings.TrimSpace(username),
		strings.TrimSpace(sublabel),
		strings.TrimSpace(url),
		userID, id,
	)
	return err
}

// SetThumbnail replaces thumbnail_s3_key and returns the previous key so the
// caller can delete the old object from S3.
func (r *Repo) SetThumbnail(ctx context.Context, id int64, key string, userID int64) (oldKey string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(thumbnail_s3_key,'') FROM social_links WHERE id = ?`, id).Scan(&oldKey); err != nil {
		return "", err
	}
	var newKey any
	if key != "" {
		newKey = key
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE social_links
		SET thumbnail_s3_key = ?, updated_at = CURRENT_TIMESTAMP, updated_by = ?
		WHERE id = ?`, newKey, userID, id); err != nil {
		return "", err
	}
	return oldKey, tx.Commit()
}
