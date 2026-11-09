package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Interface 对应 YApi 的 interface 集合。JSON 列保存的是整理好的子文档数组；可选文本字段为 nil 表示原文档里没有该字段。
type Interface struct {
	ID                  int64
	UID                 int64
	EditUID             int64 // 正在编辑的用户，0 表示无人编辑
	ProjectID           int64
	CatID               int64
	Title               string
	Path                string
	Method              string
	Status              string // undone / done
	Type                string // static / var
	APIOpened           bool
	Position            int64 // YApi 字段名 index
	Desc                *string
	Markdown            *string
	QueryPath           string // JSON 对象 {path, params}
	ReqQuery            string // 以下四个是 JSON 数组
	ReqHeaders          string
	ReqParams           string
	ReqBodyForm         string
	ReqBodyType         *string
	ReqBodyIsJSONSchema bool
	ReqBodyOther        *string
	ResBodyType         *string
	ResBodyIsJSONSchema bool
	ResBody             *string
	CustomFieldValue    *string
	Tag                 string // JSON 数组
	AddTime             int64
	UpTime              int64
}

const interfaceColumns = `id, uid, edit_uid, project_id, catid, title, path, method, status, type, api_opened, position,
	description, markdown, query_path, req_query, req_headers, req_params, req_body_form, req_body_type,
	req_body_is_json_schema, req_body_other, res_body_type, res_body_is_json_schema, res_body, custom_field_value,
	tag, add_time, up_time`

