# kypanel 多域名邮箱模块 · 分步开发文档

> 目标：在 kypanel 面板内自研一套**多域名邮箱系统**，可收可发、带 REST API（对标新浪/腾讯企业邮）。
> 要求：**纯 Go 自研**（不依赖 emersion/go-smtp、go-imap 等协议库），整合进**面板左侧菜单**，做得完善。
> 状态基线：源码已备份（git tag `pre-mail-module`，物理 zip `kypanel_src_backup_20260909_2048_pre-mail.zip`）。

---

## 一、总体架构

邮箱系统作为面板**进程内**的模块（与数据库/网站模块同级），共用面板的
`gin` 路由、`gorm`/SQLite、JWT 鉴权、前端 Vue 基建。不改动面板主进程启动方式。

```
                ┌─────────────── kypanel 面板 ───────────────┐
  外部来信(MX)──►│  SMTP 服务(自研)                            │
  SMTP客户端 ───►│    ├ 会话/命令解析                          │
  HTTP API ─────►│    ├ 收件 → 多域路由 → maildir 落盘          │
  面板 WebMail ─►│    ├ 发件 → 本域/外发(MX+DKIM)队列           │
                │  /api/mail/* (gin)  管理+对外邮件API         │
                │  MariaDB: 账号/域名/配额/APIKey 元数据        │
                │  磁盘:    邮件正文 maildir 文件               │
                └─────────────────────────────────────────────┘
```

**分层原则（纯自研）**
- 协议解析（SMTP 命令 / MIME 格式 / DKIM 签名）全部自己写，不引第三方库。
- 用 Go 标准库：`net`（TCP/TLS）、`crypto/tls`、`crypto/rsa|sha256`（DKIM）、`mime`/`net/mail`/`encoding/base64`（辅助）、`io`/`bufio`。
- 存储：正文用 **maildir**（每封一个文件），元数据（账号/域名/索引/配额）用面板现有 SQLite。

---

## 二、目录与文件规划

### 后端（新增到现有包结构）
```
internal/model/mail.go              # GORM 模型：MailDomain/Mailbox/MailAlias/MailMessageMeta/MailOutbox/MailApiKey
internal/service/
  mail_store.go                     # 增删查 Domain/Mailbox/Alias/APIKey + maildir 布局工具
  mail_mime.go                      # 【自研】RFC2822/2045：MIME 消息解析与构造（header/base64/qp/multipart）
  mail_smtp.go                      # 【自研】SMTP server：命令状态机、会话、EHLO/STARTTLS/AUTH/MAIL/RCPT/DATA
  mail_smtp_delivery.go             # 收件投递（本地路由到 maildir）+ 外发（MX 查询、SMTP 客户端、重试队列、退信）
  mail_dkim.go                      # 【自研】DKIM-Signature 生成与校验
  mail_core.go                      # 路由/校验/配额编排 + 统一入口
router/mail.go                      # setupMailRoutes(authGroup)：/api/mail/* （管理 + 对外 API）
```

### 前端（新增到现有基建）
```
web/src/api/mail.js                 # axios 封装
web/src/views/mail/
  MailDomains.vue                   # 域名邮箱（域名/配额/SPF/DKIM 引导）
  MailAccounts.vue                  # 邮箱账号（增删/启停/改密/容量）
  MailInbox.vue                     # 收件箱（网页读信）
  MailCompose.vue                   # 写信/发信
  MailApiTokens.vue                 # API 令牌
```

---

## 三、分步开发与验收

### P1 — 数据模型 + 面板「邮件」菜单 + 域名/账号管理
> 本步纯管理端，不涉及收发电邮，风险低、先让模块在面板可见可用。

1. **建模型** `internal/model/mail.go`
   - `MailDomain{ id, domain(唯一), enabled, max_accounts, max_storage_mb, dkim_private_key(加密存), spf/dkim/dmarc 引导状态, created }`
   - `Mailbox{ id, domain_id, address(name@domain 唯一), password_hash, enabled, storage_used, quota_mb, forward_to, auto_reply_on, auto_reply_text }`
   - `MailAlias{ id, domain_id, source, target }`
   - `MailOutbox{ id, from, to, subject, message_file, retry, last_try, next_try, status, dkim_signed }`
   - `MailApiKey{ id, domain_id, name, key_hash, enabled }`
   - 在 `internal/model/model.go` 的 `AutoMigrate` 列表追加这些模型。
   - 账号密码哈希复用面板现有 bcrypt 方式；APIKey 只存哈希。

