# 测试问题清单与修复记录（2026-09-11）

测试对象：测试服务器 `https://124.220.69.160:9999/12138`（admin）
测试方式：Playwright 真实浏览器操作（PC 1920×1080 / 平板 768×1024 / 手机 390×844）+ 服务器端配置与 systemd 核对
测试范围：网站管理（创建/设置/SSL/安全/日志）、防火墙、WAF、其余功能页巡检、三档视口兼容性

> 测试期间新建的测试站点：`zz-proxy-test.example.com`(19)、`zz-py-test.example.com`(20)、`zzstatic.n.05v.cn`(21)、`zz-py2.n.05v.cn`(22)，可直接删除。

---

## 一、问题清单与修复状态

| # | 级别 | 问题 | 状态 |
|---|---|---|---|
| 1 | 高 | SSL 证书列在平板/手机被隐藏 → 无法申请/管理证书 | ✅ 已修复 |
| 2 | 中 | 反向代理站点 `proxy_port` 无意义落库，导致后续端口冲突误判 | ✅ 已修复 |
| 3 | 中 | 站点安全总开关关闭时规则静默不生效、无提示 | ✅ 已修复 |
| 4 | 中 | 手机端表格操作列/关键列被裁切 | ✅ 已修复 |
| 5 | 低 | `deploy_warning` 文案过长（含命令原始输出与噪音） | ✅ 已修复 |
| 6 | 低 | 反向代理站点「项目路径」命名不贴切 | ✅ 已修复 |
| 7 | 低 | SSL 申请失败提示为 ACME 英文原文 | ✅ 已修复 |
| 8 | 低 | 手机端根目录 placeholder 过长被截断 | ✅ 已修复 |
| 9 | 严重 | （修复过程中发现）systemd 单元路径被加引号导致服务无法启动 | ✅ 已修复 |

### 1. SSL 证书列在平板/手机被隐藏（高）

- **原因**：`showSslCol = !isMobile && !isCompact`，而该列是 SSL 弹窗唯一入口。
- **修复**（`web/src/views/Website.vue`）：
  - 平板保留 SSL 列：`showSslCol = computed(() => !isMobile.value)`
  - 手机端在「操作」列新增「证书」按钮作为入口
- **验证**：平板表头含 `SSL证书`；手机端操作列出现「证书」按钮且可点击。

### 2. 反向代理 `proxy_port` 误判（中）

- **原因**：反代表单不显示「项目端口」，但前端仍提交默认值 3000，后端 proxy 分支未清空 → 落库 `proxy_port=3000`，后续 Node/Python 用 3000 被 `checkProxyPortConflict` 拦下。
- **修复**：
  - 前端 `buildPayload()`：非 node/python/go 类型清空 `proxy_port`
  - 后端 `CreateSite`：非进程型站点强制 `s.ProxyPort = 0`
  - 后端 `checkProxyPortConflict`：只比较**进程型站点**的 `ProxyPort`（兼容历史脏数据）
  - 后端 `SaveSiteSettings` 的 base tab：同样清零
- **验证**：创建 Python 站点使用默认项目端口 3000 → 创建成功（修复前会被 `zz-proxy-test` 的 3000 阻断）。

### 3. 站点安全总开关关闭时规则静默不生效（中）

- **修复**（`SiteSecurityDialog.vue`）：保存时若总开关关闭且存在规则，弹出
  「配置已保存，但「启用安全防护」未打开，规则暂不会生效」
- **验证**：关闭开关 + 存在 IP 规则后保存，提示正确出现。

### 4. 手机端表格被裁切（中）

- **修复**（`Website.vue`）：
  - 手机端操作列从 5 个按钮改为「设置 / 证书 / 更多▾」（日志·统计·安全·删除 收进下拉），列宽 190 → 152px
  - 新增 `onMoreCmd()` 与下拉样式
- **验证**：手机端操作列完整显示无裁切，「更多」菜单项为 日志/统计/安全/删除。
- **说明**：防火墙等页面表格在窄屏仍需横向滑动（未改，属可接受行为），如需进一步优化可后续处理。

### 5. `deploy_warning` 文案过长（低）

- **修复**（`internal/service/website.go`）：
  - 新增 `pickCommandError()`：过滤 pip `[notice]`、npm warn 等噪音，优先取含 error/failed/no such 的行，最多 2 行、200 字符上限
  - 新增 `pickSystemctlError()`：过滤 `Created symlink` 等噪音，240 字符上限
- **验证**：创建站点时的告警文案明显缩短、聚焦真实原因。

### 6. 反向代理「项目路径」命名（低）

