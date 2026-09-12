# 域名邮箱 IMAP / POP3 接入方案（研究稿）

> 结论先行：**IMAP 可以自研，且不需要改动现有存储结构**——把 `mail_messages.id` 直接当作 IMAP UID 用，
> UIDVALIDITY 由账号创建时间派生，配合新增一个 flags 列即可。建议先做 **POP3（约 400 行）** 打通手机客户端，
> 再做 **IMAP 最小可用集（约 2000 行）**。

---

## 一、为什么现在不能连客户端

当前邮箱模块的对外通道只有：
- **收信**：自研 SMTP server（25 / 587 / 2525 / 465），投递到 maildir
- **读信**：面板 WebMail + 门户 WebMail，走面板自己的 HTTP API（`/api/mail-portal/*`）

也就是说，**邮件存在服务器上，但没有暴露标准的邮件访问协议**，所以 Outlook / Foxmail / iOS 邮件
只能通过网页（门户）收发，无法配置账号。IMAP（993/143）与 POP3（995/110）就是补这一块。

---

## 二、协议要点（自研前必须搞清楚的 6 件事）

| # | 要点 | 说明 | 难度 |
|---|---|---|---|
| 1 | **行协议 + 字面量（literal）** | 客户端可发 `LOGIN {5}\r\nadmin {8}\r\npassword`，服务端先回 `+ OK` 再收原始字节，**不能按行读** | 中 |
| 2 | **Tag 前缀** | 每条命令以客户端 tag 开头（`a001 LOGIN ...`），所有回复（含多行）必须以同一 tag 收尾 `<tag> OK/NO/BAD` | 低 |
| 3 | **UID 与 UIDVALIDITY** | UID 在一个 mailbox 内单调递增且永不复用；UIDVALIDITY 变化时客户端必须丢弃本地缓存 | **高** |
| 4 | **Flag 与 \Deleted/EXPUNGE** | `\Seen \Answered \Flagged \Deleted \Draft`；`STORE +FLAGS \Deleted` 后要等 `EXPUNGE` 才真正删 | 中 |
| 5 | **BODYSTRUCTURE / ENVELOPE** | 客户端靠它渲染列表和附件（不下载整封）；需从 MIME 树生成嵌套 S 表达式 | **高** |
| 6 | **FETCH 分段** | `BODY.PEEK[TEXT]` / `BODY[1.2]` / `RFC822.SIZE` / `INTERNALDATE`，且 `PEEK` 不能置已读 | 中 |

> 最容易踩坑的是 **1（字面量）** 和 **5（BODYSTRUCTURE）**，其余都是状态机 + SQL 映射。

---

## 三、与现有存储的映射（关键设计，零结构改造）

### 3.1 UID：直接用 `mail_messages.id`

IMAP 只要求「**同一 mailbox 内唯一、单调递增、不复用**」，并没有要求连续。
现有 `MailMessage.ID` 是 SQLite 自增主键，天然满足：

```
UID          = mail_messages.id          （同一 mailbox 内必然唯一且递增，删除后留空洞是允许的）
UIDNEXT      = SELECT MAX(id)+1 FROM mail_messages WHERE mailbox_id = ?
UIDVALIDITY  = mailbox.created_at 的 unix 秒（账号重建 → 新时间 → 新的 UIDVALIDITY，符合语义）
```

**不需要新增映射表、不需要重排历史邮件**，这是这套存储结构天然具备的优势。

### 3.2 Flag：加一列即可

```go
// internal/model/mail.go —— MailMessage 增加
Flags string `gorm:"size:128;default:''" json:"flags"` // 逗号分隔：answered,flagged,deleted,draft（seen 复用已有字段）
```

- `\Seen` ↔ 现有 `Seen bool`（WebMail 已读 / IMAP `STORE` 双向同步）
- `\Deleted` → 置位 flags 里的 `deleted`；`EXPUNGE` 时调用已有 `deleteMailboxMessage()` 真正删文件
- `latest` → 直接读 `RawSize`、`Date`、`FromAddr`

### 3.3 Folder → 现有 folder 字段

| IMAP 邮箱名 | 现有 `folder` 值 |
|---|---|
| INBOX | `inbox` |
| Sent | `sent` |
| Drafts | `drafts` |
| Trash | （新增 `trash`，或 DELETE 直接删） |

`LIST "" "*"` 直接按账号实际存在的 folder 值返回。

### 3.4 报文读取

`FETCH BODY[]` = 读 `maildir/{cur,new}/<filename>`，**现有 `readMailboxMessage()`、`extractBodies()`、
`extractAttachments()` 全部可复用**；`BODYSTRUCTURE` 复用 `splitMultipart()` 递归生成。

---

## 四、建议的实现路径

### 方案 A（推荐）：自研最小 IMAP，纯 Go 标准库

与项目「不引第三方协议库」的既有约定一致（SMTP/MIME/DKIM 都是自研）。

命令子集（覆盖 95% 客户端行为）：

```
必做：CAPABILITY  NOOP  LOGOUT  LOGIN  AUTHENTICATE PLAIN
      LIST  LSUB  SELECT  EXAMINE  STATUS
      FETCH（UID/RFC822.SIZE/INTERNALDATE/FLAGS/ENVELOPE/BODYSTRUCTURE/BODY[...]）
      STORE（+FLAGS/-FLAGS/FLAGS，含 .SILENT）
      SEARCH（ALL/UNSEEN/SEEN/FROM/SUBJECT/SINCE/BEFORE/LARGER/SMALLER/UID）
      UID FETCH / UID STORE / UID SEARCH / UID COPY
      APPEND（上传草稿/已发送）  EXPUNGE  CLOSE  CHECK  CREATE/DELETE/RENAME
可选：IDLE（长连接推送新邮件，先不做也能用）  STARTTLS(143)  TLS(993)
```

