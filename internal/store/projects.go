package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Project 对应 YApi 的 project 集合。可选文本字段为 nil 表示原文档里没有该字段。
type Project struct {
	ID                int64
	UID               int64
	GroupID           int64
	Name              string
	Basepath          *string
	Desc              *string
	ProjectType       string // public / private
	SwitchNotice      bool
	Icon              *string
	Color             *string
	Env               string // JSON 数组
	Tag               string // JSON 数组
	PreScript         *string
	AfterScript       *string
	ProjectMockScript *string // 保留但不执行（D5）
	IsMockOpen        bool
	Strice            bool // YApi 原拼写
	IsJSON5           bool
	AddTime           int64
	UpTime            int64
}

const projectColumns = `id, uid, group_id, name, basepath, description, project_type, switch_notice, icon, color,
	env, tag, pre_script, after_script, project_mock_script, is_mock_open, strice, is_json5, add_time, up_time`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var basepath, desc, icon, color, pre, after, mock sql.NullString
	var notice, mockOpen, strice, json5 int
	err := row.Scan(&p.ID, &p.UID, &p.GroupID, &p.Name, &basepath, &desc, &p.ProjectType, &notice, &icon, &color,
		&p.Env, &p.Tag, &pre, &after, &mock, &mockOpen, &strice, &json5, &p.AddTime, &p.UpTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Basepath, p.Desc, p.Icon, p.Color = stringPtr(basepath), stringPtr(desc), stringPtr(icon), stringPtr(color)
	p.PreScript, p.AfterScript, p.ProjectMockScript = stringPtr(pre), stringPtr(after), stringPtr(mock)
	p.SwitchNotice, p.IsMockOpen, p.Strice, p.IsJSON5 = notice == 1, mockOpen == 1, strice == 1, json5 == 1
	return &p, nil
}

func insertProject(ctx context.Context, tx *sql.Tx, p *Project) error {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO projects (uid, group_id, name, basepath, description, project_type, switch_notice, icon, color,
		                       env, tag, pre_script, after_script, project_mock_script, is_mock_open, strice, is_json5,
		                       add_time, up_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UID, p.GroupID, p.Name, nullString(p.Basepath), nullString(p.Desc), p.ProjectType, boolInt(p.SwitchNotice),
		nullString(p.Icon), nullString(p.Color), p.Env, p.Tag, nullString(p.PreScript), nullString(p.AfterScript),
		nullString(p.ProjectMockScript), boolInt(p.IsMockOpen), boolInt(p.Strice), boolInt(p.IsJSON5), p.AddTime, p.UpTime)
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

// insertProjectMembers 按顺序把成员加到项目末尾，回填 SubID。
func insertProjectMembers(ctx context.Context, tx *sql.Tx, projectID int64, members []ProjectMember) error {
	var next int
	if err := tx.QueryRowContext(ctx,
		"SELECT coalesce(max(position) + 1, 0) FROM project_members WHERE project_id = ?", projectID).Scan(&next); err != nil {
		return err
	}
	for i := range members {
		if members[i].SubID == "" {
			members[i].SubID = NewObjectID()
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO project_members (project_id, uid, role, username, email, email_notice, sub_id, position)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			projectID, members[i].UID, members[i].Role, nullString(members[i].Username), nullString(members[i].Email),
			boolInt(members[i].EmailNotice), members[i].SubID, next+i); err != nil {
			return err
		}
	}
	return nil
}

// CreateProject 新建项目，同时建好默认分类 defaultCat；owner 不为 nil 时把它加为项目组长。回填各自的 ID。
func (s *Store) CreateProject(ctx context.Context, p *Project, defaultCat *Cat, owner *Member) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := insertProject(ctx, tx, p); err != nil {
			return err
		}
		defaultCat.ProjectID = p.ID
		if err := insertCat(ctx, tx, defaultCat); err != nil {
			return err
		}
		if owner == nil {
			return nil
		}
		members := []ProjectMember{{Member: *owner, EmailNotice: true}}
		if err := insertProjectMembers(ctx, tx, p.ID, members); err != nil {
			return err
		}
		owner.SubID = members[0].SubID
		return nil
	})
}

