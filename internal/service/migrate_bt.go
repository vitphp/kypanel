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
	// 秒级时间戳 + 随机后缀：仅用时间戳时，同一秒内发起两个迁出任务会拿到相同 ID 而互相覆盖
	id := "btexport-" + time.Now().Format("20060102150405") + "-" + randHex(4)
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
	for _, w := range exp.Warnings {
		t.logf("提示：%s", w)
	}

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
	// name → {id, name, webname}，用于冲突判断与覆盖时删除；
	// btDomainMap：对端已绑定的域名 → 站点名（本机站点可能在对端被换了名字绑同一个域名）
	type btSiteEntry struct{ id, name, webname string }
	btSiteMap := map[string]btSiteEntry{}
	btDomainMap := bt.SiteDomainIndex(existing)
	for _, s := range existing {
		n, _ := s["name"].(string)
		if n == "" {
			continue
		}
		id := fmt.Sprintf("%v", s["id"])
		btSiteMap[n] = btSiteEntry{id: id, name: n, webname: btParseWebname(s)}
	}

	t.logf("对端面板连接成功")

	// 2.1 迁移前硬校验对端环境（前端预检可能被跳过，直连 API 调用更会跳过）：
	// 缺 PHP 版本 / JDK（含版本不匹配）/ Node / Python 环境 / MySQL 时直接中止，
	// 避免把站点建出来、文件传上去，最后发现根本跑不起来。
	if cmp, cerr := BTEnvCompare(req.BTURL, req.BTSK, req.Sites, req.Databases); cerr == nil {
		var miss []string
		var tips []string
		if l, ok := cmp["php_missing"].([]string); ok {
			for _, v := range l {
				miss = append(miss, "PHP "+btPhpVersionDotted(v))
			}
			if len(l) > 0 {
				tips = append(tips, strFromAny(cmp["php_hint"]))
			}
		}
		if l, ok := cmp["runtime_missing"].([]string); ok {
			miss = append(miss, l...)
			if hints, ok := cmp["runtime_hints"].(map[string]string); ok {
				for _, item := range l {
					switch {
					case strings.HasPrefix(item, "Java"):
						tips = append(tips, hints["java"])
					case strings.HasPrefix(item, "Node"):
						tips = append(tips, hints["node"])
					case strings.HasPrefix(item, "Python"):
						tips = append(tips, hints["python"])
					}
				}
			}
		}
		if need, _ := cmp["mysql_required"].(bool); need {
			if installed, _ := cmp["mysql_installed"].(bool); !installed {
				miss = append(miss, "MySQL")
				tips = append(tips, "对端面板未安装 MySQL，请先安装后再迁移")
			}
		}
		if len(miss) > 0 {
			var validTips []string
			for _, s := range tips {
				if strings.TrimSpace(s) != "" {
					validTips = append(validTips, s)
				}
			}
			msg := fmt.Sprintf("对端面板环境不满足，缺少 %s。%s", strings.Join(miss, "、"), strings.Join(validTips, " "))
			t.fail(msg)
			return
		}
	}

	// 3. 创建网站/项目 + 上传文件 + 应用站点级配置
	// 注：单个网站失败不中断整体，让所有失败都能在前端展示，
	//     整体 Status 在末尾根据 Items 是否有 failed 决定。
	for _, ms := range exp.Manifest.Sites {
		if t.canceled() {
			return
		}
		domains := ms.Domain
		if ms.Domains != "" {
			domains += "," + ms.Domains
		}
		domainList := btSplitDomains(domains)
		phpVer := ms.PhpVersion
		if phpVer == "" {
			phpVer = "74"
		}
		// 目标目录尽量沿用源站目录；但源站目录若被设成了 /www/wwwroot 这类系统关键目录
		// （用户在文件选择器里选中了根目录），对端面板会直接拒绝创建——
		// 报「不能以系统关键目录作为站点目录」，统一回退到 /www/wwwroot/<站点名>。
		origRoot := strings.TrimSpace(ms.Root)
		path := btSafeSitePath(origRoot, ms.Name)
		if path != origRoot {
			if origRoot == "" {
				t.logf("网站 %s 未记录项目目录，对端面板使用 %s", ms.Name, path)
			} else {
				t.logf("网站 %s 源目录 %s 是对端面板不允许的系统关键目录，改用 %s", ms.Name, origRoot, path)
			}
		}
		ms.Root = path

		// 3.1 处理对端已存在的同名网站/项目（按用户选择：跳过 / 覆盖删除）
		// 项目在对端面板按「去点去横线的项目名」登记，因此同名判断也要用这个名字查；
		// 另外用户可能在对端用别的名字绑了同一个域名，域名命中同样算冲突。
		lookup := ms.Name
		if btIsProjectType(ms.Type) {
			lookup = btProjectName(ms.Name)
		}
		info, exists := btSiteMap[lookup]
		if !exists {
			for _, d := range domainList {
				if n, ok := btDomainMap[strings.ToLower(d)]; ok {
					info, exists = btSiteMap[n], true
					break
				}
			}
		}
		if exists {
			// 没拿到覆盖/跳过决策时绝不能静默跳过：那会让用户看到「迁移成功」但什么都没迁。
			// 正常流程前端会先弹窗收集决策，这里兜住预检漏检 / 直接调 API 的情况。
			overwrite, decided := req.SiteOverwrite[ms.Name]
			if !decided {
				t.logf("网站 %s 在对端面板已存在（%s），但未提供覆盖/跳过选择", ms.Name, info.name)
				t.addItem("site", ms.Name, "failed", "对端面板已存在同名站点，请重新预检并在弹窗中选择「覆盖」或「跳过」")
				continue
			}
			if !overwrite {
				t.logf("网站 %s 在对端面板已存在，按选择跳过", ms.Name)
				t.addItem("site", ms.Name, "skipped", "对端面板已存在，按选择跳过")
				continue
			}
			t.logf("网站 %s 在对端面板已存在，先删除后重建（覆盖）...", ms.Name)
			removed := false
			if btIsProjectType(ms.Type) {
				// 项目必须走项目删除接口：只删站点记录会留下仍在运行的守护进程与 project_config
				if err := btRemoveProject(bt, ms.Type, info.name); err == nil {
					removed = true
				} else {
					t.logf("调用对端项目删除接口失败（改用站点删除接口）: %v", err)
				}
			}
			if !removed {
				if _, err := bt.DeleteSite(info.id, info.webname); err != nil {
					t.logf("覆盖网站 %s 失败（请到对端面板手动删除该网站后重试）: %v", ms.Name, err)
					t.addItem("site", ms.Name, "failed", "删除对端旧网站失败："+err.Error())
					continue
				}
			}
		}

		pkgFile := filepath.Join(workDir, "sites", ms.Name, "wwwroot.tar.gz")

		// 3.2 进程型站点（Java / Node / Go / Python）：走对端面板的项目接口创建。
		// 注意不能用 site?action=AddSite —— 那只会建出一条 PHP 站点记录：没有 project_config、
		// 没有守护进程、也不会把域名反代到应用端口，表现就是「迁过去的 Java 站点变成了 PHP 网站」。
		if btIsProjectType(ms.Type) {
			t.logf("在对端面板创建 %s 项目 %s（目录 %s，端口 %d）...", strings.ToUpper(ms.Type), ms.Name, path, ms.ProxyPort)
			// 对端面板创建项目时会校验项目目录 / jar / 可执行文件已存在，因此先把文件传过去解压
			if err := btUploadSiteFiles(t, bt, ms, path, pkgFile); err != nil {
				t.addItem("site-file", ms.Name, "failed", err.Error())
				continue
			}
			if err := createBtProject(t, bt, &ms, path, domainList, ms.ProxyPort); err != nil {
				t.logf("创建项目 %s 失败: %v", ms.Name, err)
				t.addItem("site", ms.Name, "failed", "创建项目失败："+err.Error())
				continue
			}
			t.addItem("site", ms.Name, "success", "创建成功（"+strings.ToUpper(ms.Type)+" 项目）")
			continue
		}

		// 3.3 常规网站（PHP / 静态 / 反向代理）
		siteKind, siteType, sitePhpVer := "网站", "PHP", phpVer
		switch ms.Type {
		case model.SiteTypeStatic:
			// 静态站点：type 留空 + version=00，避免在对端面板建出一个 PHP 站点
			siteKind, siteType, sitePhpVer = "静态网站", "html", ""
		case model.SiteTypeProxy:
			// 反向代理：先建站点承载域名，随后用 CreateProxy 配置反代目标
			siteKind, siteType, sitePhpVer = "反向代理站点", "proxy", ""
		}
		t.logf("在对端面板创建%s %s（域名 %s，目录 %s）...", siteKind, ms.Name, domains, path)
		addRes, err := bt.AddSite(ms.Name, domains, path, sitePhpVer, siteType)
		if err != nil {
			t.logf("创建%s %s 失败: %v", siteKind, ms.Name, err)
			t.addItem("site", ms.Name, "failed", "创建失败："+err.Error())
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
		// 对端面板 webname（用于伪静态文件名，不同版本字段名差异也较大）
		btWebname := ""
		for _, k := range []string{"siteName", "webname", "name"} {
			if v, ok := addRes[k]; ok && v != nil {
				if s := strFromAny(v); s != "" {
					btWebname = s
					break
				}
			}
		}
		t.logf("%s %s 创建成功（ID %s，webname %s）", siteKind, ms.Name, siteID, btWebname)
		t.addItem("site", ms.Name, "success", "创建成功（ID "+siteID+"）")

		// 3.4 上传网站文件（无文件包时仅创建目录）
		if err := btUploadSiteFiles(t, bt, ms, path, pkgFile); err != nil {
			t.addItem("site-file", ms.Name, "failed", err.Error())
			continue
		}

		// 3.5 反向代理：把对端站点的请求转发到源站应用端口
		if ms.Type == model.SiteTypeProxy {
			target := strings.TrimSpace(ms.ProxyPass)
			if target == "" && ms.ProxyPort > 0 {
				target = fmt.Sprintf("http://127.0.0.1:%d", ms.ProxyPort)
			}
			if target == "" {
				t.logf("反向代理站点 %s 源站未记录反代目标，请到对端面板手动配置", ms.Name)
			} else if err := bt.CreateProxy(btPrimaryDomain(domainList, ms.Name), target); err != nil {
				t.logf("配置反向代理 %s -> %s 失败（可到对端面板手动配置）: %v", ms.Name, target, err)
				t.addItem("site-config", ms.Name, "failed", "反向代理配置失败："+err.Error())
			} else {
				t.logf("反向代理 %s -> %s 配置完成", ms.Name, target)
			}
		}

		// 3.6 站点级配置：运行目录 / 伪静态 / SSL
		if !applyBTSiteSettings(t, bt, &ms, siteID, btWebname) {
			t.addItem("site-config", ms.Name, "failed", "伪静态/运行目录/SSL 至少一项失败，请查看上方日志详情")
		}
	}

	// 4. 数据库
	dbsExist := map[string]string{}
	if dbs, err := bt.DatabaseList(); err == nil {
		for _, d := range dbs {
			// 注意：对端面板 JSON 里 id 是数字（解码后为 float64），必须用 strFromAny 而不是 (string) 断言
			if id := strFromAny(d["id"]); id != "" {
				if n, _ := d["name"].(string); n != "" {
					dbsExist[n] = id
				}
			}
		}
	}
	for _, db := range exp.Manifest.Databases {
		if t.canceled() {
			return
		}
		pass := req.DBPassword
		if pass == "" {
			pass = db.Password
		}
		if pass == "" {
			pass = randomPassword(16)
		}

		// 4.1 处理对端已存在的同名数据库
		if dbID, exists := dbsExist[db.Name]; exists {
			overwrite, decided := req.DatabaseOverwrite[db.Name]
			if !decided {
				t.logf("数据库 %s 在对端面板已存在，但未提供覆盖/跳过选择", db.Name)
				t.addItem("database", db.Name, "failed", "对端面板已存在同名数据库，请重新预检并在弹窗中选择「覆盖」或「跳过」")
				continue
			}
			if !overwrite {
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
		// 优先「服务端路径」方式（对端面板通用，老版/新版 InputSql 均支持，SQL 已上传到对端）。
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
		if t.canceled() {
			return
		}
		if btFtpExist[f.Username] {
			overwrite, decided := req.FtpOverwrite[f.Username]
			if !decided {
				t.logf("FTP 账号 %s 在对端面板已存在，但未提供覆盖/跳过选择", f.Username)
				t.addItem("ftp", f.Username, "failed", "对端面板已存在同名 FTP 账号，请重新预检并在弹窗中选择「覆盖」或「跳过」")
				continue
			}
			if !overwrite {
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
	// 统计跳过项：全是跳过（对端已有同名对象、用户选了跳过）时不能报「迁移完成」，
	// 否则用户会以为对象都迁过去了 —— 这正是之前那个「一下就提示迁移成功」的坑。
	skippedN, successN := 0, 0
	for _, it := range t.Items {
		switch it.Status {
		case "skipped":
			skippedN++
		case "success":
			successN++
		}
	}
	t.UpdatedAt = time.Now()
	t.mu.Unlock()

	switch {
	case t.Status != "success":
		t.logf("迁出未完成：上方存在失败项，请按提示处理后重试")
	case skippedN > 0 && successN == 0:
		t.logf("所选对象在对端面板都已存在，已按你的选择全部跳过，对端面板未做任何改动。")
	default:
		t.logf("迁出到对端面板完成！请到对端面板确认站点/数据库/FTP 状态。")
		if skippedN > 0 {
			t.logf("其中 %d 项因对端已存在同名对象、按你的选择跳过（详见完成页列表）。", skippedN)
		}
	}
}

// applyBTSiteSettings 把站点级设置（运行目录 / 伪静态 / SSL）同步到对端面板。
//
// 只通过官方 API 修改，绝不搬运 kypanel 的 nginx 配置片段（ConfigOverride）：
// 对端面板的站点配置由它自己生成，两套配置的语法与组织结构不同，
// 直接写入会让 web 服务器校验失败（典型错误：a duplicate default server for 0.0.0.0:80）。
// 只有目标同为 kypanel 面板时才搬运配置（见 migrate.go 的 restoreSite）。
//
// 单项失败仅记录日志、不中断整体迁移，避免某个可选配置失败导致整站回滚。
//
// btWebname 是 AddSite 接口返回的对端面板站点标识（通常是主域名，如 php.n05v.cn）。
// 对端面板伪静态文件按 webname 命名（/www/server/panel/vhost/rewrite/<webname>.conf），
// 而 kypanel 的站点名（ms.Name）可能与对端面板 webname 不一致——直接用 ms.Name 写文件
// 会落空，对端面板 UI 里看不到。所以同时尝试 webname / 主域名 / 站点名等多个候选。
func applyBTSiteSettings(t *ImportTask, bt *BTClient, ms *MigrateSite, siteID, btWebname string) bool {
	hasFail := false

	// 运行目录：kypanel 存的是相对网站根目录的路径（如 public），对端面板要求 "/public" 形式
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

	// 伪静态：对端面板 rewrite 文件按 webname 命名，候选多个 webname 确保至少一个命中
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
			break // 写一个候选即可，其他的 conf 文件对端面板不会加载
		}
		if !written {
			t.logf("网站 %s 伪静态写入失败（可到对端面板「网站 → 设置 → 伪静态」手动粘贴）", ms.Name)
			hasFail = true
		}
	}

	// SSL 证书：对端面板按主域名识别站点
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
//   runtime_required / runtime_installed / runtime_missing —— 非 PHP 项目所需运行时
//     （Java 需要 JDK、Node 需要 Node 版本、Python 需要虚拟环境；Go 跑的是已编译二进制，无需运行时）
//     runtime_missing 非空=阻断迁移
func BTEnvCompare(btURL, btSK string, sites, databases []string) (map[string]any, error) {
	bt := NewBTClient(btURL, btSK)
	phpList, err := bt.PHPVersionList()
	if err != nil {
		return nil, err
	}
	// 对端面板返回版本号可能带点（如 "7.4"）或纯数字（"74"），统一转 Remi 风格。
	// "" 与 "00" 是面板的占位值（静态站点），不是 PHP 版本，需要剔除——
	// 否则前端会显示成「PHP 、PHP 00」这种噪声。
	normalized := map[string]bool{}
	for _, v := range phpList {
		v = strings.TrimSpace(v)
		if v == "" || v == "00" {
			continue
		}
		normalized[v] = true
		key := strings.ReplaceAll(v, ".", "")
		normalized[key] = true
	}
	need := map[string]bool{}
	runtimeNeed := map[string]bool{}
	// 源站 Java 站点要求的最低 JDK 主版本（取所有 Java 站点里最高的那个；0 = 源站没记录版本）
	javaNeedMajor := 0
	for _, name := range sites {
		var s model.Site
		if err := model.DB.Where("name = ?", name).First(&s).Error; err != nil {
			continue
		}
		switch s.Type {
		case model.SiteTypePHP:
			v := phpVersionFromFpm(s.PhpFpm)
			if v == "" {
				v = "74"
			}
			need[v] = true
		case model.SiteTypeJava:
			runtimeNeed["java"] = true
			// 站点的 runtime_version 形如 "Java 21.0"：必须用 ≥ 该版本的 JDK 才能跑起来
			if m := btJavaMajor(s.RuntimeVersion); m > javaNeedMajor {
				javaNeedMajor = m
			}
		case model.SiteTypeNode:
			runtimeNeed["node"] = true
		case model.SiteTypePython:
			runtimeNeed["python"] = true
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

	// ---- 非 PHP 运行时对比（Java / Node / Python）----
	// 项目型站点迁出后由对端面板的项目守护进程运行，必须有对应运行时：
	//   Java 需要 JDK、Node 需要已安装的 Node 版本、Python 需要虚拟环境；
	//   Go 项目跑的是已经编译好的二进制，不需要 Go 工具链。
	var runtimeInstalled = map[string]any{
		"java":   []string{},
		"node":   []string{},
		"python": []string{},
	}
	var runtimeMissing []string
	runtimeHints := map[string]string{
		"java":   "请在对端面板「网站 → Java 项目」中安装 JDK",
		"node":   "请在对端面板「网站 → Node 项目」中安装 Node 版本",
		"python": "请在对端面板「网站 → Python 项目」中创建 Python 环境",
	}
	if runtimeNeed["java"] {
		// 必须按源站要求的版本比对：源站 JDK 21 编译的 jar 在 JDK 8 上会报
		// UnsupportedClassVersionError，项目建出来也是跑不起来的。
		jdks := bt.JavaJDKs()
		if home := pickJavaFrom(jdks, javaNeedMajor); home != "" {
			ver := ""
			for _, j := range jdks {
				if j.Home == home {
					ver = j.Version
					break
				}
			}
			runtimeInstalled["java"] = []string{ver}
		} else if len(jdks) == 0 {
			runtimeMissing = append(runtimeMissing, "Java (JDK)")
		} else {
			runtimeMissing = append(runtimeMissing, fmt.Sprintf("Java (JDK %d)", javaNeedMajor))
			runtimeHints["java"] = fmt.Sprintf(
				"源站项目用 JDK %d 编译，对端只装了 %s，请在对端面板「网站 → Java 项目」中安装 JDK %d 后重试",
				javaNeedMajor, btJavaVersionsText(jdks), javaNeedMajor)
		}
	}
	if runtimeNeed["node"] {
		if versions := bt.NodejsVersions(); len(versions) == 0 {
			runtimeMissing = append(runtimeMissing, "Node")
		} else {
			for _, v := range versions {
				runtimeInstalled["node"] = append(runtimeInstalled["node"].([]string), strings.TrimPrefix(v, "v"))
			}
		}
	}
	if runtimeNeed["python"] {
		if bt.PythonBinPath() == "" {
			runtimeMissing = append(runtimeMissing, "Python 环境")
		} else {
			runtimeInstalled["python"] = []string{"已安装"}
		}
	}

	allReady := len(missing) == 0 && len(runtimeMissing) == 0 && !(needMySQL && !mysqlInstalled)
	return map[string]any{
		"php_installed":        normalized,
		"php_required":         need,
		"php_missing":          missing,
		"php_hint":             "请在对端面板「软件商店」安装对应 PHP 版本",
		"mysql_required":       needMySQL,
		"mysql_installed":      mysqlInstalled,
		"mysql_local_version":  localVer,
		"mysql_remote_version": remoteVer,
		"mysql_diff":           mysqlDiff,
		"runtime_required":     runtimeNeed,
		"runtime_installed":    runtimeInstalled,
		"runtime_missing":      runtimeMissing,
		"runtime_hints":        runtimeHints,
		"java_major":           javaNeedMajor, // 源站 Java 项目要求的 JDK 主版本（0=未知）
		"all_ready":            allReady,
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
