package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    name                     TEXT NOT NULL,
    email                    TEXT NOT NULL UNIQUE,
    oidc_subject             TEXT UNIQUE,
    password_hash            TEXT NOT NULL DEFAULT '',
    currency                 TEXT NOT NULL DEFAULT '',
    search_interval_seconds  INTEGER NOT NULL DEFAULT 0,
    monitor_interval_seconds INTEGER NOT NULL DEFAULT 0,
    created_at               INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

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
    sale_type   TEXT NOT NULL DEFAULT '',
    extra       TEXT NOT NULL DEFAULT '{}',
    first_seen  INTEGER NOT NULL,
    listed_at   INTEGER,
    notified    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(search_id, external_id)
);
CREATE INDEX IF NOT EXISTS idx_listings_search_seen ON listings(search_id, first_seen DESC);

CREATE TABLE IF NOT EXISTS monitored_items (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source           TEXT NOT NULL,
    external_id      TEXT NOT NULL,
    url              TEXT NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    image_url        TEXT NOT NULL DEFAULT '',
    currency         TEXT NOT NULL DEFAULT '',
    sale_type        TEXT NOT NULL DEFAULT '',
    last_price       REAL NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'active',
    interval_seconds INTEGER NOT NULL DEFAULT 3600,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    last_checked_at  INTEGER,
    ends_at          INTEGER NOT NULL DEFAULT 0,
    archived         INTEGER NOT NULL DEFAULT 0,
    UNIQUE(user_id, source, external_id)
);

