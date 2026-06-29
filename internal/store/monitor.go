package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
)

func (s *Store) AddMonitor(ctx context.Context, m MonitoredItem) (int64, error) {
	interval := int64(m.Interval / time.Second)
	if interval <= 0 {
		interval = 3600
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO monitored_items
		   (user_id, source, external_id, url, title, image_url, currency, sale_type, last_price, status, interval_seconds, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		 ON CONFLICT(user_id, source, external_id) DO NOTHING`,
		m.UserID, m.Source, m.ExternalID, m.URL, m.Title, m.ImageURL, m.Currency, m.SaleType,
		m.LastPrice, statusOrActive(m.Status), interval, now)
	if err != nil {
		return 0, err
	}
	if id, _ := res.LastInsertId(); id > 0 {
		if affected, _ := res.RowsAffected(); affected > 0 {
			_, err = s.db.ExecContext(ctx,
				`INSERT INTO monitor_prices (monitor_id, price, status, observed_at) VALUES (?, ?, ?, ?)`,
				id, m.LastPrice, statusOrActive(m.Status), now)
			return id, err
		}
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id FROM monitored_items WHERE user_id = ? AND source = ? AND external_id = ?`,
		m.UserID, m.Source, m.ExternalID)
	var id int64
	err = row.Scan(&id)
	return id, err
}

func (s *Store) ListMonitors(ctx context.Context, userID int64) ([]MonitoredItem, error) {
	rows, err := s.db.QueryContext(ctx, monitorSelect+` WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func (s *Store) DueMonitors(ctx context.Context) ([]MonitoredItem, error) {
	rows, err := s.db.QueryContext(ctx, monitorSelect+` WHERE enabled = 1 AND status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func (s *Store) GetMonitor(ctx context.Context, id int64) (MonitoredItem, error) {
	row := s.db.QueryRowContext(ctx, monitorSelect+` WHERE id = ?`, id)
	return scanMonitor(row)
}

func (s *Store) DeleteMonitor(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitored_items WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateMonitorInterval(ctx context.Context, id int64, interval time.Duration) error {
	seconds := int64(interval / time.Second)
	if seconds <= 0 {
		seconds = 3600
	}
	_, err := s.db.ExecContext(ctx, `UPDATE monitored_items SET interval_seconds = ? WHERE id = ?`, seconds, id)
	return err
}

func (s *Store) RecordMonitorCheck(ctx context.Context, id int64, snap source.ItemSnapshot, observedAt time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO monitor_prices (monitor_id, price, status, observed_at) VALUES (?, ?, ?, ?)`,
		id, snap.Price, statusOrActive(snap.Status), observedAt.Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE monitored_items
		    SET last_price = ?, status = ?, last_checked_at = ?,
		        title = CASE WHEN ? != '' THEN ? ELSE title END,
		        image_url = CASE WHEN ? != '' THEN ? ELSE image_url END,
		        currency = CASE WHEN ? != '' THEN ? ELSE currency END
		  WHERE id = ?`,
		snap.Price, statusOrActive(snap.Status), observedAt.Unix(),
		snap.Title, snap.Title, snap.ImageURL, snap.ImageURL, snap.Currency, snap.Currency, id)
	return err
}

func (s *Store) TouchMonitorCheck(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE monitored_items SET last_checked_at = ? WHERE id = ?`, at.Unix(), id)
	return err
}

func (s *Store) PriceHistory(ctx context.Context, monitorID int64) ([]PricePoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT price, status, observed_at FROM monitor_prices WHERE monitor_id = ? ORDER BY observed_at`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.Price, &p.Status, asTime(&p.ObservedAt)); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const monitorSelect = `SELECT id, user_id, source, external_id, url, title, image_url, currency, sale_type,
	last_price, status, interval_seconds, enabled, created_at, last_checked_at FROM monitored_items`

func scanMonitors(rows *sql.Rows) ([]MonitoredItem, error) {
	var out []MonitoredItem
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMonitor(sc interface{ Scan(...any) error }) (MonitoredItem, error) {
	var (
		m       MonitoredItem
		seconds int64
		checked sql.NullInt64
	)
	if err := sc.Scan(&m.ID, &m.UserID, &m.Source, &m.ExternalID, &m.URL, &m.Title, &m.ImageURL,
		&m.Currency, &m.SaleType, &m.LastPrice, &m.Status, &seconds, asBool(&m.Enabled),
		asTime(&m.CreatedAt), &checked); err != nil {
		return MonitoredItem{}, err
	}
	m.Interval = time.Duration(seconds) * time.Second
	if checked.Valid {
		t := time.Unix(checked.Int64, 0)
		m.LastCheckedAt = &t
	}
	return m, nil
}

func statusOrActive(s string) string {
	if s == "" {
		return "active"
	}
	return s
}
