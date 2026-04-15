CREATE TABLE IF NOT EXISTS content_blocks (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by  INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- Seed the default copy for blocks we know we'll render. Editing a block via
-- the admin UI performs UPSERT on these rows.
INSERT OR IGNORE INTO content_blocks(key, value) VALUES
    ('about.heading',   'About'),
    ('about.body',      'Pelagic Society is a chronicle of life spent in and under the open ocean — spearfishing, freediving, and the people and places that shape it.'),
    ('gallery.heading', 'Gallery'),
    ('gallery.intro',   'A selection of favorite frames from the water.');

CREATE TABLE IF NOT EXISTS gallery_items (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT    NOT NULL CHECK (kind IN ('photo','video')),
    s3_key        TEXT,                          -- set when kind='photo'
    s3_key_thumb  TEXT,                          -- thumbnail variant
    youtube_id    TEXT,                          -- set when kind='video'
    caption       TEXT    NOT NULL DEFAULT '',
    width         INTEGER,
    height        INTEGER,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by    INTEGER REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_gallery_sort ON gallery_items(sort_order, id);