CREATE TABLE IF NOT EXISTS monitor_prices (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitored_items(id) ON DELETE CASCADE,
    price       REAL NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'active',
    observed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_monitor_prices ON monitor_prices(monitor_id, observed_at);

CREATE TABLE IF NOT EXISTS source_exclusions (
    user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source             TEXT NOT NULL,
    exclude            TEXT NOT NULL DEFAULT '',
    exclude_categories TEXT NOT NULL DEFAULT '',
    paused             INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, source)
);

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

	if err := s.addColumnIfMissing(ctx, "listings", "listed_at", "INTEGER"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "listings", "sale_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "users", "password_hash", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "users", "currency", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "users", "search_interval_seconds", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "users", "monitor_interval_seconds", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "monitored_items", "ends_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "searches", "image", "BLOB"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "monitored_items", "archived", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "searches", "exclude", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "searches", "exclude_categories", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "users", "is_admin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return s.addColumnIfMissing(ctx, "source_exclusions", "paused", "INTEGER NOT NULL DEFAULT 0")
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

func (s *Store) UserFromIdentity(ctx context.Context, subject, email, name string) (User, error) {
	scan := func(row *sql.Row) (User, error) {
		var u User
		err := row.Scan(&u.ID, &u.Name, &u.Email, &u.OIDCSubject, asTime(&u.CreatedAt))
		return u, err
	}
	if subject != "" {
		u, err := scan(s.db.QueryRowContext(ctx,
			`SELECT id, name, email, COALESCE(oidc_subject,''), created_at FROM users WHERE oidc_subject = ?`, subject))
		if err == nil {
			return u, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return User{}, err
		}
	}
	if email != "" {
		u, err := scan(s.db.QueryRowContext(ctx,
			`SELECT id, name, email, COALESCE(oidc_subject,''), created_at FROM users WHERE email = ?`, email))
		if err == nil {
			if subject != "" && u.OIDCSubject != subject {
				if _, err := s.db.ExecContext(ctx, `UPDATE users SET oidc_subject = ? WHERE id = ?`, subject, u.ID); err != nil {
					return User{}, err
				}
				u.OIDCSubject = subject
			}
			return u, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return User{}, err
		}
	}
	if email == "" {
		if subject == "" {
			return User{}, fmt.Errorf("cannot resolve user: no subject or email")
		}
		email = subject + "@oidc.local"
	}
	if name == "" {
		name = email
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, email, oidc_subject, created_at) VALUES (?, ?, NULLIF(?, ''), ?)`,
		name, email, subject, now.Unix())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Name: name, Email: email, OIDCSubject: subject, CreatedAt: now}, nil
}

func (s *Store) ListSearchesForUser(ctx context.Context, userID int64) ([]Search, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, source, query, params, min_price, max_price,
		        interval_seconds, enabled, created_at, last_run_at, image, exclude, exclude_categories
		 FROM searches WHERE user_id = ? ORDER BY id`, userID)
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

func (s *Store) ListSearches(ctx context.Context, enabledOnly bool) ([]Search, error) {
	q := `SELECT id, user_id, source, query, params, min_price, max_price,
	             interval_seconds, enabled, created_at, last_run_at, image, exclude, exclude_categories
	      FROM searches`
	if enabledOnly {
		q += ` WHERE enabled = 1
		       AND NOT EXISTS (SELECT 1 FROM source_exclusions x
		                       WHERE x.user_id = searches.user_id
		                         AND x.source = searches.source
		                         AND x.paused = 1)`
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
		        interval_seconds, enabled, created_at, last_run_at, image, exclude, exclude_categories
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
		                       interval_seconds, enabled, created_at, image, exclude, exclude_categories)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		se.UserID, se.Source, se.Query, string(params),
		nullFloat(se.MinPrice), nullFloat(se.MaxPrice),
		interval, boolToInt(se.Enabled), time.Now().Unix(), se.Image,
		se.Exclude, se.ExcludeCategories)
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
		        interval_seconds = ?, enabled = ?, exclude = ?, exclude_categories = ?
		 WHERE id = ?`,
		se.Source, se.Query, string(params), nullFloat(se.MinPrice), nullFloat(se.MaxPrice),
		int64(se.Interval/time.Second), boolToInt(se.Enabled),
		se.Exclude, se.ExcludeCategories, se.ID)
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

type SearchPatch struct {
	Interval          *time.Duration
	MinPrice          *float64
	MaxPrice          *float64
	Exclude           *string
	ExcludeCategories *string
	Params            map[string]string
	ReplaceParams     bool
}

func (p SearchPatch) Empty() bool {
	return p.Interval == nil && p.MinPrice == nil && p.MaxPrice == nil &&
		p.Exclude == nil && p.ExcludeCategories == nil && len(p.Params) == 0 && !p.ReplaceParams
}

func (s *Store) PatchSearches(ctx context.Context, userID int64, ids []int64, p SearchPatch) (int64, error) {
	if len(ids) == 0 || p.Empty() {
		return 0, nil
	}
	owned, err := s.OwnedSearchIDs(ctx, userID, ids)
	if err != nil {
		return 0, err
	}

	var n int64
	for _, id := range owned {
		se, err := s.GetSearch(ctx, id)
		if err != nil {
			continue
		}
		if p.Interval != nil {
			se.Interval = *p.Interval
		}
		if p.MinPrice != nil {
			v := *p.MinPrice
			if v < 0 {
				se.MinPrice = nil
			} else {
				se.MinPrice = &v
			}
		}
		if p.MaxPrice != nil {
			v := *p.MaxPrice
			if v < 0 {
				se.MaxPrice = nil
			} else {
				se.MaxPrice = &v
			}
		}
		if p.Exclude != nil {
			se.Exclude = *p.Exclude
		}
		if p.ExcludeCategories != nil {
			se.ExcludeCategories = *p.ExcludeCategories
		}
		if p.ReplaceParams {
			se.Params = p.Params
		} else if len(p.Params) > 0 {
			merged := map[string]string{}
			for k, v := range se.Params {
				merged[k] = v
			}
			for k, v := range p.Params {
				if v == "" {
					delete(merged, k)
				} else {
					merged[k] = v
				}
			}
			se.Params = merged
		}
		if err := s.UpdateSearch(ctx, se); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) RepopulateSearch(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM listings WHERE search_id = ?`, id)
	if err != nil {
		return 0, err
	}
	removed, _ := res.RowsAffected()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE searches SET last_run_at = NULL WHERE id = ?`, id); err != nil {
		return removed, err
	}
	return removed, nil
}

func (s *Store) DeleteSearches(ctx context.Context, userID int64, ids []int64) (int64, error) {
	return s.bulkSearch(ctx, `DELETE FROM searches`, userID, ids)
}

func (s *Store) SetSearchesEnabled(ctx context.Context, userID int64, ids []int64, enabled bool) (int64, error) {
	return s.bulkSearch(ctx, `UPDATE searches SET enabled = `+strconv.Itoa(boolToInt(enabled)), userID, ids)
}

func (s *Store) OwnedSearchIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := bulkArgs(userID, ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM searches WHERE user_id = ? AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) bulkSearch(ctx context.Context, prefix string, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders, args := bulkArgs(userID, ids)
	res, err := s.db.ExecContext(ctx, prefix+` WHERE user_id = ? AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func bulkArgs(userID int64, ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	return strings.Join(placeholders, ","), args
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
		`INSERT INTO listings (search_id, source, external_id, title, price, currency, url, image_url, sale_type, extra, first_seen, listed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(search_id, external_id) DO NOTHING`,
		searchID, src, l.ExternalID, l.Title, l.Price, l.Currency, l.URL, l.ImageURL, l.SaleType, string(extra), now.Unix(), nullUnix(l.ListedAt))
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
		ImageURL: l.ImageURL, SaleType: l.SaleType, Extra: l.Extra, FirstSeen: now, ListedAt: l.ListedAt,
	}, true, nil
}

