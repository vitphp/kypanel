package service

// ==================== 迁出到对端面板 ====================
// 本面板（kypanel）作为源端，把网站/数据库/FTP 迁移到目标对端面板：
//   1. 本机打包生成迁移包（复用 ExportMigration）
//   2. 调对端面板 API 创建网站（AddSite）
//   3. 上传网站文件包并解压到站点目录
//   4. 调对端面板 API 创建数据库（AddDatabase）+ 导入 SQL
//   5. 调对端面板 API 创建 FTP 账号（AddUser）

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/model"
)

// BTExportRequest 迁出到对端面板的请求
type BTExportRequest struct {
	BTURL       string   `json:"bt_url"`       // 对端面板地址，如 http://1.2.3.4:8888
	BTSK        string   `json:"bt_sk"`        // 对端面板 API 接口密钥
	Sites       []string `json:"sites"`        // 选中的网站
	Databases   []string `json:"databases"`    // 选中的数据库
	FTPs        []string `json:"ftps"`         // 选中的 FTP
	DBPassword  string   `json:"db_password"`  // 数据库新密码（空则沿用源端）
	FTPPassword string   `json:"ftp_password"` // FTP 新密码（空则随机生成）

	// 冲突处理决策（key 为网站/数据库/FTP 名称，true=覆盖，false=跳过）。
	// 由前端在迁移前检测后弹窗确认生成。
	SiteOverwrite    map[string]bool `json:"site_overwrite"`
	DatabaseOverwrite map[string]bool `json:"database_overwrite"`
	FtpOverwrite      map[string]bool `json:"ftp_overwrite"`
}

// StartExportToBT 开始迁出到对端面板（异步，返回任务 ID）
func StartExportToBT(req BTExportRequest) (string, error) {
	if req.BTURL == "" || req.BTSK == "" {
		return "", errors.New("请填写对端面板地址和 API 密钥")
	}
	if len(req.Sites) == 0 && len(req.Databases) == 0 && len(req.FTPs) == 0 {
		return "", errors.New("请至少选择一个迁移对象")
	}
	id := "btexport-" + time.Now().Format("20060102150405")
	task := newImportTask(id, TaskKindExport)
	go runExportToBT(task, req)
	return id, nil
}

