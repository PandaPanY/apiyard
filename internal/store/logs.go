package store

import (
	"context"
	"database/sql"
	"strings"
)

// Log 是一条操作动态，对应 YApi 的 log 集合。
type Log struct {
	ID       int64
	UID      int64
	Username string
	Type     string // user / group / interface / project / other / interface_col
	TypeID   int64  // 按 Type 指向分组、项目等
	Content  string // HTML 片段
	Data     *string
	AddTime  int64
}

// CreateLog 写入一条操作动态。
func (s *Store) CreateLog(ctx context.Context, l Log) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO logs (uid, username, type, typeid, content, data, add_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.UID, l.Username, l.Type, l.TypeID, l.Content, nullString(l.Data), Now())
	return err
}

// logOrder：新的在前。YApi 只按秒级时间排序（sort({add_time: -1})），同一秒内的记录 MongoDB 按索引里的插入顺序返回，
// 所以这里同一秒内按 ID 升序。迁移的数据里同一秒的动态很常见（例如批量导入），单个对象的动态顺序与 YApi 一致。
// 分组动态是 $or 查询，同一秒内分组和各项目的动态谁先谁后取决于 MongoDB 的执行计划，这里不去复现。
const logOrder = " ORDER BY add_time DESC, id ASC"

// LogFilter 是查询某个对象动态的条件。
type LogFilter struct {
	Type        string
	TypeID      int64
	InterfaceID *int64 // 只看某个接口的改动（YApi 的 selectValue）
}

func (f LogFilter) where() (string, []any) {
	where, args := "type = ? AND typeid = ?", []any{f.Type, f.TypeID}
	if f.InterfaceID != nil {
		where += " AND json_extract(data, '$.interface_id') = ?"
		args = append(args, *f.InterfaceID)
	}
	return where, args
}

// ListLogs 分页列出某个对象的动态，page 从 1 开始。
func (s *Store) ListLogs(ctx context.Context, f LogFilter, page, limit int) ([]*Log, error) {
	where, args := f.where()
	return s.queryLogs(ctx, "SELECT id, uid, username, type, typeid, content, data, add_time FROM logs WHERE "+where+logOrder+" LIMIT ? OFFSET ?",
		append(args, limit, (page-1)*limit)...)
}

// CountLogs 返回某个对象的动态总数。
func (s *Store) CountLogs(ctx context.Context, f LogFilter) (int, error) {
	where, args := f.where()
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM logs WHERE "+where, args...).Scan(&n)
	return n, err
}

// groupLogsWhere：分组本身的动态，加上分组内各项目的动态。
func groupLogsWhere(groupID int64, projectIDs []int64) (string, []any) {
	args := []any{groupID}
	where := "(type = 'group' AND typeid = ?)"
	if len(projectIDs) > 0 {
		where += " OR (type = 'project' AND typeid IN (" + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + "))"
		for _, id := range projectIDs {
			args = append(args, id)
		}
	}
	return where, args
}

// ListGroupLogs 分页列出分组及其项目的动态。
func (s *Store) ListGroupLogs(ctx context.Context, groupID int64, projectIDs []int64, page, limit int) ([]*Log, error) {
	where, args := groupLogsWhere(groupID, projectIDs)
	return s.queryLogs(ctx, "SELECT id, uid, username, type, typeid, content, data, add_time FROM logs WHERE "+where+logOrder+" LIMIT ? OFFSET ?",
		append(args, limit, (page-1)*limit)...)
}

// CountGroupLogs 返回分组及其项目的动态总数。
func (s *Store) CountGroupLogs(ctx context.Context, groupID int64, projectIDs []int64) (int, error) {
	where, args := groupLogsWhere(groupID, projectIDs)
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM logs WHERE "+where, args...).Scan(&n)
	return n, err
}

func (s *Store) queryLogs(ctx context.Context, query string, args ...any) ([]*Log, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Log
	for rows.Next() {
		var l Log
		var data sql.NullString
		if err := rows.Scan(&l.ID, &l.UID, &l.Username, &l.Type, &l.TypeID, &l.Content, &data, &l.AddTime); err != nil {
			return nil, err
		}
		l.Data = stringPtr(data)
		out = append(out, &l)
	}
	return out, rows.Err()
}
