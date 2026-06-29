package store

import (
	"context"
	"strings"
	"time"
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