func (s *Store) MarkNotified(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE listings SET notified = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateListingMarket(ctx context.Context, id int64, price float64, saleType string, extra map[string]string) error {
	encoded, err := json.Marshal(orEmpty(extra))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE listings SET price = ?, sale_type = ?, extra = ? WHERE id = ?`, price, saleType, string(encoded), id)
	return err
}

func (s *Store) ListingsPage(ctx context.Context, userID int64, filter string, sources []string, limit, offset int) ([]Listing, int, error) {
	where := `se.user_id = ?
	   AND l.id IN (
	     SELECT MIN(l2.id) FROM listings l2
	     JOIN searches se2 ON se2.id = l2.search_id
	     WHERE se2.user_id = ?
	     GROUP BY l2.source, l2.external_id
	   )`
	args := []any{userID, userID}

	for _, term := range s.excludedFeedTerms(ctx, userID) {
		where += ` AND l.title NOT LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(term)+"%")
	}
	for src, cats := range s.excludedFeedCategories(ctx, userID) {
		for _, cat := range cats {
			where += ` AND COALESCE(NOT (l.source = ? AND (
				COALESCE(json_extract(l.extra, '$.category'), '') = ?
				OR ',' || COALESCE(json_extract(l.extra, '$.categories'), '') || ',' LIKE ?
			)), 1)`
			args = append(args, src, cat, "%,"+escapeLike(cat)+",%")
		}
	}
	if filter != "" {
		clause := `(l.title LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(filter)+"%")
		for _, src := range sources {
			clause += ` OR l.source = ?`
			args = append(args, src)
		}
		clause += `)`
		where += ` AND ` + clause
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM listings l JOIN searches se ON se.id = l.search_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.search_id, l.source, l.external_id, l.title, l.price, l.currency, l.url, l.image_url, l.sale_type, l.extra, l.first_seen, l.listed_at, l.notified
		 FROM listings l
		 JOIN searches se ON se.id = l.search_id
		 WHERE `+where+`
		 ORDER BY l.first_seen DESC, l.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	listings, err := scanListings(rows)
	return listings, total, err
}

func (s *Store) excludedFeedCategories(ctx context.Context, userID int64) map[string][]string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, exclude_categories FROM source_exclusions
		 WHERE user_id = ? AND exclude_categories != ''
		 UNION
		 SELECT source, exclude_categories FROM searches
		 WHERE user_id = ? AND exclude_categories != ''`, userID, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := map[string][]string{}
	seen := map[string]bool{}
	total := 0
	for rows.Next() {
		var src, raw string
		if err := rows.Scan(&src, &raw); err != nil {
			return out
		}
		for _, f := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == ' '
		}) {
			cat := strings.TrimSpace(f)
			key := src + "/" + cat
			if cat == "" || seen[key] {
				continue
			}
			seen[key] = true
			out[src] = append(out[src], cat)
			if total++; total >= 60 {
				return out
			}
		}
	}
	return out
}

func (s *Store) excludedFeedTerms(ctx context.Context, userID int64) []string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT exclude FROM source_exclusions WHERE user_id = ? AND exclude != ''
		 UNION
		 SELECT exclude FROM searches WHERE user_id = ? AND exclude != ''`, userID, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return out
		}
		for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' }) {
			t := strings.TrimSpace(f)
			if t == "" || seen[strings.ToLower(t)] {
				continue
			}
			seen[strings.ToLower(t)] = true
			out = append(out, t)
			if len(out) >= 40 {
				return out
			}
		}
	}
	return out
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
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
		&minP, &maxP, &interval, asBool(&se.Enabled), asTime(&se.CreatedAt), &lastRun, &se.Image, &se.Exclude, &se.ExcludeCategories); err != nil {
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
			&l.Price, &l.Currency, &l.URL, &l.ImageURL, &l.SaleType, &extra, asTime(&l.FirstSeen), &listedAt, asBool(&l.Notified)); err != nil {
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
