package service

// ==================== 对端面板 → kypanel 迁入 ====================
// 通过对端面板官方 API 将对端面板网站/数据库/FTP 迁入本面板。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kypanel/internal/model"
)

// FetchBTSites 从对端面板拉取网站列表（含关联的数据库/FTP），供迁入页面选择
func FetchBTSites(panelURL, apiSK string) ([]map[string]any, error) {
	client := NewBTClient(panelURL, apiSK)
	sites, err := client.SiteList()
	if err != nil {
		return nil, fmt.Errorf("获取对端面板网站列表失败: %w", err)
	}
	dbs, err := client.DatabaseList()
	if err != nil {
		dbs = nil
	}
	ftps, err := client.FtpUserList()
	if err != nil {
		ftps = nil
	}

	var out []map[string]any
	for _, s := range sites {
		name := toStr(s["name"])
		if name == "" {
			continue
		}
		root := toStr(s["path"])
		phpVer := normBTPhpVersion(strFromAny(s["php_version"]))
		projType := strFromAny(s["project_type"])
		if projType == "" {
			projType = strFromAny(s["type"])
		}
		typ := mapBTProjectType(projType)
		domains := parseBTDomains(s["domain"])
		primary := ""
		if len(domains) > 0 {
			primary = domains[0]
		}

		// 关联数据库：库名/用户名包含网站名
		var siteDBs []string
		for _, d := range dbs {
			dbName := toStr(d["name"])
			dbUser := toStr(d["db_user"])
			if dbUser == "" {
				dbUser = toStr(d["username"])
			}
			if dbName != "" && (dbName == name || dbUser == name || strings.Contains(dbName, name) || strings.Contains(dbUser, name)) {
				siteDBs = append(siteDBs, dbName)
			}
		}

		// 关联 FTP：家目录指向网站根目录
		var siteFtps []string
		for _, f := range ftps {
			uname := toStr(f["username"])
			upath := toStr(f["path"])
			if uname != "" && root != "" && (upath == root || strings.HasPrefix(upath, root+"/")) {
				siteFtps = append(siteFtps, uname)
			}
		}

		out = append(out, map[string]any{
			// 宝塔站点 id 是 JSON 数字（解码为 float64），必须用 strFromAny 转字符串，
			// site?action=BackupSite 等接口按 id 定位站点，缺失会导致「指定参数无效」
			"id":           strFromAny(s["id"]),
			"name":         name,
			"domain":       primary,
			"domains":      strings.Join(domains, ","),
			"type":         typ,
			"php_version":  phpVer,
			"root":         root,
			"project_type": projType,
			"dbs":          siteDBs,
			"ftps":         siteFtps,
			"migratable":   true,
		})
	}
	return out, nil
}

// BuildBTImportPlan 构建对端面板迁入计划（环境对比）
func BuildBTImportPlan(req ImportPlanRequest) (*ImportPlanResult, error) {
	if req.PanelURL == "" || req.PanelToken == "" {
		return nil, errors.New("请填写对端面板地址和 API 密钥")
	}
	sites, err := FetchBTSites(req.PanelURL, req.PanelToken)
	if err != nil {
		return nil, err
	}
	var manifest MigrateManifest
	var picked []map[string]any
	for _, s := range sites {
		name := toStr(s["name"])
		if !containsStr(req.Sites, name) {
			continue
		}
		picked = append(picked, s)
		ms := MigrateSite{
			Name:       name,
			Domain:     toStr(s["domain"]),
			Domains:    toStr(s["domains"]),
			Type:       toStr(s["type"]),
			Root:       toStr(s["root"]),
			PhpVersion: normBTPhpVersion(toStr(s["php_version"])),
		}
		ms.DBs = toStringSlice(s["dbs"])
		ms.FTPs = toStringSlice(s["ftps"])
		manifest.Sites = append(manifest.Sites, ms)
	}
	// 环境对比只针对「用户实际勾选」的数据库/FTP，不把网站关联项算进去
	for _, n := range req.Databases {
		manifest.Databases = append(manifest.Databases, MigrateDB{Name: n, User: n})
	}
	for _, n := range req.FTPs {
		manifest.FTPs = append(manifest.FTPs, MigrateFTP{Username: n})
	}
	// 只有勾选了网站却一个都没匹配上才报错；允许只迁移数据库/FTP 而不迁网站
	if len(req.Sites) > 0 && len(picked) == 0 {
		return nil, errors.New("对端面板上未找到选中的网站")
	}
	env := MigrateEnvStatus()
	missing := CompareEnvForMigration(&manifest, env)
	exSites, exDBs, exFTPs := resolveImportConflicts(req.Sites, req.Databases, req.FTPs)
	return &ImportPlanResult{
		PanelVersion:  "对端面板",
		Env:           env,
		Sites:         picked,
		Missing:       missing,
		ExistingSites: exSites,
		ExistingDBs:   exDBs,
		ExistingFTPs:  exFTPs,
		DBsCount:      len(manifest.Databases),
		FTPsCount:     len(manifest.FTPs),
	}, nil
}

