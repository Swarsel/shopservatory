package store

import (
	"context"
	"strings"
	"time"
)

func (s *Store) IsAdmin(ctx context.Context, userID int64) bool {
	var v int64
	_ = s.db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&v)
	return v != 0
}

func (s *Store) SetAdmin(ctx context.Context, userID int64, admin bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, boolToInt(admin), userID)
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n)
	return n, err
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, COALESCE(oidc_subject,''), password_hash, is_admin, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.OIDCSubject, &u.PasswordHash, &u.IsAdmin, asTime(&u.CreatedAt))
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, email, COALESCE(oidc_subject,''), password_hash, is_admin, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.OIDCSubject, &u.PasswordHash, &u.IsAdmin, asTime(&u.CreatedAt)); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, name, email, passwordHash string, isAdmin bool) (User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
		name, strings.ToLower(strings.TrimSpace(email)), passwordHash, boolToInt(isAdmin), time.Now().Unix())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetUser(ctx, id)
}

func (s *Store) UpdateUser(ctx context.Context, id int64, name, email string, isAdmin bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET name = ?, email = ?, is_admin = ? WHERE id = ?`,
		name, strings.ToLower(strings.TrimSpace(email)), boolToInt(isAdmin), id)
	return err
}

func (s *Store) SetUserPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}