func runExportToBT(t *ImportTask, req BTExportRequest) {
	defer func() {
		if r := recover(); r != nil {
			t.mu.Lock()
			t.Status = "failed"
			t.Error = fmt.Sprintf("%v", r)
			t.mu.Unlock()
			t.logf("迁移失败: %v", r)
		}
	}()

	// 1. 本机打包
	t.logf("正在打包本机网站/数据库...")
	exp, err := ExportMigration(req.Sites, req.Databases, req.FTPs)
	if err != nil {
		t.fail("打包失败: " + err.Error())
		return
	}
	pkgPath := migrateExportFile(exp.ID)
	defer os.Remove(pkgPath)
	t.logf("打包完成：网站 %d 个、数据库 %d 个、FTP %d 个",
		len(exp.Manifest.Sites), len(exp.Manifest.Databases), len(exp.Manifest.FTPs))

	// 解压迁移包到工作目录
	workDir := filepath.Join(migrateRoot(), "bt-work-"+exp.ID)
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)
	if err := ungzTar(pkgPath, workDir); err != nil {
		t.fail("解压迁移包失败: " + err.Error())
		return
	}

	// 2. 连接对端面板
	t.logf("正在连接对端面板 %s ...", req.BTURL)
	bt := NewBTClient(req.BTURL, req.BTSK)
	existing, err := bt.SiteList()
	if err != nil {
		t.fail("连接对端面板失败: " + err.Error())
		return
	}
	// name → {id, webname}，用于覆盖时删除
	type btSiteEntry struct{ id, webname string }
	btSiteMap := map[string]btSiteEntry{}
	for _, s := range existing {
		n, _ := s["name"].(string)
		if n == "" {
			continue
		}
		id := fmt.Sprintf("%v", s["id"])
		btSiteMap[n] = btSiteEntry{id: id, webname: btParseWebname(s)}
	}
	// 不再打印「当前 N 个网站」，避免远程拉取后被前端误读为多余信息
	t.logf("对端面板连接成功")

	// 3. 创建网站 + 上传文件 + 应用站点级配置
	// 注：单个网站失败不中断整体，让所有失败都能在前端展示，
	//     整体 Status 在末尾根据 Items 是否有 failed 决定。
	for _, ms := range exp.Manifest.Sites {
		domains := ms.Domain
		if ms.Domains != "" {
			domains += "," + ms.Domains
		}
		phpVer := ms.PhpVersion
		if phpVer == "" {
			phpVer = "74"
		}
		// 目标建站目录沿用源站根目录，保证迁移前后网站文件夹路径一致
		path := ms.Root
		if path == "" {
			path = filepath.Join("/www/wwwroot", ms.Name)
		}

		// 3.1 处理对端已存在的同名网站（按用户选择：跳过 / 覆盖删除）
		if info, exists := btSiteMap[ms.Name]; exists {
			if !req.SiteOverwrite[ms.Name] {
				t.logf("网站 %s 在对端面板已存在，按选择跳过", ms.Name)
				t.addItem("site", ms.Name, "skipped", "对端面板已存在，按选择跳过")
				continue
			}
			t.logf("网站 %s 在对端面板已存在，先删除后重建（覆盖）...", ms.Name)
			if _, err := bt.DeleteSite(info.id, info.webname); err != nil {
				t.logf("覆盖网站 %s 失败（请到对端面板手动删除该网站后重试）: %v", ms.Name, err)
				t.addItem("site", ms.Name, "failed", "删除对端旧网站失败："+err.Error())
				continue
			}
		}

		// 3.2 在对端面板创建网站
		t.logf("在对端面板创建网站 %s（域名 %s，PHP %s，目录 %s）...", ms.Name, domains, phpVer, path)
		addRes, err := bt.AddSite(ms.Name, domains, path, phpVer)
		if err != nil {
			t.logf("创建网站 %s 失败: %v", ms.Name, err)
			t.addItem("site", ms.Name, "failed", "创建网站失败："+err.Error())
			continue
		}
		// 记下新站点 ID，后续设置运行目录需要（不同版本字段名略有差异，做多候选解析）
		siteID := ""
		for _, k := range []string{"siteId", "site_id", "id"} {
			if v, ok := addRes[k]; ok && v != nil {
				if s := strFromAny(v); s != "" && s != "0" {
					siteID = s
					break
				}
			}
		}
		// 宝塔 webname（用于伪静态文件名，不同版本字段名差异也较大）
		btWebname := ""
		for _, k := range []string{"siteName", "webname", "name"} {
			if v, ok := addRes[k]; ok && v != nil {
				if s := strFromAny(v); s != "" {
					btWebname = s
					break
				}
			}
		}
		t.logf("网站 %s 创建成功（ID %s，webname %s）", ms.Name, siteID, btWebname)
		t.addItem("site", ms.Name, "success", "创建成功（ID "+siteID+"）")

		// 3.3 上传网站文件（无文件包时跳过）
		src := filepath.Join(workDir, "sites", ms.Name, "wwwroot.tar.gz")
		if !fileExists(src) {
			t.logf("网站 %s 无文件包，跳过文件上传", ms.Name)
		} else {
			remote := "/www/backup/migrate-" + ms.Name + ".tar.gz"
			t.logf("上传网站 %s 文件包...", ms.Name)
			lastProg := time.Now()
			if err := bt.Upload(src, remote, func(done, total int64) {
				if time.Since(lastProg) < 2*time.Second {
					return
				}
				lastProg = time.Now()
				pct := float64(0)
				if total > 0 {
					pct = float64(done) / float64(total) * 100
				}
				t.logf("上传网站 %s 文件包中... %.0f%%（%d/%d MB）", ms.Name, pct, done/1024/1024, total/1024/1024)
			}); err != nil {
				t.logf("上传网站 %s 文件失败: %v", ms.Name, err)
				t.addItem("site-file", ms.Name, "failed", "上传文件失败："+err.Error())
				continue
			}
			t.logf("解压网站 %s 文件到站点目录...", ms.Name)
			if err := bt.Unzip(remote, path); err != nil {
				t.logf("解压网站 %s 文件失败: %v", ms.Name, err)
				t.addItem("site-file", ms.Name, "failed", "解压文件失败："+err.Error())
				continue
			}
			btDeleteRemoteFile(bt, remote)
			t.logf("网站 %s 文件恢复完成", ms.Name)
		}

		// 3.4 站点级配置：运行目录 / 伪静态 / SSL
		// applyBTSiteSettings 内部已记录详细日志，失败时通过返回 false 报告给外层记录 item
		configOK := applyBTSiteSettings(t, bt, &ms, siteID, btWebname)
		if !configOK {
			t.addItem("site-config", ms.Name, "failed", "伪静态/运行目录/SSL 至少一项失败，请查看上方日志详情")
		}
	}

	// 4. 数据库
	dbsExist := map[string]string{}
	if dbs, err := bt.DatabaseList(); err == nil {
		for _, d := range dbs {
			// 注意：宝塔 JSON 里 id 是数字（解码后为 float64），必须用 strFromAny 而不是 (string) 断言
			if id := strFromAny(d["id"]); id != "" {
				if n, _ := d["name"].(string); n != "" {
					dbsExist[n] = id
				}
			}
		}
	}
	for _, db := range exp.Manifest.Databases {
		pass := req.DBPassword
		if pass == "" {
			pass = db.Password
		}
		if pass == "" {
			pass = randomPassword(16)
		}

		// 4.1 处理对端已存在的同名数据库
		if dbID, exists := dbsExist[db.Name]; exists {
			if !req.DatabaseOverwrite[db.Name] {
				t.logf("数据库 %s 在对端面板已存在，按选择跳过（含 SQL 导入）", db.Name)
				t.addItem("database", db.Name, "skipped", "对端面板已存在，按选择跳过（含 SQL 导入）")
				continue
			}
			t.logf("数据库 %s 在对端面板已存在，先删除后重建（覆盖）...", db.Name)
			if _, err := bt.DeleteDatabase(dbID, db.Name); err != nil {
				t.logf("覆盖数据库 %s 失败（请到对端面板手动删除该数据库后重试）: %v", db.Name, err)
				t.addItem("database", db.Name, "failed", "删除对端旧库失败："+err.Error())
				continue
			}
			delete(dbsExist, db.Name)
		}

		// 4.2 在对端面板创建数据库
		t.logf("在对端面板创建数据库 %s ...", db.Name)
		if _, err := bt.AddDatabase(db.Name, db.User, pass); err != nil {
			t.logf("提示：若目标面板未安装/未启动 MySQL，请先到对端面板「软件商店」安装 MySQL 后再重试")
			t.logf("创建数据库 %s 失败: %v", db.Name, err)
			t.addItem("database", db.Name, "failed", "创建数据库失败："+err.Error())
			continue
		}
		t.logf("数据库 %s 创建成功", db.Name)
		t.addItem("database", db.Name, "success", "创建成功")

		// 4.3 导入 SQL
		sqlLocal := filepath.Join(workDir, "databases", db.Name+".sql.gz")
		if !fileExists(sqlLocal) {
			t.logf("数据库 %s 无备份文件，跳过导入", db.Name)
			t.addItem("database-data", db.Name, "skipped", "无备份文件")
			continue
		}
		// 解压为明文 SQL 再上传（对端面板导入接口通常要求明文）
		plain := filepath.Join(workDir, "databases", db.Name+".sql")
		if err := gunzipFile(sqlLocal, plain); err != nil {
			t.logf("数据库 %s 备份解压失败: %v", db.Name, err)
			t.addItem("database-data", db.Name, "failed", "备份解压失败："+err.Error())
			continue
		}
		// SQL 兼容性处理：源库若为 MySQL 8.0.30+（uca1400）或 8.0（0900），
		// 导出的 collation（如 utf8mb3_uca1400_ai_ci / utf8mb4_0900_ai_ci）对端低版本
		// MySQL/MariaDB 不认识，会导致 CREATE TABLE 失败、后续 INSERT 全部报
		// "Table doesn't exist"（实测对端导入日志 ERROR 1273 Unknown collation）。
		if err := normalizeSQLCompat(plain); err != nil {
			t.logf("数据库 %s SQL 兼容性处理失败: %v", db.Name, err)
			t.addItem("database-data", db.Name, "failed", "SQL 兼容性处理失败："+err.Error())
			continue
		}
		remoteSQL := "/www/backup/database/migrate-" + db.Name + ".sql"
		t.logf("上传数据库 %s 备份...", db.Name)
		if err := bt.Upload(plain, remoteSQL, nil); err != nil {
			t.logf("上传数据库 %s 备份失败: %v", db.Name, err)
			t.addItem("database-data", db.Name, "failed", "上传 SQL 备份失败："+err.Error())
			continue
		}
		dbID := dbsExist[db.Name]
		if dbID == "" {
			if dbs, err := bt.DatabaseList(); err == nil {
				for _, d := range dbs {
					if n, _ := d["name"].(string); n == db.Name {
						dbID = strFromAny(d["id"])
					}
				}
			}
		}
		t.logf("导入数据库 %s ...（dbID=%s）", db.Name, dbID)
		// 重要：导入失败必须记为 failed，不能仅 log 后继续——这是用户最敏感的「数据库空了」问题
		imported := false
		lastErr := error(nil)
		// 优先「服务端路径」方式（宝塔通用，老版/新版 InputSql 均支持，SQL 已上传到对端）。
		// 部分新版面板对 file 路径解析不同（相对名报「导入路径不存在!」、绝对路径报
		// 「数据库导入包含异常」），此时回退「本地上传」multipart 方式。
		if _, err := bt.ImportDatabase(dbID, db.Name, remoteSQL); err == nil {
			imported = true
		} else {
			lastErr = fmt.Errorf("路径方式: %v", err)
			t.logf("按路径导入失败（%v），改用「本地上传」multipart 方式重试...", err)
			if _, err2 := bt.ImportDatabaseFile(dbID, db.Name, plain); err2 == nil {
				imported = true
			} else {
				lastErr = fmt.Errorf("%v（本地上传方式：%v）", lastErr, err2)
			}
		}
		if imported {
			t.logf("数据库 %s 导入完成", db.Name)
			t.addItem("database-data", db.Name, "success", "SQL 导入完成")
		} else {
			// 诊断：读取对端导入状态与日志，帮助定位失败原因
			if st, err := bt.ImportStatus(); err == nil {
				t.logf("对端导入状态：%v", st)
			}
			if lg, err := bt.ImportLog(); err == nil && strings.TrimSpace(lg) != "" {
				t.logf("对端导入日志：%s", truncateLog(strings.TrimSpace(lg), 600))
			}
			// 保留上传好的 SQL，方便用户到对端面板「数据库 → 导入」手动选
			t.logf("数据库 %s 自动导入失败（已上传到 %s，可到对端面板「数据库 → 导入」手动选择该文件）：%v", db.Name, remoteSQL, lastErr)
			t.addItem("database-data", db.Name, "failed", "SQL 导入失败："+fmt.Sprintf("%v", lastErr)+"（SQL 已上传到 "+remoteSQL+"，可到对端面板手动导入）")
		}
		btDeleteRemoteFile(bt, remoteSQL)
	}

	// 5. FTP
	btFtpExist := map[string]bool{}
	if ftpList, err := bt.FtpUserList(); err == nil {
		for _, u := range ftpList {
			if n, _ := u["username"].(string); n != "" {
				btFtpExist[n] = true
			}
		}
	}
	for _, f := range exp.Manifest.FTPs {
		if btFtpExist[f.Username] {
			if !req.FtpOverwrite[f.Username] {
				t.logf("FTP 账号 %s 在对端面板已存在，按选择跳过", f.Username)
				t.addItem("ftp", f.Username, "skipped", "对端面板已存在，按选择跳过")
				continue
			}
			t.logf("FTP 账号 %s 在对端面板已存在，先删除后重建（覆盖）...", f.Username)
			if _, err := bt.DeleteFtpUser(f.Username); err != nil {
				t.logf("覆盖 FTP 账号 %s 失败: %v", f.Username, err)
				t.addItem("ftp", f.Username, "failed", "删除对端旧账号失败："+err.Error())
				continue
			}
		}
		pass := req.FTPPassword
		if pass == "" {
			pass = randomPassword(12)
		}
		t.logf("在对端面板创建 FTP 账号 %s（密码 %s）...", f.Username, pass)
		home := f.HomeDir
		if home == "" {
			home = "/www/wwwroot/" + f.Username
		}
		if _, err := bt.AddFtpUser(f.Username, pass, home); err != nil {
			t.logf("创建 FTP 账号 %s 失败: %v", f.Username, err)
			t.addItem("ftp", f.Username, "failed", "创建 FTP 账号失败："+err.Error())
			continue
		}
		t.logf("FTP 账号 %s 创建完成", f.Username)
		t.addItem("ftp", f.Username, "success", "创建成功")
	}

	// 6. 收尾：只要任一子任务失败，整体就标记为 failed；
	// 全部 success/skipped 才算迁出完成。前端据此决定显示"完成"页还是失败列表。
	t.mu.Lock()
	if t.hasFailedItemsLocked() {
		t.Status = "failed"
		t.Error = "存在失败的子任务，请查看下方详情后重试"
	} else {
		t.Status = "success"
	}
	t.UpdatedAt = time.Now()
	t.mu.Unlock()

	if t.Status == "success" {
		t.logf("迁出到对端面板完成！请到对端面板确认站点/数据库/FTP 状态。")
	} else {
		t.logf("迁出未完成：上方存在失败项，请按提示处理后重试")
	}
}