// runImportFromBT 执行对端面板 → kypanel 迁入
func runImportFromBT(t *ImportTask, req ImportRunRequest) {
	defer func() {
		if r := recover(); r != nil {
			t.mu.Lock()
			t.Status = "failed"
			t.Error = fmt.Sprintf("%v", r)
			t.mu.Unlock()
			t.logf("迁移失败: %v", r)
		}
	}()

	client := NewBTClient(req.PanelURL, req.PanelToken)

	t.logf("正在连接对端面板 %s ...", req.PanelURL)
	if _, err := client.SiteList(); err != nil {
		t.fail("连接对端面板失败: " + err.Error())
		return
	}
	t.logf("对端面板连接成功")

	sites, err := client.SiteList()
	if err != nil {
		t.fail("获取对端面板网站列表失败: " + err.Error())
		return
	}
	dbs, _ := client.DatabaseList()
	ftps, _ := client.FtpUserList()

	// 安装缺失环境
	if len(req.AutoInstall) > 0 {
		t.logf("开始安装缺失环境：%s ...", strings.Join(req.AutoInstall, ", "))
		for _, key := range req.AutoInstall {
			if t.canceled() {
				return
			}
			t.logf("安装 %s 中...", key)
			if err := installAppSync(key); err != nil {
				if t.canceled() {
					return
				}
				t.fail("环境安装失败（" + key + "）: " + err.Error())
				return
			}
			t.logf("%s 安装完成", key)
		}
	}

	if t.canceled() {
		return
	}
	workDir := filepath.Join(migrateRoot(), "import-bt-"+t.ID)
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)

	// 恢复网站
	restored := 0
	for _, name := range req.Sites {
		if t.canceled() {
			return
		}
		var btSite map[string]any
		for _, s := range sites {
			if toStr(s["name"]) == name {
				btSite = s
				break
			}
		}
		if btSite == nil {
			t.fail(fmt.Sprintf("对端面板上未找到网站 %s", name))
			return
		}
		t.logf("迁移网站 %s ...", name)
		if err := restoreBTSite(t, client, btSite, workDir, req); err != nil {
			if t.canceled() {
				return
			}
			if req.Overwrite {
				t.logf("网站 %s 迁移失败: %v", name, err)
				continue
			}
			t.fail("网站 " + name + " 迁移失败: " + err.Error())
			return
		}
		restored++
		t.logf("网站 %s 迁移完成", name)
	}

	// 恢复数据库（备份文件通过对端面板端口 /download 直下，不经过站点域名）
	for _, dbName := range req.Databases {
		if t.canceled() {
			return
		}
		t.logf("迁移数据库 %s ...", dbName)
		if err := restoreBTDatabase(t, client, dbs, dbName, workDir, req.NewDBPassword); err != nil {
			if t.canceled() {
				return
			}
			t.fail("数据库 " + dbName + " 迁移失败: " + err.Error())
			return
		}
		t.logf("数据库 %s 迁移完成", dbName)
	}

	// 恢复 FTP
	for _, uname := range req.FTPs {
		if t.canceled() {
			return
		}
		pass := req.NewFTPPassword
		if pass == "" {
			pass = randomPassword(12)
		}
		home := filepath.Join(webRootBase, uname)
		for _, f := range ftps {
			if toStr(f["username"]) == uname {
				if p := toStr(f["path"]); p != "" {
					home = p
				}
				break
			}
		}
		t.logf("创建 FTP 账号 %s（密码 %s）...", uname, pass)
		if err := CreateFtpUser(CreateFtpUserReq{Username: uname, Password: pass, HomeDir: home}); err != nil {
			if t.canceled() {
				return
			}
			t.fail("FTP 账号 " + uname + " 创建失败: " + err.Error())
			return
		}
		t.logf("FTP 账号 %s 创建完成", uname)
	}

	t.mu.Lock()
	t.Status = "success"
	t.mu.Unlock()
	t.logf("迁移完成：网站 %d 个、数据库 %d 个、FTP %d 个", restored, len(req.Databases), len(req.FTPs))
}

