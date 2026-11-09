package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleFiles(t *testing.T) []archiveFile {
	t.Helper()
	dir := t.TempDir()
	bin, readme := filepath.Join(dir, "apiyard"), filepath.Join(dir, "README.md")
	if err := os.WriteFile(bin, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readme, []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []archiveFile{{name: "x/apiyard", path: bin, exec: true}, {name: "x/README.md", path: readme}}
}

var mtime = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func TestTarGz(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := writeTarGz(dst, sampleFiles(t), mtime); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		got[hdr.Name] = string(data)
		if !hdr.ModTime.Equal(mtime) {
			t.Errorf("%s 的时间 %v", hdr.Name, hdr.ModTime)
		}
		if want := map[string]int64{"x/apiyard": 0o755, "x/README.md": 0o644}[hdr.Name]; hdr.Mode != want {
			t.Errorf("%s 的权限 %o，应为 %o", hdr.Name, hdr.Mode, want)
		}
	}
	if got["x/apiyard"] != "binary" || got["x/README.md"] != "readme" {
		t.Errorf("内容不对: %v", got)
	}
}

func TestZip(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "a.zip")
	if err := writeZip(dst, sampleFiles(t), mtime); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) != 2 || zr.File[0].Name != "x/apiyard" || zr.File[0].Mode().Perm() != 0o755 {
		t.Fatalf("文件列表不对: %+v", zr.File)
	}
	rc, err := zr.File[1].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	if data, _ := io.ReadAll(rc); string(data) != "readme" {
		t.Errorf("内容不对: %q", data)
	}
}

func TestChecksums(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"b.zip": "b", "a.tar.gz": "a"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeChecksums(dir, []string{"b.zip", "a.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	want := "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a.tar.gz\n" +
		"3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b.zip\n"
	if string(data) != want {
		t.Errorf("checksums.txt:\n%s", data)
	}
}

func TestVersionPattern(t *testing.T) {
	for v, ok := range map[string]bool{"v0.1.0": true, "v1.2.3-rc.1": true, "0.1.0": false, "v0.1": false, "v0.1.0 -X": false, "": false} {
		if versionPattern.MatchString(v) != ok {
			t.Errorf("%q 的校验结果应为 %v", v, ok)
		}
	}
	if err := run("v0.1", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "格式") {
		t.Errorf("错误的版本号应报错: %v", err)
	}
}
