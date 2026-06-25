package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    email        TEXT NOT NULL UNIQUE,
    oidc_subject TEXT UNIQUE,
    created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS searches (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source           TEXT NOT NULL,
    query            TEXT NOT NULL,
    params           TEXT NOT NULL DEFAULT '{}',
    min_price        REAL,
    max_price        REAL,
    interval_seconds INTEGER NOT NULL DEFAULT 300,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    last_run_at      INTEGER
);

CREATE TABLE IF NOT EXISTS listings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    search_id   INTEGER NOT NULL REFERENCES searches(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title       TEXT NOT NULL,
    price       REAL NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    extra       TEXT NOT NULL DEFAULT '{}',
    first_seen  INTEGER NOT NULL,
    listed_at   INTEGER,
    notified    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(search_id, external_id)
);
CREATE INDEX IF NOT EXISTS idx_listings_search_seen ON listings(search_id, first_seen DESC);

CREATE TABLE IF NOT EXISTS notification_targets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    config     TEXT NOT NULL DEFAULT '{}',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}

	return s.addColumnIfMissing(ctx, "listings", "listed_at", "INTEGER")
}

func (s *Store) addColumnIfMissing(ctx context.Context, table, column, typ string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             any
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Close()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ))
	return err
}

func (s *Store) EnsureDefaultUser(ctx context.Context, name, email string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, COALESCE(oidc_subject,''), created_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.OIDCSubject, asTime(&u.CreatedAt))
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, email, created_at) VALUES (?, ?, ?)`, name, email, now.Unix())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Name: name, Email: email, CreatedAt: now}, nil
}

func (s *Store) ListSearches(ctx context.Context, enabledOnly bool) ([]Search, error) {
	q := `SELECT id, user_id, source, query, params, min_price, max_price,
	             interval_seconds, enabled, created_at, last_run_at
	      FROM searches`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Search
	for rows.Next() {
		se, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) GetSearch(ctx context.Context, id int64) (Search, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, source, query, params, min_price, max_price,
		        interval_seconds, enabled, created_at, last_run_at
		 FROM searches WHERE id = ?`, id)
	return scanSearch(row)
}

func (s *Store) CreateSearch(ctx context.Context, se Search) (int64, error) {
	params, err := json.Marshal(orEmpty(se.Params))
	if err != nil {
		return 0, err
	}
	interval := int64(se.Interval / time.Second)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO searches (user_id, source, query, params, min_price, max_price,
		                       interval_seconds, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		se.UserID, se.Source, se.Query, string(params),
		nullFloat(se.MinPrice), nullFloat(se.MaxPrice),
		interval, boolToInt(se.Enabled), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSearch(ctx context.Context, se Search) error {
	params, err := json.Marshal(orEmpty(se.Params))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE searches SET source = ?, query = ?, params = ?, min_price = ?, max_price = ?,
		        interval_seconds = ?, enabled = ?
		 WHERE id = ?`,
		se.Source, se.Query, string(params), nullFloat(se.MinPrice), nullFloat(se.MaxPrice),
		int64(se.Interval/time.Second), boolToInt(se.Enabled), se.ID)
	return err
}

func (s *Store) SetSearchEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE searches SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	return err
}

func (s *Store) DeleteSearch(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM searches WHERE id = ?`, id)
	return err
}

func (s *Store) TouchSearchRun(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE searches SET last_run_at = ? WHERE id = ?`, at.Unix(), id)
	return err
}

func (s *Store) RecordListing(ctx context.Context, searchID int64, src string, l source.Listing, seenAt time.Time) (Listing, bool, error) {
	extra, err := json.Marshal(orEmpty(l.Extra))
	if err != nil {
		return Listing{}, false, err
	}
	now := seenAt
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO listings (search_id, source, external_id, title, price, currency, url, image_url, extra, first_seen, listed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(search_id, external_id) DO NOTHING`,
		searchID, src, l.ExternalID, l.Title, l.Price, l.Currency, l.URL, l.ImageURL, string(extra), now.Unix(), nullUnix(l.ListedAt))
	if err != nil {
		return Listing{}, false, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Listing{}, false, nil
	}
	id, _ := res.LastInsertId()
	return Listing{
		ID: id, SearchID: searchID, Source: src, ExternalID: l.ExternalID,
		Title: l.Title, Price: l.Price, Currency: l.Currency, URL: l.URL,
		ImageURL: l.ImageURL, Extra: l.Extra, FirstSeen: now, ListedAt: l.ListedAt,
	}, true, nil
}

