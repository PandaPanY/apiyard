// Package store 是 ApiYard 的数据访问层，底层使用 SQLite（纯 Go 实现，无需 CGO）。
// 表结构与设计取舍见 docs/schema.md。
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 注册 "sqlite" 驱动
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrNotFound 表示要查询的记录不存在。
var ErrNotFound = errors.New("记录不存在")

// Store 封装数据库连接和全部查询。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库文件并执行未完成的迁移。
func Open(ctx context.Context, file string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	db, err := sql.Open("sqlite", "file:"+file+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// SQLite 同一时刻只允许一个写入者；单连接让所有操作排队执行，避免 SQLITE_BUSY。
	// v0.1 面向中小团队，这个吞吐量足够。
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate 按文件名顺序执行版本号大于 PRAGMA user_version 的迁移，每个迁移在一个事务里完成。
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(path.Base(name), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("迁移文件名必须以版本号开头: %s", name)
		}
		if version <= current {
			continue
		}
		ddl, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		err = s.withTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(ddl)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version))
			return err
		})
		if err != nil {
			return fmt.Errorf("执行迁移 %s: %w", name, err)
		}
	}
	return nil
}

// withTx 在事务中执行 fn，fn 返回错误时回滚。
func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Now 返回 YApi 使用的时间格式：Unix 秒。
func Now() int64 {
	return time.Now().Unix()
}

// NewObjectID 生成与 MongoDB ObjectId 格式相同的 24 位十六进制 ID（4 字节时间戳 + 8 字节随机数）。
// YApi 嵌套数组的每个元素都带这样的 _id，前端可能用它作为列表的 key。
func NewObjectID() string {
	var b [12]byte
	binary.BigEndian.PutUint32(b[:4], uint32(time.Now().Unix())) // #nosec G115 -- 2106 年前不会溢出
	_, _ = rand.Read(b[4:])
	return hex.EncodeToString(b[:])
}

// nullString 把空指针转换为 SQL NULL。
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// stringPtr 把 SQL NULL 转换为空指针，NULL 表示 YApi 原文档里没有这个字段。
func stringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
