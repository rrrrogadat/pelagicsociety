CREATE TABLE IF NOT EXISTS social_links (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    platform         TEXT    NOT NULL UNIQUE,
    label            TEXT    NOT NULL DEFAULT '',
    username         TEXT    NOT NULL DEFAULT '',
    sublabel         TEXT    NOT NULL DEFAULT '',
    url              TEXT    NOT NULL DEFAULT '',
    thumbnail_s3_key TEXT,
    sort_order       INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by       INTEGER REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_socials_sort ON social_links(sort_order, id);

INSERT OR IGNORE INTO social_links(platform, label, username, sublabel, url, sort_order) VALUES
    ('youtube',   'YouTube',   '@pelagicsociety', 'Long-form edits', 'https://www.youtube.com/@pelagicsociety',  10),
    ('instagram', 'Instagram', '@pelagicsociety', 'Daily shots',     'https://www.instagram.com/pelagicsociety', 20),
    ('tiktok',    'TikTok',    '@pelagicsociety', 'Short clips',     'https://www.tiktok.com/@pelagicsociety',   30);

INSERT OR IGNORE INTO content_blocks(key, value) VALUES
    ('merch.heading', 'Merch is coming.'),
    ('merch.body',    'Pelagic Society drops are limited. Get on the list for first access.');