func scanInterface(row interface{ Scan(...any) error }) (*Interface, error) {
	var it Interface
	var opened, reqSchema, resSchema int
	var desc, markdown, reqType, reqOther, resType, resBody, custom sql.NullString
	err := row.Scan(&it.ID, &it.UID, &it.EditUID, &it.ProjectID, &it.CatID, &it.Title, &it.Path, &it.Method, &it.Status,
		&it.Type, &opened, &it.Position, &desc, &markdown, &it.QueryPath, &it.ReqQuery, &it.ReqHeaders, &it.ReqParams,
		&it.ReqBodyForm, &reqType, &reqSchema, &reqOther, &resType, &resSchema, &resBody, &custom, &it.Tag,
		&it.AddTime, &it.UpTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	it.APIOpened, it.ReqBodyIsJSONSchema, it.ResBodyIsJSONSchema = opened == 1, reqSchema == 1, resSchema == 1
	it.Desc, it.Markdown, it.CustomFieldValue = stringPtr(desc), stringPtr(markdown), stringPtr(custom)
	it.ReqBodyType, it.ReqBodyOther = stringPtr(reqType), stringPtr(reqOther)
	it.ResBodyType, it.ResBody = stringPtr(resType), stringPtr(resBody)
	return &it, nil
}

func (s *Store) queryInterfaces(ctx context.Context, query string, args ...any) ([]*Interface, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Interface
	for rows.Next() {
		it, err := scanInterface(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// interfaceArgs 返回除 id 外全部列的值，顺序与 INSERT/UPDATE 语句一致。
func interfaceArgs(it *Interface) []any {
	return []any{it.UID, it.EditUID, it.ProjectID, it.CatID, it.Title, it.Path, it.Method, it.Status, it.Type,
		boolInt(it.APIOpened), it.Position, nullString(it.Desc), nullString(it.Markdown), it.QueryPath, it.ReqQuery,
		it.ReqHeaders, it.ReqParams, it.ReqBodyForm, nullString(it.ReqBodyType), boolInt(it.ReqBodyIsJSONSchema),
		nullString(it.ReqBodyOther), nullString(it.ResBodyType), boolInt(it.ResBodyIsJSONSchema), nullString(it.ResBody),
		nullString(it.CustomFieldValue), it.Tag, it.AddTime, it.UpTime}
}

const insertInterfaceSQL = `INSERT INTO interfaces (uid, edit_uid, project_id, catid, title, path, method, status, type,
	api_opened, position, description, markdown, query_path, req_query, req_headers, req_params, req_body_form,
	req_body_type, req_body_is_json_schema, req_body_other, res_body_type, res_body_is_json_schema, res_body,
	custom_field_value, tag, add_time, up_time)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// CreateInterface 新建接口并回填 ID。
func (s *Store) CreateInterface(ctx context.Context, it *Interface) error {
	res, err := s.db.ExecContext(ctx, insertInterfaceSQL, interfaceArgs(it)...)
	if err != nil {
		return err
	}
	it.ID, err = res.LastInsertId()
	return err
}

// UpdateInterface 用 it 的内容覆盖整条接口记录（id 不变）。
func (s *Store) UpdateInterface(ctx context.Context, it *Interface) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE interfaces SET uid = ?, edit_uid = ?, project_id = ?, catid = ?, title = ?, path = ?, method = ?,
		   status = ?, type = ?, api_opened = ?, position = ?, description = ?, markdown = ?, query_path = ?,
		   req_query = ?, req_headers = ?, req_params = ?, req_body_form = ?, req_body_type = ?,
		   req_body_is_json_schema = ?, req_body_other = ?, res_body_type = ?, res_body_is_json_schema = ?,
		   res_body = ?, custom_field_value = ?, tag = ?, add_time = ?, up_time = ?
		 WHERE id = ?`, append(interfaceArgs(it), it.ID)...)
	return err
}

// InterfaceByID 按 ID 查询接口。
func (s *Store) InterfaceByID(ctx context.Context, id int64) (*Interface, error) {
	return scanInterface(s.db.QueryRowContext(ctx, "SELECT "+interfaceColumns+" FROM interfaces WHERE id = ?", id))
}

// InterfacesByRoute 按项目、路径（含查询串，原样比较）和方法查找接口，对应 YApi 的 getByPath、checkRepeat。
// method 为空时匹配任意方法（YApi 查询条件里的 undefined 会被忽略）。
func (s *Store) InterfacesByRoute(ctx context.Context, projectID int64, path, method string) ([]*Interface, error) {
	return s.queryInterfaces(ctx,
		"SELECT "+interfaceColumns+" FROM interfaces WHERE project_id = ? AND path = ? AND (? = '' OR method = ?) ORDER BY id",
		projectID, path, method, method)
}

// InterfaceFilter 是接口列表的筛选条件。ProjectID 和 CatID 为 0 时不按其筛选；Status、Tag 为空时不按其筛选，
// 否则匹配其中任意一个（YApi 的 $in）。
type InterfaceFilter struct {
	ProjectID int64
	CatID     int64
	Status    []string
	Tag       []string
}

// args 把筛选条件转换为 interfaceFilterSQL 的参数；列表条件以 JSON 数组传入，SQL 保持静态。
func (f InterfaceFilter) args() ([]any, error) {
	list := func(v []string) (sql.NullString, error) {
		if len(v) == 0 {
			return sql.NullString{}, nil
		}
		data, err := json.Marshal(v)
		return sql.NullString{String: string(data), Valid: true}, err
	}
	status, err := list(f.Status)
	if err != nil {
		return nil, err
	}
	tag, err := list(f.Tag)
	if err != nil {
		return nil, err
	}
	return []any{f.ProjectID, f.ProjectID, f.CatID, f.CatID, status, status, tag, tag}, nil
}

const interfaceFilterSQL = ` WHERE (? = 0 OR project_id = ?) AND (? = 0 OR catid = ?)
	AND (? IS NULL OR status IN (SELECT value FROM json_each(?)))
	AND (? IS NULL OR EXISTS (SELECT 1 FROM json_each(tag) t WHERE t.value IN (SELECT value FROM json_each(?))))`

// ListInterfaces 分页列出接口。byTitle 为 true 时按标题排序（YApi 的 list），否则按 index 排序（listByOptionWithPage），
// 相同时都按创建顺序。limit <= 0 表示不限条数。
func (s *Store) ListInterfaces(ctx context.Context, f InterfaceFilter, byTitle bool, offset, limit int64) ([]*Interface, error) {
	args, err := f.args()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = -1 // SQLite 中 LIMIT -1 表示不限
	}
	args = append(args, boolInt(byTitle), limit, offset)
	return s.queryInterfaces(ctx, "SELECT "+interfaceColumns+" FROM interfaces"+interfaceFilterSQL+
		" ORDER BY CASE WHEN ? = 1 THEN title END, position, id LIMIT ? OFFSET ?", args...)
}

// CountInterfaces 统计符合条件的接口数。
func (s *Store) CountInterfaces(ctx context.Context, f InterfaceFilter) (int64, error) {
	args, err := f.args()
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM interfaces"+interfaceFilterSQL, args...).Scan(&n)
	return n, err
}

// InterfacesByProject 列出项目的全部接口，按 index、创建顺序排列，用于按分类组织菜单。
func (s *Store) InterfacesByProject(ctx context.Context, projectID int64) ([]*Interface, error) {
	return s.queryInterfaces(ctx,
		"SELECT "+interfaceColumns+" FROM interfaces WHERE project_id = ? ORDER BY position, id", projectID)
}

// SetInterfacePosition 修改接口的 index，返回匹配的行数。
func (s *Store) SetInterfacePosition(ctx context.Context, id, position int64) (int64, error) {
	return s.execCount(ctx, "UPDATE interfaces SET position = ? WHERE id = ?", position, id)
}

// ClaimInterfaceEditor 在接口无人编辑（或编辑者就是 uid）时把 uid 记为编辑者，返回是否成功。
func (s *Store) ClaimInterfaceEditor(ctx context.Context, id, uid int64) (bool, error) {
	n, err := s.execCount(ctx, "UPDATE interfaces SET edit_uid = ? WHERE id = ? AND edit_uid IN (0, ?)", uid, id, uid)
	return n > 0, err
}

// ReleaseInterfaceEditor 在编辑者仍是 uid 时清除编辑状态。
func (s *Store) ReleaseInterfaceEditor(ctx context.Context, id, uid int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE interfaces SET edit_uid = 0 WHERE id = ? AND edit_uid = ?", id, uid)
	return err
}

// resetInterfaceEditors 清除所有编辑状态。编辑状态随 WebSocket 连接存在，进程重启后不会再有人在编辑。
func (s *Store) resetInterfaceEditors(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE interfaces SET edit_uid = 0 WHERE edit_uid <> 0")
	return err
}

// DeleteInterface 删除接口，返回删除的行数。
func (s *Store) DeleteInterface(ctx context.Context, id int64) (int64, error) {
	return s.execCount(ctx, "DELETE FROM interfaces WHERE id = ?", id)
}

// DeleteCat 删除分类及其下的接口，返回删除的接口数（YApi 把 delByCatid 的结果作为响应）。
func (s *Store) DeleteCat(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM interfaces WHERE catid = ?", id)
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM interface_cats WHERE id = ?", id)
		return err
	})
	return n, err
}

func (s *Store) execCount(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// InterfacesByQueryPath 按 query_path.path（去掉查询串的路径）和方法查找接口，对应 YApi 的 getByQueryPath。
func (s *Store) InterfacesByQueryPath(ctx context.Context, projectID int64, path, method string) ([]*Interface, error) {
	return s.queryInterfaces(ctx,
		"SELECT "+interfaceColumns+" FROM interfaces WHERE project_id = ? AND json_extract(query_path, '$.path') = ? AND method = ? ORDER BY id",
		projectID, path, method)
}

// VarInterfaces 列出项目里路径带参数（type 为 var）的接口，对应 YApi 的 getVar，Mock 按路径模板匹配时使用。
func (s *Store) VarInterfaces(ctx context.Context, projectID int64, method string) ([]*Interface, error) {
	return s.queryInterfaces(ctx,
		"SELECT "+interfaceColumns+" FROM interfaces WHERE project_id = ? AND type = 'var' AND method = ? ORDER BY id",
		projectID, method)
}
