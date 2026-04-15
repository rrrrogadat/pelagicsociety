package gallery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Kind string

const (
	KindPhoto Kind = "photo"
	KindVideo Kind = "video"
)

type Item struct {
	ID         int64
	Kind       Kind
	S3Key      string
	S3KeyThumb string
	YouTubeID  string
	Caption    string
	Width      int
	Height     int
	SortOrder  int
	CreatedAt  time.Time
}

// ThumbURL / FullURL are populated by the server when rendering; left as
// helper fields so templates can access without extra plumbing.
func (i Item) IsPhoto() bool { return i.Kind == KindPhoto }
func (i Item) IsVideo() bool { return i.Kind == KindVideo }

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func (r *Repo) List(ctx context.Context) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, kind, COALESCE(s3_key,''), COALESCE(s3_key_thumb,''),
		       COALESCE(youtube_id,''), caption, COALESCE(width,0), COALESCE(height,0),
		       sort_order, created_at
		FROM gallery_items
		ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var kind string
		if err := rows.Scan(&it.ID, &kind, &it.S3Key, &it.S3KeyThumb, &it.YouTubeID,
			&it.Caption, &it.Width, &it.Height, &it.SortOrder, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Kind = Kind(kind)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, id int64) (*Item, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, kind, COALESCE(s3_key,''), COALESCE(s3_key_thumb,''),
		       COALESCE(youtube_id,''), caption, COALESCE(width,0), COALESCE(height,0),
		       sort_order, created_at
		FROM gallery_items WHERE id = ?`, id)
	var it Item
	var kind string
	if err := row.Scan(&it.ID, &kind, &it.S3Key, &it.S3KeyThumb, &it.YouTubeID,
		&it.Caption, &it.Width, &it.Height, &it.SortOrder, &it.CreatedAt); err != nil {
		return nil, err
	}
	it.Kind = Kind(kind)
	return &it, nil
}

func (r *Repo) AddPhoto(ctx context.Context, s3Key, thumbKey, caption string, w, h int, userID int64) (int64, error) {
	var nextSort int
	_ = r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order),0)+1 FROM gallery_items`).Scan(&nextSort)
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO gallery_items(kind, s3_key, s3_key_thumb, caption, width, height, sort_order, created_by)
		VALUES('photo', ?, ?, ?, ?, ?, ?, ?)`,
		s3Key, thumbKey, strings.TrimSpace(caption), w, h, nextSort, userID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *Repo) AddVideo(ctx context.Context, youtubeID, caption string, userID int64) (int64, error) {
	var nextSort int
	_ = r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order),0)+1 FROM gallery_items`).Scan(&nextSort)
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO gallery_items(kind, youtube_id, caption, sort_order, created_by)
		VALUES('video', ?, ?, ?, ?)`,
		youtubeID, strings.TrimSpace(caption), nextSort, userID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *Repo) UpdateCaption(ctx context.Context, id int64, caption string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE gallery_items SET caption = ? WHERE id = ?`,
		strings.TrimSpace(caption), id)
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM gallery_items WHERE id = ?`, id)
	return err
}

// Move shifts an item up or down in the sort order by swapping with its
// neighbour. direction: -1 (up) or +1 (down).
func (r *Repo) Move(ctx context.Context, id int64, direction int) error {
	if direction != -1 && direction != 1 {
		return errors.New("direction must be -1 or 1")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var curSort int
	if err := tx.QueryRowContext(ctx, `SELECT sort_order FROM gallery_items WHERE id = ?`, id).Scan(&curSort); err != nil {
		return err
	}

	op, order := ">", "ASC"
	if direction == -1 {
		op, order = "<", "DESC"
	}
	var neighbourID int64
	var neighbourSort int
	err = tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id, sort_order FROM gallery_items WHERE sort_order %s ? ORDER BY sort_order %s LIMIT 1`, op, order),
		curSort).Scan(&neighbourID, &neighbourSort)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already at the edge
	}
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE gallery_items SET sort_order = ? WHERE id = ?`, neighbourSort, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE gallery_items SET sort_order = ? WHERE id = ?`, curSort, neighbourID); err != nil {
		return err
	}
	return tx.Commit()
}

// ParseYouTubeID extracts a video ID from any YouTube URL form (watch, short,
// shortened, embed, mobile). Returns "" for unrecognized input.
var ytIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func ParseYouTubeID(raw string) string {
	raw = strings.TrimSpace(raw)
	if ytIDPattern.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	switch host {
	case "youtu.be":
		id := strings.TrimPrefix(u.Path, "/")
		if ytIDPattern.MatchString(id) {
			return id
		}
	case "youtube.com":
		if v := u.Query().Get("v"); ytIDPattern.MatchString(v) {
			return v
		}
		if strings.HasPrefix(u.Path, "/shorts/") {
			id := strings.TrimPrefix(u.Path, "/shorts/")
			id = strings.SplitN(id, "/", 2)[0]
			if ytIDPattern.MatchString(id) {
				return id
			}
		}
		if strings.HasPrefix(u.Path, "/embed/") {
			id := strings.TrimPrefix(u.Path, "/embed/")
			id = strings.SplitN(id, "/", 2)[0]
			if ytIDPattern.MatchString(id) {
				return id
			}
		}
	}
	return ""
}
