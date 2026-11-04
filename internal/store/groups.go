package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Group 对应 YApi 的 group 集合；Type 为 private 时是用户的个人空间。
type Group struct {
	ID           int64
	UID          int64 // 创建者
	Name         string
	Desc         *string
	Type         string
	CustomField1 *string // JSON 文本；nil 表示 null
	AddTime      int64
	UpTime       int64
}

// Member 是分组（或项目）的成员。Username、Email 是 YApi 保存的用户信息副本。
type Member struct {
	UID      int64
	Role     string // owner / dev / guest
	Username *string
	Email    *string
	SubID    string // 成员子文档的 ObjectId
}

const groupColumns = "id, uid, group_name, group_desc, type, custom_field1, add_time, up_time"

func scanGroup(row interface{ Scan(...any) error }) (*Group, error) {
	var g Group
	var desc, cf sql.NullString
	err := row.Scan(&g.ID, &g.UID, &g.Name, &desc, &g.Type, &cf, &g.AddTime, &g.UpTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.Desc, g.CustomField1 = stringPtr(desc), stringPtr(cf)
	return &g, nil
}

// CreateGroup 新建分组及其初始成员，回填分组 ID 和成员的 SubID。
func (s *Store) CreateGroup(ctx context.Context, g *Group, members []Member) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO groups (uid, group_name, group_desc, type, add_time, up_time) VALUES (?, ?, ?, ?, ?, ?)`,
			g.UID, g.Name, nullString(g.Desc), g.Type, g.AddTime, g.UpTime)
		if err != nil {
			return err
		}
		if g.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		// custom_field1 取列默认值，读回来保证与数据库一致
		var cf sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT custom_field1 FROM groups WHERE id = ?", g.ID).Scan(&cf); err != nil {
			return err
		}
		g.CustomField1 = stringPtr(cf)
		return insertGroupMembers(ctx, tx, g.ID, members)
	})
}

// GroupByID 按 ID 查询分组。
func (s *Store) GroupByID(ctx context.Context, id int64) (*Group, error) {
	return scanGroup(s.db.QueryRowContext(ctx, "SELECT "+groupColumns+" FROM groups WHERE id = ?", id))
}

// PrivateGroupOf 返回用户的个人空间。
func (s *Store) PrivateGroupOf(ctx context.Context, uid int64) (*Group, error) {
	return scanGroup(s.db.QueryRowContext(ctx,
		"SELECT "+groupColumns+" FROM groups WHERE uid = ? AND type = 'private' ORDER BY id LIMIT 1", uid))
}

// GroupNameExists 判断分组名是否已被使用（YApi 要求全站唯一）。
func (s *Store) GroupNameExists(ctx context.Context, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM groups WHERE group_name = ?", name).Scan(&n)
	return n > 0, err
}

// PublicGroups 按 ID 升序列出全部公共分组。
func (s *Store) PublicGroups(ctx context.Context) ([]*Group, error) {
	return s.queryGroups(ctx, "SELECT "+groupColumns+" FROM groups WHERE type = 'public' ORDER BY id")
}

// MemberGroups 列出用户创建或参与的公共分组，按 ID 升序。
func (s *Store) MemberGroups(ctx context.Context, uid int64) ([]*Group, error) {
	return s.queryGroups(ctx,
		`SELECT `+groupColumns+` FROM groups WHERE type = 'public'
		   AND (uid = ? OR id IN (SELECT group_id FROM group_members WHERE uid = ?))
		 ORDER BY id`, uid, uid)
}

// VisibleProjectGroupIDs 按项目 ID 顺序返回用户能看到的项目所属分组（可能重复）：
// 公开项目，以及用户创建或参与的私有项目。对应 YApi project.getAuthList。
func (s *Store) VisibleProjectGroupIDs(ctx context.Context, uid int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id FROM projects
		  WHERE project_type = 'public' OR uid = ?
		     OR id IN (SELECT project_id FROM project_members WHERE uid = ?)
		  ORDER BY id`, uid, uid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PublicGroupsByIDs 按 ID 升序返回给定 ID 中的公共分组。
func (s *Store) PublicGroupsByIDs(ctx context.Context, ids []int64) ([]*Group, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	return s.queryGroups(ctx,
		"SELECT "+groupColumns+" FROM groups WHERE type = 'public' AND id IN ("+placeholders+") ORDER BY id", args...)
}

func (s *Store) queryGroups(ctx context.Context, query string, args ...any) ([]*Group, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpdateGroup 修改分组名称、描述和自定义字段，返回匹配的行数。
// customField1 为 nil 时写入 null：YApi 的修改接口总是覆盖这个字段。
func (s *Store) UpdateGroup(ctx context.Context, id int64, name string, desc, customField1 *string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE groups SET group_name = ?, group_desc = ?, custom_field1 = ?, up_time = ? WHERE id = ?",
		name, nullString(desc), nullString(customField1), Now(), id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteGroup 删除分组；项目、分类、接口、成员等由外键级联删除。返回删除的行数。
func (s *Store) DeleteGroup(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM groups WHERE id = ?", id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GroupMembers 按加入顺序列出分组成员。
func (s *Store) GroupMembers(ctx context.Context, groupID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT uid, role, username, email, sub_id FROM group_members WHERE group_id = ? ORDER BY position", groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Member
	for rows.Next() {
		var m Member
		var username, email sql.NullString
		if err := rows.Scan(&m.UID, &m.Role, &username, &email, &m.SubID); err != nil {
			return nil, err
		}
		m.Username, m.Email = stringPtr(username), stringPtr(email)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GroupMember 返回分组中的某个成员。
func (s *Store) GroupMember(ctx context.Context, groupID, uid int64) (*Member, error) {
	var m Member
	var username, email sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT uid, role, username, email, sub_id FROM group_members WHERE group_id = ? AND uid = ?", groupID, uid).
		Scan(&m.UID, &m.Role, &username, &email, &m.SubID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Username, m.Email = stringPtr(username), stringPtr(email)
	return &m, nil
}

// AddGroupMembers 把成员追加到分组末尾，回填 SubID；返回分组是否存在（对应 YApi 更新结果中的 n）。
func (s *Store) AddGroupMembers(ctx context.Context, groupID int64, members []Member) (bool, error) {
	var exists bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM groups WHERE id = ?", groupID).Scan(&n); err != nil {
			return err
		}
		if exists = n > 0; !exists {
			return nil
		}
		return insertGroupMembers(ctx, tx, groupID, members)
	})
	return exists, err
}

func insertGroupMembers(ctx context.Context, tx *sql.Tx, groupID int64, members []Member) error {
	var next int
	if err := tx.QueryRowContext(ctx,
		"SELECT coalesce(max(position) + 1, 0) FROM group_members WHERE group_id = ?", groupID).Scan(&next); err != nil {
		return err
	}
	for i := range members {
		members[i].SubID = NewObjectID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO group_members (group_id, uid, role, username, email, sub_id, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			groupID, members[i].UID, members[i].Role, nullString(members[i].Username), nullString(members[i].Email),
			members[i].SubID, next+i); err != nil {
			return err
		}
	}
	return nil
}

// ChangeGroupMemberRole 修改成员角色，返回匹配的行数。
func (s *Store) ChangeGroupMemberRole(ctx context.Context, groupID, uid int64, role string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE group_members SET role = ? WHERE group_id = ? AND uid = ?", role, groupID, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RemoveGroupMember 移除成员，返回删除的行数。
func (s *Store) RemoveGroupMember(ctx context.Context, groupID, uid int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ? AND uid = ?", groupID, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