// restoreBTSite 从对端面板恢复单个网站到本机
func restoreBTSite(t *ImportTask, client *BTClient, btSite map[string]any, workDir string, req ImportRunRequest) error {
	name := toStr(btSite["name"])
	phpVer := normBTPhpVersion(toStr(btSite["php_version"]))
	projType := toStr(btSite["project_type"])
	typ := mapBTProjectType(projType)
	domains := parseBTDomains(btSite["domain"])
	primary := ""
	if len(domains) > 0 {
		primary = domains[0]
	}
	// 宝塔部分站点（反向代理/站点名即域名）domain 字段可能为空，回退用站点名作为主域名
	if primary == "" {
		primary = name
	}

	var count int64
	model.DB.Model(&model.Site{}).Where("name = ?", name).Count(&count)
	if count > 0 {
		if !req.Overwrite {
			return errors.New("同名网站已存在（如需覆盖请勾选覆盖选项）")
		}
		// 覆盖模式：先删除本机同名站点，再按源端重新创建
		t.logf("检测到本机已存在同名网站 %s，执行覆盖迁移 ...", name)
		var old model.Site
		if err := model.DB.Where("name = ?", name).First(&old).Error; err != nil {
			return fmt.Errorf("覆盖迁移失败：查询本机同名网站出错: %w", err)
		}
		if _, err := DeleteSite(DeleteSiteReq{ID: old.ID, DelRoot: true, DelDB: false}); err != nil {
			return fmt.Errorf("覆盖迁移失败：删除本机同名网站 %s 出错: %w", name, err)
		}
	}

	// 端口：对端面板返回了有效端口则沿用，否则默认 80
	// （CreateSite 要求 Port 必须在 1-65535，不填会直接报错）
	port := toInt(btSite["port"])
	if port <= 0 || port > 65535 {
		port = 80
	}
	createReq := CreateSiteReq{
		Name:   name,
		Domain: primary,
		Type:   typ,
		Port:   port,
		Remark: "从对端面板迁移",
	}
	if len(domains) > 1 {
		createReq.Domain = strings.Join(domains, ",")
	}
	if typ == model.SiteTypePHP {
		v := phpVer
		if v == "" {
			v = "74"
		}
		// 宝塔版本号是两位简写（74），本机 RuntimeVersion 需带点格式（PHP 7.4），
		// 否则 ensureRuntime 会在 PATH 里找 php74 而实际二进制名是 php7.4
		createReq.RuntimeVersion = "PHP " + btPhpVersionDotted(v)
	}
	s, err := CreateSite(createReq)
	if err != nil {
		return err
	}

	pkgFile := filepath.Join(workDir, name+".tar.gz")
	t.logf("下载网站 %s 文件中...", name)
	if err := downloadBTSitePackage(t, client, btSite, pkgFile); err != nil {
		return err
	}

	if s.Root != "" {
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return err
		}
		if err := ungzTar(pkgFile, s.Root); err != nil {
			return fmt.Errorf("解压网站文件失败: %w", err)
		}
		// 宝塔压缩包通常会把站点目录本身包一层（如解压后出现 /root/api.vitphp.cn/），
		// 需要把这一层子目录里的文件上移到网站根目录，避免访问路径多套一层。
		if err := flattenBTSiteRoot(s.Root, name); err != nil {
			return fmt.Errorf("整理网站目录层级失败: %w", err)
		}
		// 解压后的文件带的是宝塔源端属主/权限（常为 root），统一改为本机 Web 运行用户
		// （www-data 等），避免属主为 root 导致权限过高、被入侵后波及整台服务器的风险。
		if err := ChownToWebUser(s.Root, true); err != nil {
			t.logf("网站 %s 文件归属调整失败（可忽略）: %v", name, err)
		}
		// 清理源面板残留的 .user.ini（宝塔等会带，且常带 immutable 位），
		// 避免其 open_basedir 干扰本面板生成的配置，也防止 immutable 位导致文件无法在管理器内删除
		_, _ = ExecCommand("chattr -i "+shellQuote(filepath.Join(s.Root, ".user.ini")), 5*time.Second)
		_ = os.Remove(filepath.Join(s.Root, ".user.ini"))
	}

	// 运行目录：源站 nginx 的 root 相对站点 path 的路径（如 public），
	// 让迁移后的 PHP 站点 root 指向实际可访问目录（ThinkPHP/Laravel 等框架）
	if typ == model.SiteTypePHP {
		if rd := btSiteRunDir(client, btSite, primary, name, s.Root); rd != "" {
			s.RuntimeDir = rd
			t.logf("网站 %s 运行目录设置为 %s", name, rd)
		}
	}

	// 伪静态：读取源站点的 rewrite 规则并写入本机
	if typ == model.SiteTypePHP || typ == model.SiteTypeStatic {
		if rw := client.GetSiteRewrite(name, primary); rw != "" {
			s.Rewrite = rw
			t.logf("网站 %s 伪静态规则已读取", name)
		}
	}

	// SSL 证书：读取源站点证书与私钥并写入本机（兼容宝塔不同版本证书存放路径）
	if cert, key := client.GetSiteSSLCert(primary); cert != "" && key != "" {
		certPath, keyPath := siteSSLPath(s.Name)
		if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
			t.logf("创建证书目录失败（跳过 SSL）: %v", err)
		} else if err := os.WriteFile(certPath, []byte(cert), 0o600); err != nil {
			t.logf("写入证书文件失败（跳过 SSL）: %v", err)
		} else if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
			t.logf("写入私钥文件失败（跳过 SSL）: %v", err)
		} else {
			s.SslEnabled = true
			s.SslCertPath = certPath
			s.SslKeyPath = keyPath
			t.logf("网站 %s SSL 证书已恢复", name)
		}
	}

	// 统一保存配置并生成站点配置
	if err := model.DB.Save(s).Error; err != nil {
		return fmt.Errorf("保存网站 %s 配置失败: %w", name, err)
	}
	if err := writeSiteConfAndReload(s); err != nil {
		t.logf("生成网站 %s 配置失败（可到站点设置里手动配置）: %v", name, err)
	}

	if err := webReload(); err != nil {
		return fmt.Errorf("重载 web 服务器失败: %w", err)
	}
	return nil
}

