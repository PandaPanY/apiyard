package migrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

type blockedSource struct {
	Source
	entered, release chan struct{}
}

func (s *blockedSource) Each(ctx context.Context, coll string, fn func(bson.D) error) error {
	if coll == "user" {
		close(s.entered)
		<-s.release
	}
	return s.Source.Each(ctx, coll, fn)
}

func TestConcurrentMigrationRefusesSharedTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target.db")
	source := &blockedSource{Source: sample(), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := Run(context.Background(), source, Options{Target: target}); done <- err }()
	select {
	case <-source.entered:
	case err := <-done:
		t.Fatalf("migration failed before blocking: %v", err)
	}
	_, err := Run(context.Background(), sample(), Options{Target: target})
	close(source.release)
	if first := <-done; first != nil {
		t.Fatal(first)
	}
	if err == nil {
		t.Fatal("concurrent migration must refuse target")
	}
}

func TestInstallFailurePreservesOldDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "old.db")
	tmp := filepath.Join(dir, "new.db")
	if err := os.WriteFile(target, []byte("old-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new-data"), 0600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("rename failure")
	err := installDatabase(tmp, target, true, nil, func(string, string) error { return injected })
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old-data" {
		t.Fatal("old database lost")
	}
	backups, _ := filepath.Glob(target + ".before-migrate-*")
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	data, _ = os.ReadFile(backups[0])
	if string(data) != "old-data" {
		t.Fatal("backup lost")
	}
	if err := installDatabase(tmp, target, false, nil, os.Rename); err == nil {
		t.Fatal("no-overwrite must refuse destination")
	}
	if err := os.WriteFile(target+"-wal", []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installDatabase(tmp, target, true, nil, os.Rename); err == nil {
		t.Fatal("live WAL must refuse replacement")
	}
}