// CopyCat 是复制项目时要新建的分类：按 Name、Desc 新建，并复制源项目中 SourceID 分类下的接口。
type CopyCat struct {
	SourceID int64
	Name     string
	Desc     *string
}

// CopyProject 新建项目 p，按顺序新建 cats 中的分类并复制源项目 sourceID 对应分类下的接口（按标题排序，
// 与 YApi 的 listByInterStatus 一致），再加入 members。复制出的接口归 uid 所有，创建和更新时间为 now。
// 只复制属于源项目的分类，避免借复制读取其他项目的接口。
func (s *Store) CopyProject(ctx context.Context, p *Project, sourceID int64, cats []CopyCat, members []ProjectMember, uid, now int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := insertProject(ctx, tx, p); err != nil {
			return err
		}
		for _, src := range cats {
			cat := &Cat{UID: uid, ProjectID: p.ID, Name: src.Name, Desc: src.Desc, AddTime: now, UpTime: now}
			if err := insertCat(ctx, tx, cat); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO interfaces (uid, edit_uid, project_id, catid, title, path, method, status, type, api_opened,
				   position, description, markdown, query_path, req_query, req_headers, req_params, req_body_form,
				   req_body_type, req_body_is_json_schema, req_body_other, res_body_type, res_body_is_json_schema, res_body,
				   custom_field_value, tag, add_time, up_time, extra)
				 SELECT ?, edit_uid, ?, ?, title, path, method, status, type, api_opened,
				   position, description, markdown, query_path, req_query, req_headers, req_params, req_body_form,
				   req_body_type, req_body_is_json_schema, req_body_other, res_body_type, res_body_is_json_schema, res_body,
				   custom_field_value, tag, ?, ?, extra
				   FROM interfaces WHERE catid = ? AND project_id = ? ORDER BY title, id`,
				uid, p.ID, cat.ID, now, now, src.SourceID, sourceID); err != nil {
				return err
			}
		}
		return insertProjectMembers(ctx, tx, p.ID, members)
	})
}

// ProjectByID 按 ID 查询项目。
func (s *Store) ProjectByID(ctx context.Context, id int64) (*Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE id = ?", id))
}

// ProjectsByGroup 按 ID 倒序列出分组下的项目（YApi 的 sort({_id: -1})）。
func (s *Store) ProjectsByGroup(ctx context.Context, groupID int64) ([]*Project, error) {
	return s.queryProjects(ctx, "SELECT "+projectColumns+" FROM projects WHERE group_id = ? ORDER BY id DESC", groupID)
}

// SearchProjects 按项目名不区分大小写地模糊匹配，最多 10 个。
func (s *Store) SearchProjects(ctx context.Context, keyword string) ([]*Project, error) {
	return s.queryProjects(ctx,
		"SELECT "+projectColumns+" FROM projects WHERE instr(lower(name), ?) > 0 ORDER BY id LIMIT 10", strings.ToLower(keyword))
}

func (s *Store) queryProjects(ctx context.Context, query string, args ...any) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectNameExists 判断分组内是否已有同名项目。
func (s *Store) ProjectNameExists(ctx context.Context, name string, groupID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE name = ? AND group_id = ?", name, groupID).Scan(&n)
	return n > 0, err
}

// ProjectPatch 是项目修改接口可以改的字段，为 nil 的不修改。
type ProjectPatch struct {
	Name, Basepath, Desc, ProjectType         *string
	Icon, Color, Env, Tag                     *string
	PreScript, AfterScript, ProjectMockScript *string
	GroupID                                   *int64
	SwitchNotice, IsMockOpen, Strice, JSON5   *bool
}

// UpdateProject 按 patch 修改项目并刷新 up_time，返回匹配的行数。
func (s *Store) UpdateProject(ctx context.Context, id int64, p ProjectPatch) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE projects SET
		   name = coalesce(?, name), basepath = coalesce(?, basepath), description = coalesce(?, description),
		   project_type = coalesce(?, project_type), icon = coalesce(?, icon), color = coalesce(?, color),
		   env = coalesce(?, env), tag = coalesce(?, tag), pre_script = coalesce(?, pre_script),
		   after_script = coalesce(?, after_script), project_mock_script = coalesce(?, project_mock_script),
		   group_id = coalesce(?, group_id), switch_notice = coalesce(?, switch_notice),
		   is_mock_open = coalesce(?, is_mock_open), strice = coalesce(?, strice), is_json5 = coalesce(?, is_json5),
		   up_time = ?
		 WHERE id = ?`,
		nullString(p.Name), nullString(p.Basepath), nullString(p.Desc), nullString(p.ProjectType),
		nullString(p.Icon), nullString(p.Color), nullString(p.Env), nullString(p.Tag), nullString(p.PreScript),
		nullString(p.AfterScript), nullString(p.ProjectMockScript), nullInt(p.GroupID), nullBool(p.SwitchNotice),
		nullBool(p.IsMockOpen), nullBool(p.Strice), nullBool(p.JSON5), Now(), id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteProject 删除项目；分类、接口、成员、token、关注由外键级联删除。返回删除的行数。
func (s *Store) DeleteProject(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM projects WHERE id = ?", id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ProjectMembers 按加入顺序列出项目成员。
func (s *Store) ProjectMembers(ctx context.Context, projectID int64) ([]ProjectMember, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT uid, role, username, email, email_notice, sub_id FROM project_members WHERE project_id = ? ORDER BY position",
		projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ProjectMember
	for rows.Next() {
		m, err := scanProjectMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ProjectMemberOf 返回项目中的某个成员。
func (s *Store) ProjectMemberOf(ctx context.Context, projectID, uid int64) (*ProjectMember, error) {
	return scanProjectMember(s.db.QueryRowContext(ctx,
		"SELECT uid, role, username, email, email_notice, sub_id FROM project_members WHERE project_id = ? AND uid = ?",
		projectID, uid))
}

// AddProjectMembers 把成员追加到项目末尾（默认接收邮件通知），回填 SubID；返回项目是否存在。
func (s *Store) AddProjectMembers(ctx context.Context, projectID int64, members []Member) (bool, error) {
	var exists bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE id = ?", projectID).Scan(&n); err != nil {
			return err
		}
		if exists = n > 0; !exists {
			return nil
		}
		list := make([]ProjectMember, len(members))
		for i, m := range members {
			m.SubID = ""
			list[i] = ProjectMember{Member: m, EmailNotice: true}
		}
		if err := insertProjectMembers(ctx, tx, projectID, list); err != nil {
			return err
		}
		for i := range members {
			members[i].SubID = list[i].SubID
		}
		return nil
	})
	return exists, err
}

// ChangeProjectMemberRole 修改项目成员角色，返回匹配的行数。
func (s *Store) ChangeProjectMemberRole(ctx context.Context, projectID, uid int64, role string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE project_members SET role = ? WHERE project_id = ? AND uid = ?", role, projectID, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetProjectMemberNotice 设置项目成员是否接收邮件通知，返回匹配的行数。
func (s *Store) SetProjectMemberNotice(ctx context.Context, projectID, uid int64, notice bool) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE project_members SET email_notice = ? WHERE project_id = ? AND uid = ?",
		boolInt(notice), projectID, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RemoveProjectMember 移除项目成员，返回删除的行数。
func (s *Store) RemoveProjectMember(ctx context.Context, projectID, uid int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM project_members WHERE project_id = ? AND uid = ?", projectID, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ProjectMember 是项目成员，比分组成员多一个是否接收邮件通知。
type ProjectMember struct {
	Member
	EmailNotice bool
}

func scanProjectMember(row interface{ Scan(...any) error }) (*ProjectMember, error) {
	var m ProjectMember
	var username, email sql.NullString
	var notice int
	err := row.Scan(&m.UID, &m.Role, &username, &email, &notice, &m.SubID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Username, m.Email, m.EmailNotice = stringPtr(username), stringPtr(email), notice == 1
	return &m, nil
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullBool(v *bool) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(boolInt(*v)), Valid: true}
}
