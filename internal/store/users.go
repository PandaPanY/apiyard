package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// User 对应 YApi 的 user 集合。
type User struct {
	ID       int64
	Username string
	Email    string
	Password string // 哈希值，见 auth.HashPassword
	Passsalt string
	Role     string // admin / member
	Type     string // site / third
	Study    bool   // 是否已看完新手指引
	AddTime  int64
	UpTime   int64
}

const userColumns = "id, username, email, password, passsalt, role, type, study, add_time, up_time"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var study int
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Password, &u.Passsalt, &u.Role, &u.Type, &study, &u.AddTime, &u.UpTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Study = study == 1
	return &u, nil
}

// CreateUser 新建用户并回填 ID。
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password, passsalt, role, type, study, add_time, up_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.Email, u.Password, u.Passsalt, u.Role, u.Type, boolInt(u.Study), u.AddTime, u.UpTime)
	if err != nil {
		return err
	}
	u.ID, err = res.LastInsertId()
	return err
}

// UserByID 按 ID 查询用户。
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
}

// UserByEmail 按邮箱查询用户。
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE email = ?", email))
}

// CountUsers 返回用户总数。
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n)
	return n, err
}

// ListUsers 按 ID 倒序分页列出用户，page 从 1 开始。
func (s *Store) ListUsers(ctx context.Context, page, limit int) ([]*User, error) {
	return s.queryUsers(ctx, "SELECT "+userColumns+" FROM users ORDER BY id DESC LIMIT ? OFFSET ?",
		limit, (page-1)*limit)
}

// SearchUsers 按邮箱或用户名不区分大小写地模糊匹配，最多返回 10 个。
func (s *Store) SearchUsers(ctx context.Context, keyword string) ([]*User, error) {
	k := strings.ToLower(keyword)
	return s.queryUsers(ctx,
		"SELECT "+userColumns+" FROM users WHERE instr(lower(email), ?) > 0 OR instr(lower(username), ?) > 0 ORDER BY id LIMIT 10",
		k, k)
}

func (s *Store) queryUsers(ctx context.Context, query string, args ...any) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// syncMemberCopies 把用户的最新用户名和邮箱同步到分组、项目成员里保存的副本，参数依次为三个 uid。
var syncMemberCopies = []string{
	`UPDATE group_members SET username = (SELECT username FROM users WHERE id = ?),
	        email = (SELECT email FROM users WHERE id = ?) WHERE uid = ?`,
	`UPDATE project_members SET username = (SELECT username FROM users WHERE id = ?),
	        email = (SELECT email FROM users WHERE id = ?) WHERE uid = ?`,
}

// UpdateProfile 修改用户名和邮箱（为 nil 的不改），并同步分组、项目成员里保存的副本。返回匹配的行数。
// YApi 只同步了第一个包含该成员的项目（漏了 multi 选项），这里全部同步。
func (s *Store) UpdateProfile(ctx context.Context, uid int64, username, email *string) (int64, error) {
	var n int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE users SET username = coalesce(?, username), email = coalesce(?, email), up_time = ? WHERE id = ?",
			nullString(username), nullString(email), Now(), uid)
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		for _, stmt := range syncMemberCopies {
			if _, err := tx.ExecContext(ctx, stmt, uid, uid, uid); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// UpdatePassword 更换密码哈希和 passsalt。passsalt 也是登录令牌的签名密钥，所以旧的登录状态随之失效。
func (s *Store) UpdatePassword(ctx context.Context, uid int64, hash, passsalt string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET password = ?, passsalt = ?, up_time = ? WHERE id = ?",
		hash, passsalt, Now(), uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteUser 删除用户，返回删除的行数。与 YApi 一样不清理该用户的个人空间和成员身份。
func (s *Store) DeleteUser(ctx context.Context, uid int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkStudied 记录用户已看完新手指引，返回受影响的行数。
func (s *Store) MarkStudied(ctx context.Context, uid int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET study = 1, up_time = ? WHERE id = ?", Now(), uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