- **修复**（`SiteSettings.vue`）：仅 node/python/go 显示「项目路径」，其余（含反向代理）显示「网站目录」；反向代理额外提示「用于存放 SSL 证书验证文件（/.well-known），由面板自动管理」。
- **验证**：反代站点设置基础页 LABELS 含 `网站目录`，提示文案正确。

### 7. ACME 错误中文归纳（低）

- **修复**（`internal/service/acme.go`）：新增 `friendlyACMEError()`，按 dns / connection / unauthorized / rate 分类给出中文说明，并保留原始错误。
- **验证**：申请失败返回
  `域名验证失败: 域名 DNS 解析异常：请确认域名已解析到本服务器，且解析在境外/多地区可查询（CA 会从多个节点校验）。原始错误：…`

### 8. 手机端 placeholder 过长（低）

- **修复**：`rootPlaceholder` 精简为 `留空默认 <路径>`（详细规则由下方 tip 承载）。

### 9. systemd 单元路径加引号（严重，修复过程中引入并修复）

- **现象**：进程型站点创建/启动时报
  `Unit xxx.service has a bad unit file setting`
- **根因**：为「避免路径含空格」给 `WorkingDirectory=` / `ExecStart=` 加了引号，但 **systemd 这两个指令不做 shell 分词**，引号被当作路径一部分 →
  `WorkingDirectory= path is not absolute: "/www/wwwroot/xxx"`
- **修复**（`internal/service/website.go`）：
  - `WorkingDirectory` / `ExecStart` 恢复为裸值（站点名与 runner 文件名均受白名单约束，无空格）
  - 删除 `systemdQuoteArg`
  - **新增自愈**：`SiteAction` 的 start/restart 改为经 `writeSiteService()` 重建单元后再启动，可自动修复已损坏的历史单元文件
- **验证**：
  - `systemd-analyze verify` 对两个站点均无错误
  - 单元文件为 `WorkingDirectory=/www/wwwroot/xxx`（无引号）
  - 对既有「坏单元」站点点「启动」→ 接口 code=0，单元被重建为有效格式，服务 `enabled`

---

## 二、本轮改动的其他实测验证（通过）

| 修复项 | 验证结果 |
|---|---|
| 反向代理省略协议自动补全 | 生成 `proxy_pass http://127.0.0.1:18081;` ✅ |
| WebSocket 支持 | 站点配置含 `proxy_http_version 1.1` + `Upgrade` + `Connection $lp_connection_upgrade`；全局 `00-kypanel-global.conf` 生成 map；`nginx -t` 通过 ✅ |
| 上传体积限制 | 反代 location 内 `client_max_body_size 100m;` ✅ |
| 启动命令 `bash -c` 包装 | runner 脚本 `exec bash -c 'python app.py'` ✅ |
| `PORT` 自动注入 | runner 脚本 `export PORT=3000` ✅ |
| 启动命令可用性预检 | gunicorn 缺失时告警「启动命令中的 "gunicorn" 未在当前运行环境中找到」 ✅ |
| 依赖安装命令 | 创建时执行并返回精简告警 ✅ |
| 端口冲突校验 | 正确拦截；且不再误伤（见问题 2） ✅ |
| 命令不可用时不自动启动 | 站点状态为「已停止」 ✅ |
| 创建失败不整体回滚 | 返回告警且站点保留 ✅ |
| 单站安全 IP 黑名单 | 生成 `if ($remote_addr = "1.2.3.4") { return 403; }` 并 include、`nginx -t` 通过 ✅ |
| 站点真实访问 | `zzstatic.n.05v.cn` DNS → 124.220.69.160，HTTP 200 ✅ |

---

## 三、环境问题（非面板缺陷）

**Let's Encrypt 境外二次验证节点无法解析 `*.n.05v.cn`**
- 错误：`During secondary validation: DNS problem: networking error looking up A for zzstatic.n.05v.cn`
- 本机/国内解析正常、站点 HTTP 200 可访问；属 DNS 服务商对境外查询的限制或解析未全球生效。
- 建议：测试可用 LiteSSL 或 DNS 验证；正式环境确认 DNS 境外可达性。

---

## 四、其余功能页巡检结论（未发现异常）

PC 视口逐页打开，控制台 **0 error / 0 warning**：

概览、网站、数据库、FTP、Docker、监控、日志、防火墙、WAF（防护中，34 条内置规则）、应用商店、计划任务、文件管理、进程管理、备份中心、域名邮箱、用户管理、设置。

三档视口页面级横向溢出检测：**全部无溢出**；弹窗有全局 `max-width: 94vw` 兜底（`web/src/styles/index.css`）。
