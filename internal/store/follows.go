package store

import (
	"context"
	"database/sql"
	"errors"
)

// Follow 是用户关注的项目，ProjectName、Icon、Color 是 YApi 保存的项目信息副本。
type Follow struct {
	ID          int64
	UID         int64
	ProjectID   int64 // YApi 字段名 projectid
	ProjectName string
	Icon        *string
	Color       *string
}

// CreateFollow 新建关注并回填 ID。
func (s *Store) CreateFollow(ctx context.Context, f *Follow) error {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO follows (uid, project_id, projectname, icon, color) VALUES (?, ?, ?, ?, ?)",
		f.UID, f.ProjectID, f.ProjectName, nullString(f.Icon), nullString(f.Color))
	if err != nil {
		return err
	}
	f.ID, err = res.LastInsertId()
	return err
}

// FollowsOf 按关注先后列出用户关注的项目。
func (s *Store) FollowsOf(ctx context.Context, uid int64) ([]*Follow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, uid, project_id, projectname, icon, color FROM follows WHERE uid = ? ORDER BY id", uid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Follow
	for rows.Next() {
		var f Follow
		var icon, color sql.NullString
		if err := rows.Scan(&f.ID, &f.UID, &f.ProjectID, &f.ProjectName, &icon, &color); err != nil {
			return nil, err
		}
		f.Icon, f.Color = stringPtr(icon), stringPtr(color)
		out = append(out, &f)
	}
	return out, rows.Err()
}

// IsFollowing 判断用户是否已关注项目。
func (s *Store) IsFollowing(ctx context.Context, uid, projectID int64) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM follows WHERE uid = ? AND project_id = ? LIMIT 1", uid, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// DeleteFollow 取消关注，返回删除的行数。
func (s *Store) DeleteFollow(ctx context.Context, uid, projectID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM follows WHERE uid = ? AND project_id = ?", uid, projectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateFollowStyle 同步项目的图标和颜色到所有关注记录。
// YApi 只更新当前用户自己的关注记录，其他人看到的仍是旧图标，这里全部同步。
func (s *Store) UpdateFollowStyle(ctx context.Context, projectID int64, icon, color *string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE follows SET icon = ?, color = ? WHERE project_id = ?",
		nullString(icon), nullString(color), projectID)
	return err
}
