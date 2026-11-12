# 发布流程

发布由 GitHub Actions 完成（`.github/workflows/release.yml`），本机只需要打标签。

下一版本的 [v0.1.1 发布说明草稿](releases/v0.1.1.md) 和 [验收记录](releases/v0.1.1-validation.md) 已独立整理。推送版本标签会在验证通过后推送镜像，因此发布准备阶段不要推送标签。

## 步骤

1. 确认 `main` 上的 CI 全部通过，`DEVLOG.md`、`README.md` 已更新。
2. 打标签并推送：

   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

3. 工作流会做两件事：
   - 两个发布任务都依赖共同的 vet、race 测试和漏洞检查成功。用 `tools/dist` 编译 6 个平台（Linux、macOS、Windows 的 amd64 和 arm64），创建 **Release 草稿**，附上压缩包、`checksums.txt` 和 `modules.json`；
   - 构建 amd64 和 arm64 的镜像，推送到 `ghcr.io/pandapany/apiyard`，标签为 `0.1.0` 和 `0.1`。
4. 在 GitHub 的 Releases 页面检查草稿的说明和附件，确认后点“Publish release”。

带后缀的版本（如 `v0.1.0-rc.1`）会标记为预发布，镜像只打完整版本号的标签，不更新 `0.1`。

## 在本机打包

与 CI 用的是同一个工具，可以先在本机确认：

```bash
go run ./tools/fetch-yapi-web
go run ./tools/dist -version v0.1.0      # 产物在 dist/
```

- 版本号必须是 `v1.2.3` 或 `v1.2.3-rc.1` 的形式，会写进可执行文件（`apiyard version` 可以看到）。
- 压缩包里文件的时间统一取当前提交的时间，同一个提交打出的包内容相同。
- 只编译部分平台：`-targets linux/amd64,windows/amd64`。

## CI

每次推送到 `main` 和每个 PR 都会运行 `.github/workflows/ci.yml`：`go vet`、`go test -race`、golangci-lint、6 个平台的编译，以及两种架构的镜像构建（不推送）。golangci-lint 的版本固定在工作流里，升级本机版本时同步修改。
