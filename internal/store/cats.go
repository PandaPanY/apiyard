package store

import (
	"context"
	"database/sql"
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
		`SELECT id, uid, project_id, name, description, position, add_time, up_time
		   FROM interface_cats WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Cat
	for rows.Next() {
		var c Cat
		var desc sql.NullString
		if err := rows.Scan(&c.ID, &c.UID, &c.ProjectID, &c.Name, &desc, &c.Position, &c.AddTime, &c.UpTime); err != nil {
			return nil, err
		}
		c.Desc = stringPtr(desc)
		out = append(out, &c)
	}
	return out, rows.Err()
}