2. **建管理接口** `internal/service/mail_store.go` + `internal/router/mail.go`
   - `setupMailRoutes(authGroup)` 挂到 `router.go` 的 authGroup（权限码 `mail`）。
   - REST：
     - `GET/POST /api/mail/domains`、`DELETE /api/mail/domains/:id`
     - `GET/POST /api/mail/accounts?domain_id=`、`PATCH/DELETE .../accounts/:id`
     - `GET/POST /api/mail/api-tokens`、`DELETE .../api-tokens/:id`
   - 沿用面板现有响应包装 `utils.Ok/Fail` 与 `recordOpForCtx` 审计。

3. **前端接入左侧菜单**
   - `TheSidebar.vue` 的 `menu` 加**顶级「邮件」分组**（带 children 子项）：域名邮箱 / 邮箱账号 / 收件箱 / 写信 / API 令牌。前端模板已支持 `children` 手风琴。
   - 路由注册 `web/src/router/routes/*`（或 index.js）新增 `/mail/*`。
   - `web/src/api/mail.js` 封装请求。
   - 视图：`MailDomains.vue`、`MailAccounts.vue` 先做（列表 + 新增弹窗 + 启停/删除）。

4. **验收**
   - 面板左侧出现「邮件」菜单，能展开子项。
   - 能新增一个域名、在其下新建邮箱账号（本地 SQLite 落库）。
   - 域名/账号列表能展示、能删除、账号能启停。

---

### P2 — 【自研】SMTP 收发核心 + maildir + 多域路由
> 本步是邮局的"心脏"。开始写协议前先明确：SMTP 是逐行文本协议，核心在**命令状态机 + 会话缓冲**。

1. **maildir 布局**（纯文件）`mail_store.go`
   ```
   <DataDir>/mail/<domain>/<user>/cur/、new/、tmp/
   每封邮件 = 一个文件（落盘前写 tmp，改名入 new，读后移 cur）
   ```
   - `DeliverMessage(domain, user, rawBytes)` 实现原子落盘。

2. **【自研】MIME 解析** `mail_mime.go`
   - `ParseMessage([]byte) -> Header + Body`；解析 From/To/Subject/Date/Message-ID 等（处理 RFC2047 编码、多行折叠）。
   - `EncodeMessage(...)` 构造一封（收本地先可用纯文本，后续再加 multipart/附件）。
   - 验收用 Go `net/mail` 仅做**测试对照**（不在生产依赖里）。

3. **【自研】SMTP server** `mail_smtp.go`
   - 用标准库 `net.Listen` 起 TCP 服务（开发先监听本地随机端口测，正式再绑 25/465/587）。
   - 自实现状态机：
     - 连接 → `220 greeting` → 客户端发命令
     - `EHLO/HELO` → 返回能力（含 `AUTH PLAIN LOGIN`、`STARTTLS`）
     - `MAIL FROM` / `RCPT TO` → 记录信封
     - `DATA` → 收正文到 `\r\n.\r\n` 结束 → 触发投递
     - 处理 `QUIT`、`RSET`、`NOOP`；命令超时与大小上限
   - 会话逐命令 `bufio` 读行、大写化命令字、严格解析。
   - 收件投递：解析收件人 → 查 `MailDomain` + `Mailbox` → 匹配则 `DeliverMessage` → `250`；不匹配 → `550`。
   - **发件人限制（防开放中继）**：仅允许本站域名账号发送（`MAIL FROM` 域必须属于某 `MailDomain`，或连接已 AUTH）。

4. **自研 SMTP 客户端（外发）+ 队列**（可在 P4 细化，此处先占位实现单次直发）
   - `deliverExternal(mail)`：`net.LookupMX(目标域)` → 逐 MX 尝试建连 → SMTP 客户端对话发信。

5. **验收**
   - 用命令行工具（如 `nc`/自写小工具或 `curl --url smtp://`）本地连 SMTP 端口，发一封给本站账号 → 邮件落到该账号 maildir。
   - 用 Go 测试写收发往返用例（起 server → client 发 → 断言 maildir 有该信且头部正确）。
   - 用一个真实邮箱做外发对测（本机 → 自己邮箱收），验证能发出去。