// applyBTSiteSettings 把站点级设置（运行目录 / 伪静态 / SSL）同步到对端面板。
//
// 只通过官方 API 修改，绝不搬运 kypanel 的 nginx 配置片段（ConfigOverride）：
// 对端面板（宝塔）的站点配置由它自己生成，两套配置的语法与组织结构不同，
// 直接写入会让 web 服务器校验失败（典型错误：a duplicate default server for 0.0.0.0:80）。
// 只有目标同为 kypanel 面板时才搬运配置（见 migrate.go 的 restoreSite）。
//
// 单项失败仅记录日志、不中断整体迁移，避免某个可选配置失败导致整站回滚。
//
// btWebname 是 AddSite 接口返回的宝塔站点标识（通常是主域名，如 php.n05v.cn）。
// 宝塔伪静态文件按 webname 命名（/www/server/panel/vhost/rewrite/<webname>.conf），
// 而 kypanel 的站点名（ms.Name）可能与宝塔 webname 不一致——直接用 ms.Name 写文件
// 会落空，宝塔 UI 里看不到。所以同时尝试 webname / 主域名 / 站点名等多个候选。
// applyBTSiteSettings 把站点级设置（运行目录 / 伪静态 / SSL）同步到对端面板。
//
// 返回值：true=全部成功，false=至少一项失败（外层会据此在 t.Items 里追加 site-config 失败记录）。
//
// 只通过官方 API 修改，绝不搬运 kypanel 的 nginx 配置片段（ConfigOverride）：
// 对端面板（宝塔）的站点配置由它自己生成，两套配置的语法与组织结构不同，
// 直接写入会让 web 服务器校验失败（典型错误：a duplicate default server for 0.0.0.0:80）。
// 只有目标同为 kypanel 面板时才搬运配置（见 migrate.go 的 restoreSite）。
//
// btWebname 是 AddSite 接口返回的宝塔站点标识（通常是主域名，如 php.n05v.cn）。
// 宝塔伪静态文件按 webname 命名（/www/server/panel/vhost/rewrite/<webname>.conf），
// 而 kypanel 的站点名（ms.Name）可能与宝塔 webname 不一致——直接用 ms.Name 写文件
// 会落空，宝塔 UI 里看不到。所以同时尝试 webname / 主域名 / 站点名等多个候选。
func applyBTSiteSettings(t *ImportTask, bt *BTClient, ms *MigrateSite, siteID, btWebname string) bool {
	hasFail := false

	// 运行目录：kypanel 存的是相对网站根目录的路径（如 public），宝塔要求 "/public" 形式
	if siteID != "" && strings.TrimSpace(ms.RuntimeDir) != "" {
		runPath := "/" + strings.Trim(strings.TrimSpace(ms.RuntimeDir), "/")
		if runPath != "/" {
			t.logf("设置网站 %s 运行目录为 %s ...", ms.Name, runPath)
			if err := bt.SetSiteRunPath(siteID, runPath); err != nil {
				t.logf("设置网站 %s 运行目录失败（可到对端面板「网站目录」手动设置）: %v", ms.Name, err)
				hasFail = true
			} else {
				t.logf("网站 %s 运行目录设置完成", ms.Name)
			}
		}
	}

	// 伪静态：宝塔 rewrite 文件按 webname 命名，候选多个 webname 确保至少一个命中
	if strings.TrimSpace(ms.Rewrite) != "" {
		candidates := make([]string, 0, 4)
		seen := map[string]bool{}
		add := func(name string) {
			n := strings.TrimSpace(name)
			if n != "" && !seen[n] {
				seen[n] = true
				candidates = append(candidates, n)
			}
		}
		add(btWebname)
		add(ms.Domain)
		if ms.Domains != "" {
			for _, p := range strings.Split(ms.Domains, ",") {
				add(strings.TrimSpace(p))
				break // 只取第一项作为 webname 候选
			}
		}
		add(ms.Name)

		written := false
		for _, wn := range candidates {
			t.logf("写入网站 %s 伪静态规则到 %s.conf ...", ms.Name, wn)
			if err := bt.ApplyCustomRewrite(wn, ms.Rewrite); err != nil {
				t.logf("写入 %s.conf 失败: %v", wn, err)
				continue
			}
			t.logf("网站 %s 伪静态规则已写入（%s.conf）", ms.Name, wn)
			written = true
			break // 写一个候选即可，其他的 conf 文件宝塔不会加载
		}
		if !written {
			t.logf("网站 %s 伪静态写入失败（可到对端面板「网站 → 设置 → 伪静态」手动粘贴）", ms.Name)
			hasFail = true
		}
	}

	// SSL 证书：宝塔按主域名识别站点
	if !ms.SslEnabled || ms.SslCert == "" || ms.SslKey == "" || ms.Domain == "" {
		return !hasFail
	}
	t.logf("部署网站 %s 的 SSL 证书 ...", ms.Name)
	if err := bt.SetSSL(ms.Domain, ms.SslKey, ms.SslCert); err != nil {
		t.logf("部署网站 %s SSL 证书失败（可到对端面板手动部署）: %v", ms.Name, err)
		return false
	}
	t.logf("网站 %s SSL 证书部署完成", ms.Name)
	if ms.SslForce {
		if err := bt.HttpToHttps(ms.Domain); err != nil {
			t.logf("开启网站 %s 强制 HTTPS 失败: %v", ms.Name, err)
			hasFail = true
		} else {
			t.logf("网站 %s 已开启强制 HTTPS", ms.Name)
		}
	}
	return !hasFail
}



