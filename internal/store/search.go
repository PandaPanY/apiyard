package store

import (
	"context"
	"strings"
)

// SearchGroups 按分组名不区分大小写地模糊匹配，最多 10 个（与 YApi 一样包含个人空间）。
func (s *Store) SearchGroups(ctx context.Context, keyword string) ([]*Group, error) {
	return s.queryGroups(ctx,
		"SELECT "+groupColumns+" FROM groups WHERE instr(lower(group_name), ?) > 0 ORDER BY id LIMIT 10", strings.ToLower(keyword))
}

// InterfaceHit 是接口搜索结果里用到的字段。
type InterfaceHit struct {
	ID        int64
	UID       int64
	Title     string
	ProjectID int64
	AddTime   int64
	UpTime    int64
}

// SearchInterfaces 按接口名或路径不区分大小写地模糊匹配，最多 10 个。
func (s *Store) SearchInterfaces(ctx context.Context, keyword string) ([]InterfaceHit, error) {
	k := strings.ToLower(keyword)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, uid, title, project_id, add_time, up_time FROM interfaces
		  WHERE instr(lower(title), ?) > 0 OR instr(lower(path), ?) > 0 ORDER BY id LIMIT 10`, k, k)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []InterfaceHit
	for rows.Next() {
		var h InterfaceHit
		if err := rows.Scan(&h.ID, &h.UID, &h.Title, &h.ProjectID, &h.AddTime, &h.UpTime); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