---

### P3 — WebMail 收件箱 / 写信前端
1. 后端读信接口 `mail_store.go` 加：
   - `ListMessages(domain,user,page)`：扫 maildir，返回 主题/发件人/时间/是否已读/uid。
   - `GetMessage(domain,user,uid)`：读某封全文 + 标记已读。
   - `DeleteMessage / SetFlag`。
   - `Compose`：调用 MIME 构造 + 走 P2 的发信/外发。
2. 前端 `MailInbox.vue`（收件列表 + 读信详情 + 删除 + 刷新）、`MailCompose.vue`（收件人/主题/正文/发送）。
3. **验收**：P2 给某账号发封测试信 → 网页收件箱能看到、能打开读；网页写信发给本站另一账号能被对方收到；写给外域能发。

---

### P4 — DKIM 签名 + MX 外发队列 + SPF/DKIM/DMARC 引导
1. **【自研】DKIM** `mail_dkim.go`
   - 生成密钥：`rsa.GenerateKey`（2048）→ 存 `MailDomain.dkim_private_key`；同时可导出公钥/公钥格式文案给用户在 DNS 配 `TXT k=rsa; p=...`。
   - 签名：对出站邮件按 DKIM 规则挑 header（from/subject/date/message-id...）做 `rsa.SHA256` 签名 → 插 `DKIM-Signature` 头。
   - 校验（可选，先只做签名，校验留给需要时）。
2. **外发队列 + 重试**：`MailOutbox` 表；发送失败按 `next_try` 退避重试；超过次数生成**退信**回原发件人。
3. **DNS 引导**：`/api/mail/domains/:id/dns-guide` 返回该域需配的 MX/SPF/DKIM/DMARC 记录文案；可选"自检"（本域发测试信看是否通过）。
4. **验收**：域下账号发出的信带 DKIM-Signature 且能通过第三方 DKIM 校验器；SPF/DKIM/DMARC 记录在 DNS 配好后被主流服务识别。

---

### P5 — REST API（对外）+ 配额 + 别名/自动回复 + 审计
1. **对外邮件 API**（对标新浪/腾讯企业邮，用 `MailApiKey` 鉴权）
   - `POST /api/mail/send`：`{ api_key, from, to[], subject, text|html }` → 走发信核心。
   - `GET /api/mail/messages`：拉某账号收件箱（供第三方轮询）。
   - API Key 绑定 `domain_id`，只能操作本域，隔离租户。
2. **配额**：收信前检查 `Mailbox.storage_used` / 域 `max_storage_mb`，超限 `552`/拒收；发信同理（若设）。
3. **别名 / 自动回复**：`MailAlias` 转发；`Mailbox.auto_reply_*` 收到未缺席回复时自动回复。
4. **审计**：进出信记录接到现有操作日志/独立投递日志。
5. **验收**：用 curl 带 API key 调 `/api/mail/send` 能发信；跨域账号互不可见；别名收信正确转发；自动回复生效。

---

### P6（可选）— IMAP/POP3 给第三方客户端
> WebMail 走内部 API，若需让手机邮件App / Outlook 连接，再补自研 IMAP server。工作量较大，另行规划。

---

## 四、通用工程约定（沿用面板现有风格）
- **后端**：新增 router 一律在 `authGroup` 里 `setupMailRoutes`，权限 `mail`；响应用 `utils.Ok/Fail`；写操作 `recordOpForCtx` 审计；SQL 走 gorm 参数化。
- **模型**：新表加入 `model.go AutoMigrate`。
- **前端**：axios 用 `web/src/utils/request`；新页面在 `TheSidebar` menu + router 注册；列表页遵守"卸载清定时器 + try/finally 复位 loading"等既有规范。
- **安全**：密码/私钥/APIKey 一律不落明文（哈希/加密存储）；发信账号必须认证，杜绝开放中继。
- **菜单权限**：默认给超管/授予 `mail` 权限的账号可见。

## 五、回退
- 任何阶段改出问题：`git checkout pre-mail-module -- <文件>` 局部还原；或整体 `git checkout pre-mail-module`。
- 完整源码基线：物理 zip `kypanel_src_backup_20260909_2048_pre-mail.zip`。