// btDeleteRemoteFile 删除对端面板服务器上的临时文件（失败不报错）
func btDeleteRemoteFile(bt *BTClient, path string) {
	params := url.Values{}
	params.Set("name", path)
	if _, err := bt.btRequest("files", "DeleteFile", params); err != nil {
		_ = err
	}
}

// gunzipFile 将 .sql.gz 解压为明文 .sql
func gunzipFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gr.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, gr)
	return err
}

// normalizeSQLCompat 对导出的 SQL 做兼容性降级，使其能在低版本 MySQL/MariaDB 上导入：
//   - utf8mb3 → utf8mb4（utf8mb3 即 utf8，MySQL 8.0.30+ 才区分；utf8mb4 自 MySQL 5.5/MariaDB 10.1 起支持）
//   - MySQL 8.0.30+ 的 uca1400 系列与 8.0 的 0900 系列 collation → unicode_ci（5.5/MariaDB 10.1+ 通用）
//
// 不处理会报 "ERROR 1273 Unknown collation: 'xxx'" 导致 CREATE TABLE 失败，
// 后续 INSERT 全部报 "Table doesn't exist"（对端导入日志实测）。
func normalizeSQLCompat(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sql := string(data)
	out := strings.ReplaceAll(sql, "utf8mb3", "utf8mb4")
	for _, c := range []string{
		"uca1400_ai_ci", "uca1400_as_ci", "uca1400_ai_cs", "uca1400_as_cs",
		"0900_ai_ci", "0900_as_cs", "0900_ai_cs", "0900_as_ci",
	} {
		out = strings.ReplaceAll(out, c, "unicode_ci")
	}
	if out == sql {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// BTEnvCompare 预检：把本机选中网站/数据库需要的环境与对端面板已装环境对比。
// 返回：
//   php_required / php_installed / php_missing —— PHP 版本需求与缺失（缺失=阻断迁移）
//   mysql_required / mysql_installed / mysql_local_version / mysql_remote_version / mysql_diff
//     mysql_diff: none(未选库) / missing(对端无 MySQL=阻断) / same(版本一致) /
//                diff(版本不同=需用户确认) / downgrade(本机 8.x 迁到对端 5.x=高风险确认) / unknown(版本未知=确认)
func BTEnvCompare(btURL, btSK string, sites, databases []string) (map[string]any, error) {
	bt := NewBTClient(btURL, btSK)
	phpList, err := bt.PHPVersionList()
	if err != nil {
		return nil, err
	}
	// 对端面板返回版本号可能带点（如 "7.4"）或纯数字（"74"），统一转 Remi 风格
	normalized := map[string]bool{}
	for _, v := range phpList {
		v = strings.TrimSpace(v)
		normalized[v] = true
		key := strings.ReplaceAll(v, ".", "")
		normalized[key] = true
	}
	need := map[string]bool{}
	for _, name := range sites {
		var s model.Site
		if err := model.DB.Where("name = ?", name).First(&s).Error; err != nil {
			continue
		}
		if s.Type == model.SiteTypePHP {
			v := phpVersionFromFpm(s.PhpFpm)
			if v == "" {
				v = "74"
			}
			need[v] = true
		}
	}
	var missing []string
	for v := range need {
		if !normalized[v] {
			missing = append(missing, v)
		}
	}

	// ---- MySQL 环境对比 ----
	needMySQL := len(databases) > 0
	mysqlDiff := "none" // 未选数据库，不要求 MySQL
	mysqlInstalled := false
	localVer := localMySQLVersion()
	remoteVer := ""
	if needMySQL {
		mysqlInstalled, remoteVer = btRemoteMySQLVersion(bt)
		if !mysqlInstalled {
			mysqlDiff = "missing" // 对端没有 MySQL：阻断
		} else if localVer != "" && remoteVer != "" {
			if localVer == remoteVer {
				mysqlDiff = "same"
			} else if mysqlMajor(localVer) > mysqlMajor(remoteVer) {
				// 本机 MySQL 8.x → 对端 5.x：高版本导低版本，字符集/排序规则不兼容风险最高
				mysqlDiff = "downgrade"
			} else {
				mysqlDiff = "diff"
			}
		} else {
			mysqlDiff = "unknown" // 至少一端拿不到版本：提示用户确认
		}
	}

	allReady := len(missing) == 0 && !(needMySQL && !mysqlInstalled)
	return map[string]any{
		"php_installed":      normalized,
		"php_required":       need,
		"php_missing":        missing,
		"mysql_required":     needMySQL,
		"mysql_installed":    mysqlInstalled,
		"mysql_local_version": localVer,
		"mysql_remote_version": remoteVer,
		"mysql_diff":          mysqlDiff,
		"all_ready":           allReady,
	}, nil
}

// localMySQLVersion 探测本机 MySQL/MariaDB 主版本（如 "5.5" / "8.0"）。
// 优先读应用记录缓存，避免每次预检都执行探测命令；无记录时兜底执行版本命令。
func localMySQLVersion() string {
	if rec, err := model.GetAppRecord("mysql"); err == nil && rec.Version != "" {
		return parseMySQLVersion(rec.Version)
	}
	if v, err := probeVersion("mysqld --version 2>/dev/null || mysql --version 2>/dev/null"); err == nil {
		return parseMySQLVersion(v)
	}
	return ""
}

// btRemoteMySQLVersion 获取对端面板 MySQL 安装状态与主版本。
// 对端 GetMySqlInfo 接口拿版本；接口不可用时以「能列出数据库」兜底判定 MySQL 已安装。
func btRemoteMySQLVersion(bt *BTClient) (installed bool, version string) {
	if res, err := bt.btRequest("system", "GetMySqlInfo", nil); err == nil {
		if v := strFromAny(res["mysql_version"]); v != "" {
			return true, parseMySQLVersion(v)
		}
	}
	if _, err := bt.DatabaseList(); err == nil {
		return true, ""
	}
	return false, ""
}

// parseMySQLVersion 从版本输出提取主版本号（如 "Ver 8.0.36" → "8.0"、"5.5.62-MariaDB" → "5.5"）
func parseMySQLVersion(out string) string {
	re := regexp.MustCompile(`(\d+)\.(\d+)`)
	if m := re.FindStringSubmatch(out); m != nil {
		return m[1] + "." + m[2]
	}
	return ""
}

// mysqlMajor 取主版本整数（8.0 → 8，5.5 → 5），用于判断降级方向
func mysqlMajor(v string) int {
	re := regexp.MustCompile(`^\d+`)
	if m := re.FindString(v); m != "" {
		if n, err := strconv.Atoi(m); err == nil {
			return n
		}
	}
	return 0
}
