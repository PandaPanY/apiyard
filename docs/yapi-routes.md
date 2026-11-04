# YApi 前端调用的接口清单

> 来源：`yapi-vendor@1.12.0` 中 `client/` 和 `exts/` 的源码静态扫描，共 105 个路径（预构建产物中为 104 个）。
> 前端沿用 YApi 原有界面（决策 D4），因此这些接口的路径、参数和响应格式都要与 YApi 一致。

| 阶段 | 数量 |
|---|---|
| M1 | 27 |
| M2 | 38 |
| M2 占位 | 4 |
| M3 | 3 |
| 延后 | 33 |

“开放”列标记属于开放 API 白名单（支持 token 访问）的接口。

| 阶段 | 接口 | 开放 | 备注 |
|---|---|---|---|
| M1 | `/api/follow/add` |  |  |
| M1 | `/api/follow/del` |  |  |
| M1 | `/api/follow/list` |  |  |
| M1 | `/api/group/add` |  |  |
| M1 | `/api/group/add_member` |  |  |
| M1 | `/api/group/change_member_role` |  |  |
| M1 | `/api/group/del` |  |  |
| M1 | `/api/group/del_member` |  |  |
| M1 | `/api/group/get` |  |  |
| M1 | `/api/group/get_member_list` |  |  |
| M1 | `/api/group/get_mygroup` |  |  |
| M1 | `/api/group/list` |  |  |
| M1 | `/api/group/up` |  |  |
| M1 | `/api/user` |  |  |
| M1 | `/api/user/avatar` |  |  |
| M1 | `/api/user/change_password` |  |  |
| M1 | `/api/user/del` |  |  |
| M1 | `/api/user/find` |  |  |
| M1 | `/api/user/list` |  |  |
| M1 | `/api/user/login` |  |  |
| M1 | `/api/user/logout` |  |  |
| M1 | `/api/user/reg` |  |  |
| M1 | `/api/user/search` |  |  |
| M1 | `/api/user/status` |  |  |
| M1 | `/api/user/up_study` |  |  |
| M1 | `/api/user/update` |  |  |
| M1 | `/api/user/upload_avatar` |  |  |
| M2 | `/api/interface/add` | ✓ |  |
| M2 | `/api/interface/add_cat` | ✓ |  |
| M2 | `/api/interface/del` |  |  |
| M2 | `/api/interface/del_cat` |  |  |
| M2 | `/api/interface/get` | ✓ |  |
| M2 | `/api/interface/getCatMenu` | ✓ |  |
| M2 | `/api/interface/list` | ✓ |  |
| M2 | `/api/interface/list_cat` | ✓ |  |
| M2 | `/api/interface/list_menu` | ✓ |  |
| M2 | `/api/interface/save` | ✓ |  |
| M2 | `/api/interface/schema2json` |  |  |
| M2 | `/api/interface/solve_conflict` |  |  |
| M2 | `/api/interface/up` | ✓ |  |
| M2 | `/api/interface/up_cat` |  |  |
| M2 | `/api/interface/up_cat_index` |  |  |
| M2 | `/api/interface/up_index` |  |  |
| M2 | `/api/log/list` |  |  |
| M2 | `/api/log/list_by_update` |  |  |
| M2 | `/api/project/add` |  |  |
| M2 | `/api/project/add_member` |  |  |
| M2 | `/api/project/change_member_email_notice` |  |  |
| M2 | `/api/project/change_member_role` |  |  |
| M2 | `/api/project/check_project_name` |  |  |
| M2 | `/api/project/copy` |  |  |
| M2 | `/api/project/del` |  |  |
| M2 | `/api/project/del_member` |  |  |
| M2 | `/api/project/download` |  | 原版没有实现（前端按钮是隐藏的），返回 404，ApiYard 相同 |
| M2 | `/api/project/get` | ✓ |  |
| M2 | `/api/project/get_env` |  |  |
| M2 | `/api/project/get_member_list` |  |  |
| M2 | `/api/project/list` |  |  |
| M2 | `/api/project/search` |  |  |
| M2 | `/api/project/token` |  |  |
| M2 | `/api/project/up` | ✓ |  |
| M2 | `/api/project/up_env` |  |  |
| M2 | `/api/project/up_tag` |  |  |
| M2 | `/api/project/update_token` |  |  |
| M2 | `/api/project/upset` |  |  |
| M2 占位 | `/api/interface/download_crx` |  | 前端目录里带有插件包，直接提供下载 |
| M2 占位 | `/api/project/swagger_url` |  | 返回 402 和提示（服务端代取 URL 有 SSRF 风险） |
| M2 占位 | `/api/user/login_by_ldap` |  | 返回 401 和“暂不支持 LDAP 登录” |
| M2 占位 | `/api/user/login_by_token` |  | 与原版没有登录插件时相同：跳回首页 |
| M3 | `/api/interface/interUpload` |  | 导入导出 |
| M3 | `/api/plugin/export` | ✓ | 导入导出 |
| M3 | `/api/plugin/exportSwagger` | ✓ | 导入导出 |
| 延后 | `/api/col/add_case` |  | 自动化测试 |
| 延后 | `/api/col/add_case_list` |  | 自动化测试 |
| 延后 | `/api/col/add_col` |  | 自动化测试 |
| 延后 | `/api/col/case` |  | 自动化测试 |
| 延后 | `/api/col/case_env_list` |  | 自动化测试 |
| 延后 | `/api/col/case_list` |  | 自动化测试 |
| 延后 | `/api/col/case_list_by_var_params` |  | 自动化测试 |
| 延后 | `/api/col/clone_case_list` |  | 自动化测试 |
| 延后 | `/api/col/del_case` |  | 自动化测试 |
| 延后 | `/api/col/del_col` |  | 自动化测试 |
| 延后 | `/api/col/list` |  | 自动化测试 |
| 延后 | `/api/col/run_script` |  | 自动化测试 |
| 延后 | `/api/col/up_case` |  | 自动化测试 |
| 延后 | `/api/col/up_case_index` |  | 自动化测试 |
| 延后 | `/api/col/up_col` |  | 自动化测试 |
| 延后 | `/api/col/up_col_index` |  | 自动化测试 |
| 延后 | `/api/open/run_auto_test` | ✓ | 自动化测试 |
| 延后 | `/api/plugin/advmock/case/del` |  | 高级 Mock |
| 延后 | `/api/plugin/advmock/case/hide` |  | 高级 Mock |
| 延后 | `/api/plugin/advmock/case/list` |  | 高级 Mock |
| 延后 | `/api/plugin/advmock/case/save` |  | 高级 Mock |
| 延后 | `/api/plugin/advmock/get` |  | 高级 Mock |
| 延后 | `/api/plugin/advmock/save` |  | 高级 Mock |
| 延后 | `/api/plugin/autoSync/get` |  | Swagger 自动同步 |
| 延后 | `/api/plugin/autoSync/save` |  | Swagger 自动同步 |
| 延后 | `/api/plugin/export/pdf` |  | 导出 PDF |
| 延后 | `/api/plugin/statismock/count` |  | 统计 |
| 延后 | `/api/plugin/statismock/get` |  | 统计 |
| 延后 | `/api/plugin/statismock/get_system_status` |  | 统计 |
| 延后 | `/api/plugin/statismock/group_data_statis` |  | 统计 |
| 延后 | `/api/plugin/wiki_desc/get` |  | Wiki |
| 延后 | `/api/plugin/wiki_desc/up` |  | Wiki |
| 延后 | `/api/ws_plugin/wiki_desc/solve_conflict` |  | Wiki |
