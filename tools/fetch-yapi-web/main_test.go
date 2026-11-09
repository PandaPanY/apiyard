package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// makeTarball 按给定的 文件名→内容 构造 tar.gz。
func makeTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractStaticOnlyKeepsStaticDir(t *testing.T) {
	data := makeTarball(t, map[string]string{
		"package/static/index.html":    "<html>",
		"package/static/prd/assets.js": "window.WEBPACK_ASSETS={}",
		"package/server/app.js":        "server code",
	})
	out := t.TempDir()

	n, err := extractStatic(bytes.NewReader(data), out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("解压文件数 = %d, 期望 2", n)
	}
	got, err := os.ReadFile(filepath.Join(out, "prd", "assets.js"))
	if err != nil || string(got) != "window.WEBPACK_ASSETS={}" {
		t.Errorf("prd/assets.js 内容 = %q, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "app.js")); !os.IsNotExist(err) {
		t.Error("static 目录之外的文件不应被解压")
	}
}

func TestDropPlainCopies(t *testing.T) {
	out := t.TempDir()
	for _, name := range []string{"index.html", "prd/a.js", "prd/a.js.gz", "prd/b.css.gz", "prd/assets.js"} {
		p := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n, err := dropPlainCopies(out)
	if err != nil || n != 1 {
		t.Fatalf("删除了 %d 个, err = %v，应为 1", n, err)
	}
	for name, exists := range map[string]bool{"prd/a.js": false, "prd/a.js.gz": true, "prd/b.css.gz": true, "prd/assets.js": true, "index.html": true} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); (err == nil) != exists {
			t.Errorf("%s 是否存在应为 %v", name, exists)
		}
	}
}

func TestExtractStaticRejectsPathTraversal(t *testing.T) {
	data := makeTarball(t, map[string]string{"package/static/../../evil.txt": "x"})
	if _, err := extractStatic(bytes.NewReader(data), t.TempDir()); err == nil {
		t.Fatal("包含 .. 的路径应当被拒绝")
	}
}

func TestVerifyIntegrity(t *testing.T) {
	data := []byte("tarball")
	sum := sha512.Sum512(data)
	good := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])

	if err := verifyIntegrity(data, good); err != nil {
		t.Errorf("正确的校验值被拒绝: %v", err)
	}
	if err := verifyIntegrity([]byte("tampered"), good); err == nil {
		t.Error("内容被篡改时应当校验失败")
	}
	if err := verifyIntegrity(data, "sha1-abc"); err == nil {
		t.Error("不支持的算法应当报错")
	}
}
