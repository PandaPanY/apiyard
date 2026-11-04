package store

import (
	"context"
	"database/sql"
	"errors"
)

// Avatar 是用户上传的头像。
type Avatar struct {
	Image []byte
	Type  string // MIME 类型
}

// SaveAvatar 保存用户头像，已有则覆盖；返回是否为新建。
func (s *Store) SaveAvatar(ctx context.Context, uid int64, a Avatar) (created bool, err error) {
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM avatars WHERE uid = ?", uid).Scan(&n); err != nil {
			return err
		}
		created = n == 0
		_, err := tx.ExecContext(ctx,
			"INSERT INTO avatars (uid, image, type) VALUES (?, ?, ?) ON CONFLICT (uid) DO UPDATE SET image = excluded.image, type = excluded.type",
			uid, a.Image, a.Type)
		return err
	})
	return created, err
}

// AvatarOf 返回用户的头像。
func (s *Store) AvatarOf(ctx context.Context, uid int64) (*Avatar, error) {
	var a Avatar
	err := s.db.QueryRowContext(ctx, "SELECT image, type FROM avatars WHERE uid = ?", uid).Scan(&a.Image, &a.Type)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