// flattenBTSiteRoot 整理宝塔站点备份多包的一层目录。
// 宝塔压缩站点目录时通常会把 /www/wwwroot/<name> 整体打包，解压到网站根目录后
// 会多出一层 <name>/ 子目录。本函数把该子目录里的所有文件/目录上移到网站根目录，
// 并覆盖根目录下同名文件（如 kypanel 默认生成的 index.html/404.html），最后删除空子目录。
func flattenBTSiteRoot(root, name string) error {
	nested := filepath.Join(root, name)
	info, err := os.Stat(nested)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(nested)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(nested, e.Name())
		dst := filepath.Join(root, e.Name())
		if _, err := os.Stat(dst); err == nil {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("清理目标 %s 失败: %w", dst, err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("移动 %s -> %s 失败: %w", src, dst, err)
		}
	}
	if err := os.Remove(nested); err != nil {
		return fmt.Errorf("删除空目录 %s 失败: %w", nested, err)
	}
	return nil
}

// zipBTSiteDir 用 files?action=Zip 把站点目录压缩到对端备份目录，
// 作为 BackupSite 接口失败时的兜底（仅依赖文件压缩，不依赖站点备份功能）。
// 压缩产物落在 /www/backup/site/<站点名>/ 下，WaitSiteBackup/NewestSiteBackup 可直接发现。
func zipBTSiteDir(client *BTClient, btSite map[string]any, name string) error {
	sitePath := strings.TrimRight(toStr(btSite["path"]), "/")
	if sitePath == "" {
		return errors.New("站点根目录为空")
	}
	dir := "/www/backup/site/" + name
	client.CreateRemoteDir(dir)
	zfile := fmt.Sprintf("%s_web_%s.tar.gz", name, time.Now().Format("20060102_150405"))
	return client.ZipDir(sitePath, dir+"/"+zfile, "tar.gz")
}

