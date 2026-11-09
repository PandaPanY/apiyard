// Command dist 交叉编译各平台的 apiyard，打包成压缩包并生成 SHA256 校验文件。
// 本机和 GitHub Actions 的发布流程都用它，保证两边产物相同。
//
// 用法：go run ./tools/dist -version v0.1.0 [-out dist] [-targets linux/amd64,windows/amd64]
//
// 运行前先用 go run ./tools/fetch-yapi-web 下载前端，否则可执行文件里没有界面。
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// defaultTargets 是每次发布的平台。modernc.org/sqlite 是纯 Go 实现，关掉 CGO 就能交叉编译。
var defaultTargets = []string{
	"linux/amd64", "linux/arm64",
	"darwin/amd64", "darwin/arm64",
	"windows/amd64", "windows/arm64",
}

// extraFiles 随可执行文件一起放进压缩包。
var extraFiles = []string{"LICENSE", "NOTICE", "README.md"}

// versionPattern 只接受 v1.2.3 或 v1.2.3-rc.1 这样的版本号，它会进入文件名和 -ldflags。
var versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

func main() {
	version := flag.String("version", "", "版本号，如 v0.1.0（必填）")
	out := flag.String("out", "dist", "输出目录（会先清空）")
	targets := flag.String("targets", strings.Join(defaultTargets, ","), "要编译的平台，逗号分隔")
	flag.Parse()

	if err := run(*version, *out, strings.Split(*targets, ",")); err != nil {
		slog.Error("打包失败", "err", err)
		os.Exit(1)
	}
}

func run(version, out string, targets []string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("版本号 %q 的格式应为 v1.2.3 或 v1.2.3-rc.1", version)
	}
	if _, err := os.Stat(filepath.Join("web", "yapi", "index.html")); err != nil {
		return errors.New("没有找到前端，先执行 go run ./tools/fetch-yapi-web")
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}

	// 压缩包里的文件时间统一用当前提交的时间，同一个提交打出的包内容一致
	mtime := commitTime()
	var archives []string
	for _, target := range targets {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(target), "/")
		if !ok {
			return fmt.Errorf("平台 %q 的格式应为 os/arch", target)
		}
		name, err := buildTarget(version, out, goos, goarch, mtime)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", goos, goarch, err)
		}
		slog.Info("已打包", "file", name)
		archives = append(archives, name)
	}
	return writeChecksums(out, archives)
}

// buildTarget 编译一个平台并打包，返回压缩包的文件名。
func buildTarget(version, out, goos, goarch string, mtime time.Time) (string, error) {
	binName := "apiyard"
	if goos == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(out, "build", goos+"_"+goarch, binName)
	cmd := exec.Command("go", "build", "-trimpath", // #nosec G204 -- 参数来自本工具的固定值和校验过的版本号
		"-ldflags", "-s -w -X main.buildVersion="+version,
		"-o", bin, "./cmd/apiyard")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(bin)) }()

	// 压缩包里有一层与压缩包同名的目录，解压后不会把文件散落在当前目录
	base := fmt.Sprintf("apiyard_%s_%s_%s", strings.TrimPrefix(version, "v"), goos, goarch)
	files := []archiveFile{{name: base + "/" + binName, path: bin, exec: true}}
	for _, f := range extraFiles {
		files = append(files, archiveFile{name: base + "/" + f, path: f})
	}
	if goos == "windows" {
		return base + ".zip", writeZip(filepath.Join(out, base+".zip"), files, mtime)
	}
	return base + ".tar.gz", writeTarGz(filepath.Join(out, base+".tar.gz"), files, mtime)
}

// commitTime 返回 HEAD 的提交时间；不在 git 仓库里时用当前时间。
func commitTime() time.Time {
	data, err := exec.Command("git", "log", "-1", "--format=%cI").Output()
	if err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil {
			return t
		}
	}
	return time.Now()
}

type archiveFile struct {
	name string // 压缩包里的路径
	path string // 本地文件
	exec bool   // 是否可执行
}

func (f archiveFile) mode() os.FileMode {
	if f.exec {
		return 0o755
	}
	return 0o644
}

func writeTarGz(dst string, files []archiveFile, mtime time.Time) (err error) {
	out, err := os.Create(dst) // #nosec G304 -- dst 由输出目录和固定的文件名组成
	if err != nil {
		return err
	}
	defer closeInto(out, &err)
	gz := gzip.NewWriter(out)
	defer closeInto(gz, &err)
	tw := tar.NewWriter(gz)
	defer closeInto(tw, &err)

	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: f.name, Mode: int64(f.mode()), Size: int64(len(data)), ModTime: mtime, Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func writeZip(dst string, files []archiveFile, mtime time.Time) (err error) {
	out, err := os.Create(dst) // #nosec G304 -- dst 由输出目录和固定的文件名组成
	if err != nil {
		return err
	}
	defer closeInto(out, &err)
	zw := zip.NewWriter(out)
	defer closeInto(zw, &err)

	for _, f := range files {
		hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: mtime}
		hdr.SetMode(f.mode())
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if err := copyFile(w, f.path); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path) // #nosec G304 -- path 是刚编译出的文件或仓库里的固定文件
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, f)
	return err
}

// writeChecksums 生成 checksums.txt，格式与 sha256sum 相同，可以用 sha256sum -c 校验。
func writeChecksums(dir string, names []string) error {
	slices.Sort(names)
	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- 本工具刚生成的压缩包
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if err := os.RemoveAll(filepath.Join(dir, "build")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(b.String()), 0o600)
}

// closeInto 关闭 c，并在 *err 为空时把关闭的错误写进去：压缩包的结尾在 Close 时才写入，错误不能丢。
func closeInto(c io.Closer, err *error) {
	if cerr := c.Close(); *err == nil {
		*err = cerr
	}
}
