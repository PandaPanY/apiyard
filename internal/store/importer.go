package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Importer 在一个事务里批量写入从 YApi 迁移来的数据：按列名插入，保留原来的 ID。只供数据迁移使用。
// 表名和列名都由调用方的代码给出，不来自外部输入。
type Importer struct {
	tx    *sql.Tx
	stmts map[string]*sql.Stmt
}

// Import 在一个事务里执行 fn；fn 返回错误时全部回滚。
// 单条记录写入失败（例如违反约束）不会中断事务，调用方可以记录下来后继续写入其他记录。
func (s *Store) Import(ctx context.Context, fn func(*Importer) error) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		im := &Importer{tx: tx, stmts: map[string]*sql.Stmt{}}
		defer im.close()
		return fn(im)
	})
}

func (im *Importer) close() {
	for _, st := range im.stmts {
		_ = st.Close()
	}
}

// Insert 向 table 插入一行，cols 与 vals 一一对应。相同的表和列复用预编译语句。
func (im *Importer) Insert(ctx context.Context, table string, cols []string, vals []any) error {
	if len(cols) != len(vals) {
		return fmt.Errorf("%s：列数 %d 与值的个数 %d 不一致", table, len(cols), len(vals))
	}
	key := table + "(" + strings.Join(cols, ",") + ")"
	st, ok := im.stmts[key]
	if !ok {
		query := "INSERT INTO " + key + " VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")" // #nosec G202 -- 表名和列名来自代码
		var err error
		if st, err = im.tx.PrepareContext(ctx, query); err != nil {
			return err
		}
		im.stmts[key] = st
	}
	_, err := st.ExecContext(ctx, vals...)
	return err
}

// RaiseSequence 把 table 的自增序列至少抬高到 seq，之后新建的记录不会复用 YApi 里已删除数据的 ID。
func (im *Importer) RaiseSequence(ctx context.Context, table string, seq int64) error {
	_, err := im.tx.ExecContext(ctx, "UPDATE sqlite_sequence SET seq = max(seq, ?) WHERE name = ?", seq, table)
	return err
}

// Sequence 返回 table 当前的自增序列值。
func (s *Store) Sequence(ctx context.Context, table string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name = ?", table).Scan(&seq)
	return seq, err
}

// CountRows 返回 table 的行数，用于迁移后的核对。table 由调用方的代码给出。
func (s *Store) CountRows(ctx context.Context, table string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n) // #nosec G202 -- 表名来自代码
	return n, err
}
