PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  display_name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin','user')),
  enabled INTEGER NOT NULL DEFAULT 1,
  force_password_change INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS integrations (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  bili_cookie_enc TEXT NOT NULL DEFAULT '',
  bili_status TEXT NOT NULL DEFAULT 'missing',
  bili_name TEXT NOT NULL DEFAULT '',
  bili_last_validated INTEGER,
  bili_error TEXT NOT NULL DEFAULT '',
  bark_server TEXT NOT NULL DEFAULT 'https://api.day.app',
  bark_key_enc TEXT NOT NULL DEFAULT '',
  bark_level TEXT NOT NULL DEFAULT 'active',
  bark_sound TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS bili_refresh_tokens (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_enc TEXT NOT NULL,
  refreshed_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS creators (
  mid TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NOT NULL DEFAULT '',
  latest_bvid TEXT NOT NULL DEFAULT '',
  latest_title TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  creator_mid TEXT NOT NULL REFERENCES creators(mid) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1,
  baseline_bvid TEXT NOT NULL DEFAULT '',
  subscribed_at INTEGER NOT NULL,
  UNIQUE(user_id, creator_mid)
);

CREATE TABLE IF NOT EXISTS videos (
  bvid TEXT PRIMARY KEY,
  creator_mid TEXT NOT NULL REFERENCES creators(mid) ON DELETE CASCADE,
  title TEXT NOT NULL,
  url TEXT NOT NULL,
  published_at INTEGER NOT NULL,
  detected_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  bvid TEXT NOT NULL REFERENCES videos(bvid) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  sent_at INTEGER,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, bvid)
);

CREATE TABLE IF NOT EXISTS poll_states (
  creator_mid TEXT PRIMARY KEY REFERENCES creators(mid) ON DELETE CASCADE,
  last_polled_at INTEGER,
  next_poll_at INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

INSERT OR IGNORE INTO app_settings(key, value) VALUES ('poll_interval_seconds', '300');
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_deliveries_due ON deliveries(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_deliveries_user_history ON deliveries(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_subscriptions_creator ON subscriptions(creator_mid, enabled);

-- Older versions treated Bark's {"code":200,"message":"success"} response as a
-- failure and retried notifications that had already been delivered.
UPDATE deliveries
SET status = 'sent', last_error = '', sent_at = COALESCE(sent_at, created_at)
WHERE status IN ('pending', 'failed') AND last_error = 'success';

-- Following imports created by older versions waited before their first poll,
-- leaving the UI to report that no public videos existed in the meantime.
UPDATE poll_states
SET next_poll_at = 0
WHERE last_polled_at IS NULL
  AND last_error = ''
  AND EXISTS (
    SELECT 1 FROM creators c
    WHERE c.mid = poll_states.creator_mid AND c.latest_bvid = ''
  );
