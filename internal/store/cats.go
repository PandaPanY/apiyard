package store

import (
	"context"
	"database/sql"
	"errors"
)

// Cat 是接口分类，对应 YApi 的 interface_cat 集合。
type Cat struct {
	ID        int64
	UID       int64
	ProjectID int64
	Name      string
	Desc      *string
	Position  int // YApi 字段名 index
	AddTime   int64
	UpTime    int64
}

func insertCat(ctx context.Context, tx *sql.Tx, c *Cat) error {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO interface_cats (uid, project_id, name, description, position, add_time, up_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.UID, c.ProjectID, c.Name, nullString(c.Desc), c.Position, c.AddTime, c.UpTime)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	return err
}

// CatsByProject 按顺序列出项目的分类（YApi 的 sort({index: 1})，相同时按创建顺序）。
func (s *Store) CatsByProject(ctx context.Context, projectID int64) ([]*Cat, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+catColumns+" FROM interface_cats WHERE project_id = ? ORDER BY position, id", projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Cat
	for rows.Next() {
		c, err := scanCat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const catColumns = "id, uid, project_id, name, description, position, add_time, up_time"

func scanCat(row interface{ Scan(...any) error }) (*Cat, error) {
	var c Cat
	var desc sql.NullString
	err := row.Scan(&c.ID, &c.UID, &c.ProjectID, &c.Name, &desc, &c.Position, &c.AddTime, &c.UpTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Desc = stringPtr(desc)
	return &c, nil
}

// CatByID 按 ID 查询分类。
func (s *Store) CatByID(ctx context.Context, id int64) (*Cat, error) {
	return scanCat(s.db.QueryRowContext(ctx, "SELECT "+catColumns+" FROM interface_cats WHERE id = ?", id))
}

// CreateCat 新建分类并回填 ID。
func (s *Store) CreateCat(ctx context.Context, c *Cat) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return insertCat(ctx, tx, c) })
}

// UpdateCat 修改分类名称和描述（为 nil 的不改）并刷新 up_time，返回匹配的行数。
func (s *Store) UpdateCat(ctx context.Context, id int64, name, desc *string) (int64, error) {
	return s.execCount(ctx,
		"UPDATE interface_cats SET name = coalesce(?, name), description = coalesce(?, description), up_time = ? WHERE id = ?",
		nullString(name), nullString(desc), Now(), id)
}

// SetCatPosition 修改分类的 index，返回匹配的行数。
func (s *Store) SetCatPosition(ctx context.Context, id, position int64) (int64, error) {
	return s.execCount(ctx, "UPDATE interface_cats SET position = ? WHERE id = ?", position, id)
}
