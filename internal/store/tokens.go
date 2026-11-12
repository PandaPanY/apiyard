package store

import (
	"context"
	"database/sql"
	"errors"
)

// ProjectToken 返回项目的原始 token；还没有生成过时返回 ErrNotFound。
func (s *Store) ProjectToken(ctx context.Context, projectID int64) (string, error) {
	var token string
	err := s.db.QueryRowContext(ctx, "SELECT token FROM tokens WHERE project_id = ?", projectID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return token, err
}

// SetProjectToken 写入或替换项目的原始 token。
func (s *Store) SetProjectToken(ctx context.Context, projectID int64, token string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO tokens (project_id, token) VALUES (?, ?) ON CONFLICT (project_id) DO UPDATE SET token = excluded.token",
			projectID, token)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM project_credentials WHERE project_id=?", projectID)
		return err
	})
}

// ProjectIDByToken 按原始 token 查找项目；找不到时返回 ErrNotFound。
func (s *Store) ProjectIDByToken(ctx context.Context, token string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT project_id FROM tokens WHERE token = ?", token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