// btSiteRunDir 计算源站运行目录：nginx root 相对站点 path 的相对路径（如 public），
// 若 root 与站点 path 相同则返回空（使用站点根目录）。
// primary 为源站主域名，name 为源站站点名（宝塔 vhost 配置可能按二者之一命名），
// rootDir 为本机解压后的站点根目录，用于源端配置解析失败时兜底探测常见框架运行目录。
func btSiteRunDir(client *BTClient, btSite map[string]any, primary, name, rootDir string) string {
	sitePath := strings.TrimRight(toStr(btSite["path"]), "/")
	if sitePath == "" {
		sitePath = rootDir
	}
	if primary == "" {
		primary = name
	}
	// 优先从对端 nginx 配置解析运行目录（配置文件可能以主域名或站点名命名）
	webRoot := client.GetSiteWebRoot(primary)
	if webRoot == "" && name != "" && name != primary {
		webRoot = client.GetSiteWebRoot(name)
	}
	webRoot = strings.TrimRight(webRoot, "/")
	if webRoot != "" && webRoot != sitePath && strings.HasPrefix(webRoot, sitePath+"/") {
		return strings.TrimPrefix(webRoot, sitePath+"/")
	}
	// 兜底：源端配置无法解析时，探测本机解压目录下的常见 PHP 框架运行目录
	// （站点文件已下载到 rootDir，存在 public/index.php 等即视为框架入口目录）
	for _, cand := range []string{"public", "public_html", "web", "dist"} {
		if info, err := os.Stat(filepath.Join(rootDir, cand, "index.php")); err == nil && !info.IsDir() {
			return cand
		}
	}
	return ""
}

// downloadBTSitePackage 下载对端面板网站文件：
// 直接用 files?action=Zip 压缩站点目录（不走宝塔网站备份接口，各版本参数差异大），
// 再通过对端面板端口 /download 直下（不依赖站点域名/SSL/伪静态）。
func downloadBTSitePackage(t *ImportTask, client *BTClient, btSite map[string]any, dest string) error {
	name := toStr(btSite["name"])
	if err := zipBTSiteDir(client, btSite, name); err != nil {
		return fmt.Errorf("对端面板压缩网站目录失败: %w", err)
	}
	p, err := client.WaitSiteBackup(name, 10*time.Minute)
	if err != nil {
		return err
	}
	if err := client.DownloadViaPanel(t.Ctx(), p, dest, func(done, total int64) {
		t.setProgress("downloading_site", "下载网站文件", name, done, total)
	}); err != nil {
		t.clearProgress()
		if t.canceled() {
			return err
		}
		return fmt.Errorf("下载网站文件失败: %w", err)
	}
	t.clearProgress()
	return nil
}

// isGzipFile 校验文件是否为 gzip 压缩（读取魔数 1f 8b）
func isGzipFile(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [2]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic[0] == 0x1f && magic[1] == 0x8b
}

