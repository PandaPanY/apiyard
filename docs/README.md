# ApiYard 文档

ApiYard 是兼容 YApi 的轻量接口管理平台。界面沿用 YApi 1.12 的前端（换了配色、顶栏和登录页），日常操作（建项目、写接口、Mock、导入导出）与 YApi 相同，可以参考 [YApi 使用文档](https://hellosean1025.github.io/yapi/)；与 YApi 不同的地方见 [与 YApi 的差异](differences.md)。

## 使用

- [部署与配置](deployment.md)：可执行文件、Docker、配置项、反向代理、备份与升级
- [从 YApi 迁移](migration.md)：一条命令从 YApi 的 MongoDB 迁移，原账号、token 和链接继续有效
- [与 YApi 的差异](differences.md)：暂不支持的功能、正常使用中能看到的不同、安全修复

## 开发

- [架构设计](architecture.md)：技术选型和关键决定
- [路线图](roadmap.md)：里程碑和验收标准
- [测试](testing.md)：单元测试、与原版 YApi 逐字段比对的兼容测试、CI
- [发布流程](release.md)
- [YApi 接口清单](yapi-routes.md)、[数据结构](schema.md)