**文件结构（与现有约定一致）**

```
internal/service/mail_imap.go        # TCP 监听 + TLS 升级 + tag/字面量解析 + 命令分发
internal/service/mail_imap_parse.go  # IMAP 语法：atom / quoted / literal / 括号列表
internal/service/mail_imap_fetch.go  # ENVELOPE / BODYSTRUCTURE / BODY[section] 生成
internal/service/mail_imap_store.go  # SELECT/FETCH/STORE/EXPUNGE 与 maildir+SQL 映射
internal/service/mail_imap_search.go # SEARCH 条件 → SQL
internal/service/mail_pop3.go        # POP3（可独立先做）
```

**核心骨架示意**

```go
func handleImapConn(conn net.Conn, implicitTLS bool) {
    // 1. 可选隐式 TLS（993）
    // 2. 220 问候
    // 3. 循环：读 tag + 命令（注意 literal → 回 "+ " 再收 n 字节）
    // 4. 按命令分发；每条回复以 "<tag> OK/NO/BAD" 结束
}

// UID 分配：无需额外分配，直接用主键
func imapUidNext(mailboxID uint) uint32 {
    var max uint
    model.DB.Model(&model.MailMessage{}).Where("mailbox_id = ?", mailboxID).Select("COALESCE(MAX(id),0)").Scan(&max)
    return uint32(max) + 1
}
```

### 方案 B（不推荐）：引入 `emersion/go-imap`

- 优点：命令/字面量解析现成，省 1~2 天
- 缺点：**违背项目既定约定**（此前已专门移除 `gorilla/websocket`、`gopsutil` 等依赖改为自研）；
  且它只提供协议框架，`BODYSTRUCTURE`/存储映射同样要自己写，实际节省有限

---

## 五、分步实施与验收

### P1 — POP3（1 天，收益最快）
- `USER/PASS/STAT/LIST/RETR/TOP/DELE/QUIT/UIDL`，默认 `995`（隐式 TLS）+ `110`（STLS）
- 认证复用 `Mailbox` 的 bcrypt 密码
- 验收：iOS / Foxmail 添加 POP3 账号能收信、能下载附件；`DELE` 后 `QUIT` 才真正删除

### P2 — IMAP 只读最小集（2~3 天）
- `CAPABILITY LOGIN LIST SELECT FETCH(FLAGS/INTERNALDATE/RFC822.SIZE/ENVELOPE/BODY[]) LOGOUT` + `993/143 STARTTLS`
- 验收：Thunderbird 配 IMAP 能列出文件夹、看到邮件列表和正文

### P3 — IMAP 读写与附件（2 天）
- `BODYSTRUCTURE` + `BODY[1.2]` 分段 + `STORE` flags + `EXPUNGE` + `UID FETCH` + `APPEND`
- `\Seen` 与 WebMail 双向同步
- 验收：客户端能标已读/未读、下载单个附件、删除邮件；网页端同步显示

### P4 — 搜索与 IDLE（1~2 天）
- `SEARCH` 子集 → SQL；`IDLE` 用 DB 轮询（1~2s）推送 `EXISTS`，新邮件即时提醒
- 验收：客户端搜索主题/发件人；收到新邮件后客户端自动出现

---

## 六、安全要点（与现有 SMTP 保持一致）

1. **必须 TLS**：993/995 隐式 TLS，143/110 提供 STARTTLS；证书复用
   `mailTLSCertificate()`（面板证书优先，否则自签）。
2. **认证**：复用 `Mailbox.PasswordHash`（bcrypt）+ `PasswordHash` 校验函数；
   `AUTHENTICATE PLAIN` 的 base64 解析与 SMTP 侧同一套代码。
3. **限速**：复用 `AllowMailConnection(ip)`（每 IP 每分钟上限）。
4. **越权**：所有命令操作的 `mailbox_id` 必须来自登录账号，禁止跨账号 FETCH
   （与门户 token 同样的隔离原则）。
5. **大小限制**：单条命令/字面量上限（如 10MB），防止内存打爆。

---

## 七、工作量与风险

| 项 | 估计 |
|---|---|
| POP3 全套 | 约 400 行，1 天 |
| IMAP P2（只读） | 约 900 行，2~3 天 |
| IMAP P3（读写+附件） | 约 800 行，2 天 |
| IMAP P4（搜索+IDLE） | 约 400 行，1~2 天 |
| **合计** | **约 2500 行，6~8 天** |

**主要风险**
1. `BODYSTRUCTURE` 生成错误 → 客户端能连上但列表/附件异常（需按 RFC 3501 逐字段对照，用 Thunderbird 实测）
2. 字面量 + 流水线（pipelining）解析 → 连接卡死（需专门写解析单测，参照本次 SMTP 的测试方式）
3. 并发访问同一 mailbox（WebMail 删除 vs 客户端 FETCH）→ 文件已删索引还在
   （已有 `readMailboxMessage` 的「文件缺失」兜底，需明确返回 `NO` 而不是崩溃）

---

## 八、建议

1. **先做 POP3**：一天内让手机/客户端能收信，投入产出比最高；多数用户只关心「能不能收信」。
2. 再做 **IMAP P2+P3**：覆盖 Outlook/Thunderbird 的完整收发体验。
3. IMAP 与 POP3 都挂在现有 `StartMailSmtpServer()` 同级启动，端口复用现有的
   `PANEL_MAIL_*_PORTS` 环境变量模式，保持不变。
