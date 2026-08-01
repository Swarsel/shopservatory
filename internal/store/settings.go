package store

import (
	"context"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
)

type Settings struct {
	Currency        string
	SearchInterval  time.Duration
	MonitorInterval time.Duration
	TelegramChatID  string
}

func (s *Store) UserSettings(ctx context.Context, userID int64) (Settings, error) {
	var (
		currency          string
		searchS, monitorS int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT currency, search_interval_seconds, monitor_interval_seconds FROM users WHERE id = ?`, userID).
		Scan(&currency, &searchS, &monitorS)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		Currency:        currency,
		SearchInterval:  time.Duration(searchS) * time.Second,
		MonitorInterval: time.Duration(monitorS) * time.Second,
		TelegramChatID:  s.userTelegramChatID(ctx, userID),
	}, nil
}

func (s *Store) UserCurrency(ctx context.Context, userID int64) string {
	var c string
	_ = s.db.QueryRowContext(ctx, `SELECT currency FROM users WHERE id = ?`, userID).Scan(&c)
	return c
}

func (s *Store) UpdateUserSettings(ctx context.Context, userID int64, currency string, searchInterval, monitorInterval time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET currency = ?, search_interval_seconds = ?, monitor_interval_seconds = ? WHERE id = ?`,
		strings.ToUpper(strings.TrimSpace(currency)),
		int64(searchInterval/time.Second), int64(monitorInterval/time.Second), userID)
	return err
}

func (s *Store) userTelegramChatID(ctx context.Context, userID int64) string {
	var cfg string
	err := s.db.QueryRowContext(ctx,
		`SELECT config FROM notification_targets WHERE user_id = ? AND kind = 'telegram' AND enabled = 1 ORDER BY id LIMIT 1`, userID).
		Scan(&cfg)
	if err != nil {
		return ""
	}
	return decodeMap(cfg)["chat_id"]
}

func (s *Store) SetTelegramChatID(ctx context.Context, userID int64, chatID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM notification_targets WHERE user_id = ? AND kind = 'telegram'`, userID); err != nil {
		return err
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	_, err := s.CreateTarget(ctx, NotificationTarget{
		UserID: userID, Kind: "telegram",
		Config: map[string]string{"chat_id": chatID}, Enabled: true,
	})
	return err
}

type SourceExclusion struct {
	Source            string
	Exclude           string
	ExcludeCategories string
	Paused            bool
}

func (s *Store) SourceExclusions(ctx context.Context, userID int64) (map[string]SourceExclusion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, exclude, exclude_categories, paused FROM source_exclusions WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SourceExclusion{}
	for rows.Next() {
		var e SourceExclusion
		if err := rows.Scan(&e.Source, &e.Exclude, &e.ExcludeCategories, &e.Paused); err != nil {
			return nil, err
		}
		out[e.Source] = e
	}
	return out, rows.Err()
}

func (s *Store) SetSourceExclusion(ctx context.Context, userID int64, e SourceExclusion) error {
	if strings.TrimSpace(e.Exclude) == "" && strings.TrimSpace(e.ExcludeCategories) == "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE source_exclusions SET exclude = '', exclude_categories = ''
			 WHERE user_id = ? AND source = ?`, userID, e.Source)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM source_exclusions WHERE user_id = ? AND source = ? AND paused = 0`, userID, e.Source)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO source_exclusions (user_id, source, exclude, exclude_categories)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, source) DO UPDATE SET exclude = excluded.exclude,
		                                            exclude_categories = excluded.exclude_categories`,
		userID, e.Source, strings.TrimSpace(e.Exclude), strings.TrimSpace(e.ExcludeCategories))
	return err
}

func (s *Store) SetSourcePaused(ctx context.Context, userID int64, src string, paused bool) error {
	if !paused {
		_, err := s.db.ExecContext(ctx,
			`UPDATE source_exclusions SET paused = 0 WHERE user_id = ? AND source = ?`, userID, src)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM source_exclusions
			 WHERE user_id = ? AND source = ? AND exclude = '' AND exclude_categories = ''`, userID, src)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO source_exclusions (user_id, source, paused) VALUES (?, ?, 1)
		 ON CONFLICT(user_id, source) DO UPDATE SET paused = 1`, userID, src)
	return err
}

func (s *Store) PausedSources(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source FROM source_exclusions WHERE user_id = ? AND paused = 1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		out[src] = true
	}
	return out, rows.Err()
}

func mergeLists(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + ", " + b
	}
}

func (s *Store) EffectiveSpec(ctx context.Context, se Search) source.SearchSpec {
	spec := se.Spec()
	var e SourceExclusion
	err := s.db.QueryRowContext(ctx,
		`SELECT exclude, exclude_categories FROM source_exclusions WHERE user_id = ? AND source = ?`,
		se.UserID, se.Source).Scan(&e.Exclude, &e.ExcludeCategories)
	if err != nil {
		return spec
	}
	spec.Exclude = mergeLists(e.Exclude, spec.Exclude)
	spec.ExcludeCategories = mergeLists(e.ExcludeCategories, spec.ExcludeCategories)
	return spec
}
