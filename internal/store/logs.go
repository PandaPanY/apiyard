package store

import "context"

// Log 是一条操作动态，对应 YApi 的 log 集合。
type Log struct {
	UID      int64
	Username string
	Type     string // user / group / interface / project / other / interface_col
	TypeID   int64  // 按 Type 指向分组、项目等
	Content  string // HTML 片段
	Data     *string
}

// CreateLog 写入一条操作动态。
func (s *Store) CreateLog(ctx context.Context, l Log) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO logs (uid, username, type, typeid, content, data, add_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.UID, l.Username, l.Type, l.TypeID, l.Content, nullString(l.Data), Now())
	return err
}
