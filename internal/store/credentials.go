package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// IssueProjectCredential returns a stable, random credential bound to its issuer.
// The token contains no client-controlled identity or encrypted claims.
func (s *Store) IssueProjectCredential(ctx context.Context, projectID, uid int64) (string, error) {
	var token string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT credential FROM project_credentials WHERE project_id=? AND uid=?", projectID, uid).Scan(&token)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var data [32]byte
		if _, err := rand.Read(data[:]); err != nil {
			return err
		}
		token = hex.EncodeToString(data[:])
		_, err = tx.ExecContext(ctx, "INSERT INTO project_credentials(project_id,uid,credential) VALUES (?,?,?)", projectID, uid, token)
		return err
	})
	return token, err
}

// ProjectCredential resolves only credentials actually issued by ApiYard.
func (s *Store) ProjectCredential(ctx context.Context, token string) (projectID, uid int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT project_id,uid FROM project_credentials WHERE credential=?", token).Scan(&projectID, &uid)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
