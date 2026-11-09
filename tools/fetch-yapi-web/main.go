// Command fetch-yapi-web 从 npm 下载 yapi-vendor，校验完整性后把其中预构建的前端（static 目录）解压到本地。
// 已有预压缩 .gz 的文件不保留原文件：前端要打进可执行文件，这样能小 6MB，不支持 gzip 的客户端由服务端解压。
//
// 用法：go run ./tools/fetch-yapi-web [-version 1.12.0] [-out web/yapi]
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	staticPrefix = "package/static/"
	// 单个文件和整个压缩包的大小上限，防止异常数据耗尽磁盘或内存。
	maxFileSize    = 50 << 20
	maxTarballSize = 100 << 20
)

func main() {
	version := flag.String("version", "1.12.0", "yapi-vendor 版本")
	registry := flag.String("registry", "https://registry.npmjs.org", "npm 仓库地址")
	out := flag.String("out", "web/yapi", "输出目录（会先清空）")
	flag.Parse()

	if err := run(*registry, *version, *out); err != nil {
		slog.Error("获取 YApi 前端失败", "err", err)
		os.Exit(1)
	}
}

func run(registry, version, out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var meta struct {
		Dist struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	metaURL := strings.TrimSuffix(registry, "/") + "/yapi-vendor/" + version
	body, err := download(ctx, metaURL, 1<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return fmt.Errorf("解析包信息: %w", err)
	}

	tarball, err := download(ctx, meta.Dist.Tarball, maxTarballSize)
	if err != nil {
		return err
	}
	if err := verifyIntegrity(tarball, meta.Dist.Integrity); err != nil {
		return err
	}

	if err := os.RemoveAll(out); err != nil {
		return err
	}
	n, err := extractStatic(bytes.NewReader(tarball), out)
	if err != nil {
		return err
	}
	dropped, err := dropPlainCopies(out)
	if err != nil {
		return err
	}
	n -= dropped
	// 恢复仓库里的占位文件（内容为空），否则 git 会显示它被删除
	if err := os.WriteFile(filepath.Join(out, ".gitkeep"), nil, 0o600); err != nil {
		return err
	}
	slog.Info("已解压 YApi 前端", "version", version, "files", n, "out", out)
	return nil
}

// download 读取 url 的内容，超过 limit 字节视为错误。
func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("下载 %s: 超过 %d 字节上限", url, limit)
	}
	return data, nil
}

// verifyIntegrity 按 npm 的 Subresource Integrity 格式（sha512-<base64>）校验数据。
func verifyIntegrity(data []byte, integrity string) error {
	want, ok := strings.CutPrefix(integrity, "sha512-")
	if !ok {
		return fmt.Errorf("不支持的完整性格式: %q", integrity)
	}
	sum := sha512.Sum512(data)
	if got := base64.StdEncoding.EncodeToString(sum[:]); got != want {
		return errors.New("压缩包完整性校验失败，内容与 npm 记录不一致")
	}
	return nil
}

// extractStatic 把 npm 包中 package/static/ 下的普通文件解压到 out，返回文件数。
func extractStatic(r io.Reader, out string) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(hdr.Name, staticPrefix) {
			continue
		}
		rel, err := safeRelPath(strings.TrimPrefix(hdr.Name, staticPrefix))
		if err != nil {
			return count, err
		}
		if err := writeFile(filepath.Join(out, rel), tr); err != nil {
			return count, err
		}
		count++
	}
}

// dropPlainCopies 删除 dir 下已有同名 .gz 的原文件，返回删除的个数。
func dropPlainCopies(dir string) (int, error) {
	var plain []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".gz") {
			return err
		}
		if _, err := os.Stat(strings.TrimSuffix(p, ".gz")); err == nil {
			plain = append(plain, strings.TrimSuffix(p, ".gz"))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, p := range plain {
		if err := os.Remove(p); err != nil {
			return 0, err
		}
	}
	return len(plain), nil
}

// safeRelPath 拒绝绝对路径和包含 ".." 的路径，防止解压到输出目录之外。
func safeRelPath(name string) (string, error) {
	clean := path.Clean(name)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("压缩包中有不安全的路径: %q", name)
	}
	return filepath.FromSlash(clean), nil
}

func writeFile(dst string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	f, err := os.Create(dst) // #nosec G304 -- dst 已经过 safeRelPath 校验
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(src, maxFileSize+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n > maxFileSize {
		err = fmt.Errorf("%s 超过单个文件大小上限", dst)
	}
	return err
}