func (s *Store) MarkNotified(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE listings SET notified = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) RecentListings(ctx context.Context, limit int) ([]Listing, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, search_id, source, external_id, title, price, currency, url, image_url, extra, first_seen, listed_at, notified
		 FROM listings
		 WHERE id IN (SELECT MIN(id) FROM listings GROUP BY source, external_id)
		 ORDER BY first_seen DESC, id ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanListings(rows)
}

func (s *Store) ListTargets(ctx context.Context, userID int64) ([]NotificationTarget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, kind, config, enabled, created_at
		 FROM notification_targets WHERE user_id = ? AND enabled = 1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NotificationTarget
	for rows.Next() {
		var t NotificationTarget
		var cfg string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Kind, &cfg, asBool(&t.Enabled), asTime(&t.CreatedAt)); err != nil {
			return nil, err
		}
		t.Config = decodeMap(cfg)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) EnsureTelegramTarget(ctx context.Context, userID int64, chatID string) error {
	targets, err := s.ListTargets(ctx, userID)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if t.Kind == "telegram" && t.Config["chat_id"] == chatID {
			return nil
		}
	}
	_, err = s.CreateTarget(ctx, NotificationTarget{
		UserID:  userID,
		Kind:    "telegram",
		Config:  map[string]string{"chat_id": chatID},
		Enabled: true,
	})
	return err
}

func (s *Store) CreateTarget(ctx context.Context, t NotificationTarget) (int64, error) {
	cfg, err := json.Marshal(orEmpty(t.Config))
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_targets (user_id, kind, config, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?)`, t.UserID, t.Kind, string(cfg), boolToInt(t.Enabled), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSearch(sc scanner) (Search, error) {
	var (
		se       Search
		params   string
		minP     sql.NullFloat64
		maxP     sql.NullFloat64
		interval int64
		lastRun  sql.NullInt64
	)
	if err := sc.Scan(&se.ID, &se.UserID, &se.Source, &se.Query, &params,
		&minP, &maxP, &interval, asBool(&se.Enabled), asTime(&se.CreatedAt), &lastRun); err != nil {
		return Search{}, err
	}
	se.Params = decodeMap(params)
	if minP.Valid {
		se.MinPrice = &minP.Float64
	}
	if maxP.Valid {
		se.MaxPrice = &maxP.Float64
	}
	se.Interval = time.Duration(interval) * time.Second
	if lastRun.Valid {
		t := time.Unix(lastRun.Int64, 0)
		se.LastRunAt = &t
	}
	return se, nil
}

func scanListings(rows *sql.Rows) ([]Listing, error) {
	var out []Listing
	for rows.Next() {
		var (
			l        Listing
			extra    string
			listedAt sql.NullInt64
		)
		if err := rows.Scan(&l.ID, &l.SearchID, &l.Source, &l.ExternalID, &l.Title,
			&l.Price, &l.Currency, &l.URL, &l.ImageURL, &extra, asTime(&l.FirstSeen), &listedAt, asBool(&l.Notified)); err != nil {
			return nil, err
		}
		l.Extra = decodeMap(extra)
		if listedAt.Valid {
			l.ListedAt = time.Unix(listedAt.Int64, 0)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func decodeMap(s string) map[string]string {
	if s == "" {
		return map[string]string{}
	}
	m := map[string]string{}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func nullUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func asTime(t *time.Time) sql.Scanner { return (*unixTime)(t) }

type unixTime time.Time

func (u *unixTime) Scan(v any) error {
	switch n := v.(type) {
	case int64:
		*u = unixTime(time.Unix(n, 0))
	case nil:
		*u = unixTime(time.Time{})
	default:
		return fmt.Errorf("unixTime: unexpected %T", v)
	}
	return nil
}

func asBool(b *bool) sql.Scanner { return (*intBool)(b) }

type intBool bool

func (i *intBool) Scan(v any) error {
	switch n := v.(type) {
	case int64:
		*i = intBool(n != 0)
	case bool:
		*i = intBool(n)
	case nil:
		*i = false
	default:
		return fmt.Errorf("intBool: unexpected %T", v)
	}
	return nil
}