// restoreBTDatabase 从对端面板恢复单个数据库到本机
func restoreBTDatabase(t *ImportTask, client *BTClient, btDbs []map[string]any, dbName, workDir, newPassword string) error {
	var dbID, srcPassword string
	for _, d := range btDbs {
		if toStr(d["name"]) == dbName {
			dbID = strFromAny(d["id"])
			srcPassword = toStr(d["password"])
			break
		}
	}
	if dbID == "" {
		return fmt.Errorf("对端面板上未找到数据库 %s", dbName)
	}

	t.logf("触发对端面板数据库 %s 备份 ...", dbName)
	if err := client.DatabaseBackupNow(dbID); err != nil {
		return fmt.Errorf("对端面板数据库备份失败: %w", err)
	}

	var remotePath string
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if p := client.NewestDBBackup(dbName); p != "" {
			remotePath = p
			break
		}
		time.Sleep(3 * time.Second)
	}
	if remotePath == "" {
		return errors.New("等待对端面板数据库备份超时")
	}

	t.logf("下载数据库 %s 备份（%s）...", dbName, remotePath)
	sqlFile := filepath.Join(workDir, dbName+".sql.gz")
	if err := client.DownloadViaPanel(t.Ctx(), remotePath, sqlFile, func(done, total int64) {
		t.setProgress("downloading_db", "下载数据库备份", dbName, done, total)
	}); err != nil {
		t.clearProgress()
		if t.canceled() {
			return err
		}
		return fmt.Errorf("下载数据库备份失败: %w", err)
	}
	t.clearProgress()

	pass := newPassword
	if pass == "" {
		if srcPassword != "" {
			pass = srcPassword
		} else {
			pass = randomPassword(16)
		}
	}
	err := CreateDatabase(CreateDatabaseReq{Name: dbName, User: dbName, Password: pass, Charset: "utf8mb4"})
	if err != nil && !strings.Contains(err.Error(), "已存在") {
		return err
	}
	// 导入由 MySQL 服务端执行，无法精确计算百分比，仅给出阶段提示避免界面长时间无变化
	t.setProgress("importing_db", "导入数据库", dbName, 0, 0)
	if err := DatabaseImport("mysql", dbName, sqlFile); err != nil {
		t.clearProgress()
		return fmt.Errorf("导入数据库失败: %w", err)
	}
	t.clearProgress()
	return nil
}

// DetectPanel 自动探测面板类型（kypanel / bt）
func DetectPanel(panelURL, token string) (map[string]any, error) {
	if panelURL == "" || token == "" {
		return nil, errors.New("请填写面板地址和 API 密钥")
	}
	panelURL = NormalizePanelURL(panelURL)
	// 先尝试 kypanel
	rp := NewRemotePanel(panelURL, token)
	kpErr := rp.Ping()
	if kpErr == nil {
		return map[string]any{
			"panel_type": "kypanel",
			"version":    rp.Version,
		}, nil
	}
	// 再尝试对端面板
	bt := NewBTClient(panelURL, token)
	_, btErr := bt.SiteList()
	if btErr == nil {
		return map[string]any{
			"panel_type": "bt",
			"version":    "对端面板",
		}, nil
	}
	return nil, fmt.Errorf("无法连接面板或密钥错误，请检查地址和 API 密钥（kypanel: %v；对端面板: %v）", kpErr, btErr)
}

// ---- 对端面板类型/字段转换辅助 ----

func normBTPhpVersion(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "php-")
	v = strings.TrimPrefix(v, "php")
	var d strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	return d.String()
}

// btPhpVersionDotted 把宝塔两位版本号（如 74）转成带点格式（7.4）
func btPhpVersionDotted(v string) string {
	if len(v) >= 2 {
		return v[:1] + "." + v[1:]
	}
	return v
}

func mapBTProjectType(t string) string {
	switch strings.ToLower(t) {
	case "php", "1":
		return model.SiteTypePHP
	case "node", "nodejs":
		return model.SiteTypeNode
	case "python":
		return model.SiteTypePython
	case "go", "golang":
		return model.SiteTypeGo
	case "static":
		return model.SiteTypeStatic
	case "proxy":
		return model.SiteTypeProxy
	default:
		return model.SiteTypeStatic
	}
}

func parseBTDomains(v any) []string {
	var out []string
	switch d := v.(type) {
	case string:
		var arr []string
		if json.Unmarshal([]byte(d), &arr) == nil {
			out = arr
		} else {
			// 宝塔 domain 字段通常为空格分隔（"a.com www.a.com"），部分版本为逗号分隔
			for _, p := range strings.FieldsFunc(d, func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
			}) {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
		}
	case []any:
		for _, i := range d {
			out = append(out, fmt.Sprint(i))
		}
	case []string:
		out = d
	}
	return out
}

func strFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return fmt.Sprint(int(f))
	}
	return fmt.Sprint(v)
}
