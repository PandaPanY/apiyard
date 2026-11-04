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
