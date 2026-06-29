package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

func (s *Store) SeedUser(ctx context.Context, name, email, passwordHash string) (User, bool, error) {
	existing, err := s.UserByEmail(ctx, email)
	switch {
	case err == nil:
		if _, uerr := s.db.ExecContext(ctx,
			`UPDATE users SET name = ?, password_hash = ? WHERE id = ?`,
			name, passwordHash, existing.ID); uerr != nil {
			return User{}, false, uerr
		}
		u, gerr := s.UserByEmail(ctx, email)
		return u, false, gerr
	case errors.Is(err, sql.ErrNoRows):
		if _, ierr := s.db.ExecContext(ctx,
			`INSERT INTO users (name, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
			name, email, passwordHash, time.Now().Unix()); ierr != nil {
			return User{}, false, ierr
		}
		u, gerr := s.UserByEmail(ctx, email)
		return u, true, gerr
	default:
		return User{}, false, err
	}
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, COALESCE(oidc_subject,''), password_hash, created_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.OIDCSubject, &u.PasswordHash, asTime(&u.CreatedAt))
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now.Unix(), now.Add(ttl).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) SessionUserID(ctx context.Context, token string) (int64, bool) {
	if token == "" {
		return 0, false
	}
	var (
		userID  int64
		expires int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token = ?`, token).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return 0, false
	}
	if time.Now().Unix() >= expires {
		_ = s.DeleteSession(ctx, token)
		return 0, false
	}
	return userID, true
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}
