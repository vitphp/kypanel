package service

// ==================== 对端面板 → kypanel 迁入 ====================
// 通过对端面板官方 API 将对端面板网站/数据库/FTP 迁入本面板。

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	if len(picked) == 0 {
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
			t.logf("安装 %s 中...", key)
			if err := installAppSync(key); err != nil {
				t.fail("环境安装失败（" + key + "）: " + err.Error())
				return
			}
			t.logf("%s 安装完成", key)
		}
	}

	workDir := filepath.Join(migrateRoot(), "import-bt-"+t.ID)
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)

	// 恢复网站
	restored := 0
	for _, name := range req.Sites {
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

	// 恢复数据库（对端文件直链传输的中转域名：优先用本次迁移的站点域名，确保可公网访问）
	webDomains := []string{}
	seen := map[string]bool{}
	for _, name := range req.Sites {
		for _, s := range sites {
			if toStr(s["name"]) != name {
				continue
			}
			for _, d := range buildBTDownloadDomains(client, s) {
				if !seen[d] {
					seen[d] = true
					webDomains = append(webDomains, d)
				}
			}
			break
		}
	}
	// 兜底：全部站点里的域名
	if len(webDomains) == 0 {
		for _, s := range sites {
			for _, d := range buildBTDownloadDomains(client, s) {
				if !seen[d] {
					seen[d] = true
					webDomains = append(webDomains, d)
				}
			}
		}
	}
	for _, dbName := range req.Databases {
		t.logf("迁移数据库 %s ...", dbName)
		if err := restoreBTDatabase(t, client, dbs, dbName, workDir, req.NewDBPassword, webDomains); err != nil {
			t.fail("数据库 " + dbName + " 迁移失败: " + err.Error())
			return
		}
		t.logf("数据库 %s 迁移完成", dbName)
	}

	// 恢复 FTP
	for _, uname := range req.FTPs {
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
	if err := downloadBTSitePackage(client, btSite, pkgFile); err != nil {
		return err
	}

	if s.Root != "" {
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return err
		}
		if err := ungzTar(pkgFile, s.Root); err != nil {
			return fmt.Errorf("解压网站文件失败: %w", err)
		}
	}

	// 运行目录：源站 nginx 的 root 相对站点 path 的路径（如 public），
	// 让迁移后的 PHP 站点 root 指向实际可访问目录（ThinkPHP/Laravel 等框架）
	if typ == model.SiteTypePHP {
		if rd := btSiteRunDir(client, btSite); rd != "" {
			s.RuntimeDir = rd
			model.DB.Save(s)
			if err := writeSiteConfAndReload(s); err != nil {
				t.logf("设置网站 %s 运行目录 %s 失败（可到站点设置里手动配置）: %v", name, rd, err)
			} else {
				t.logf("网站 %s 运行目录设置为 %s", name, rd)
			}
		}
	}

	if err := webReload(); err != nil {
		return fmt.Errorf("重载 web 服务器失败: %w", err)
	}
	return nil
}

// btSiteRunDir 计算源站运行目录：nginx root 相对站点 path 的相对路径（如 public），
// 若 root 与站点 path 相同则返回空（使用站点根目录）
func btSiteRunDir(client *BTClient, btSite map[string]any) string {
	sitePath := strings.TrimRight(toStr(btSite["path"]), "/")
	if sitePath == "" {
		return ""
	}
	domain := toStr(btSite["domain"])
	if domain == "" {
		domain = toStr(btSite["name"])
	}
	webRoot := client.GetSiteWebRoot(domain)
	if webRoot == "" {
		return ""
	}
	webRoot = strings.TrimRight(webRoot, "/")
	if webRoot == sitePath {
		return ""
	}
	if strings.HasPrefix(webRoot, sitePath+"/") {
		return strings.TrimPrefix(webRoot, sitePath+"/")
	}
	return ""
}

// resumeDownloadHTTP 从 URL 断点续传下载到本地文件（对端站点备份可能很大）
func resumeDownloadHTTP(url, dest string) error {
	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	offset, _ := f.Seek(0, io.SeekEnd)
	attempts := 0
	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := client.Do(req)
		if err != nil {
			if attempts < 3 {
				attempts++
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("下载对端面板文件失败: %w", err)
		}
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			resp.Body.Close()
			return nil // 已下载完成
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return fmt.Errorf("下载对端面板文件失败: HTTP %d", resp.StatusCode)
		}
		n, err := io.Copy(f, resp.Body)
		resp.Body.Close()
		offset += n
		if err != nil {
			if attempts < 3 {
				attempts++
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("下载对端面板文件失败: %w", err)
		}
		// 无 Range 响应或下载长度 < 块大小说明已完成（服务器可能不支持 Range，一次拉完）
		return nil
	}
}

// downloadBTSitePackage 下载对端面板网站文件：优先用已有备份，否则触发备份，
// 再通过对端站点 web 根目录直链传输（兼容宝塔新版无文件下载/打包 API 的面板）。
//
// 备份查找用 GetDirNew 直列 /www/backup/site/<站点名>/ 目录，而不是 backup 表：
// 宝塔 backup 表按 id 查会返回全表（id 是备份自增主键，与站点 id 无关），
// 曾导致拿错数据库备份当网站包下载，解压报 "This does not look like a tar archive"。
func downloadBTSitePackage(client *BTClient, btSite map[string]any, dest string) error {
	name := toStr(btSite["name"])
	remotePath := client.NewestSiteBackup(name)
	if remotePath == "" {
		if err := client.BackupSiteNow(name); err != nil {
			return fmt.Errorf("对端面板网站备份失败: %w", err)
		}
		p, err := client.WaitSiteBackup(name, 10*time.Minute)
		if err != nil {
			return err
		}
		remotePath = p
	}
	domains := buildBTDownloadDomains(client, btSite)
	if err := client.DownloadViaWebCandidates(remotePath, domains, dest); err != nil {
		return err
	}
	// 下载后再校验是 gzip 包，避免拿到非 tar 文件时才在解压阶段报出难懂的错
	if !isGzipFile(dest) {
		return fmt.Errorf("下载的网站备份不是有效的 tar.gz 文件（对端路径 %s，请检查该站点的备份文件）", remotePath)
	}
	return nil
}

// buildBTDownloadDomains 构建对端面板文件直链传输的候选域名列表：
// 站点记录里的 domain 字段 + 站点名 + nginx 配置里的 server_name
func buildBTDownloadDomains(client *BTClient, btSite map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, d := range parseBTDomains(btSite["domain"]) {
		add(d)
	}
	add(toStr(btSite["domain"]))
	add(toStr(btSite["name"]))
	// 从站点 nginx 配置补充 server_name（部分站点 domain 字段为空或为数字）
	for _, cand := range out {
		for _, d := range client.btSiteConfServerNames(cand) {
			add(d)
		}
	}
	return out
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
func restoreBTDatabase(t *ImportTask, client *BTClient, btDbs []map[string]any, dbName, workDir, newPassword string, webDomains []string) error {
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
	if err := client.DownloadViaWebCandidates(remotePath, webDomains, sqlFile); err != nil {
		return fmt.Errorf("下载对端面板数据库备份失败: %w", err)
	}

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
	if err := DatabaseImport("mysql", dbName, sqlFile); err != nil {
		return fmt.Errorf("导入数据库失败: %w", err)
	}
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
