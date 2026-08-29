package service

// ==================== 对端面板 → kypanel 迁入 ====================
// 通过对端面板官方 API 将对端面板网站/数据库/FTP 迁入本面板。

import (
	"encoding/json"
	"errors"
	"fmt"
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
			PhpVersion: toStr(s["php_version"]),
		}
		ms.DBs = toStringSlice(s["dbs"])
		ms.FTPs = toStringSlice(s["ftps"])
		manifest.Sites = append(manifest.Sites, ms)
		for _, d := range ms.DBs {
			manifest.Databases = append(manifest.Databases, MigrateDB{Name: d, User: d})
		}
		for _, f := range ms.FTPs {
			manifest.FTPs = append(manifest.FTPs, MigrateFTP{Username: f})
		}
	}
	if len(picked) == 0 {
		return nil, errors.New("对端面板上未找到选中的网站")
	}
	env := MigrateEnvStatus()
	missing := CompareEnvForMigration(&manifest, env)
	return &ImportPlanResult{
		PanelVersion: "对端面板",
		Env:          env,
		Sites:        picked,
		Missing:      missing,
		DBsCount:     len(manifest.Databases),
		FTPsCount:    len(manifest.FTPs),
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

	// 恢复数据库
	for _, dbName := range req.Databases {
		t.logf("迁移数据库 %s ...", dbName)
		if err := restoreBTDatabase(t, client, dbs, dbName, workDir, req.NewDBPassword); err != nil {
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

	var count int64
	model.DB.Model(&model.Site{}).Where("name = ?", name).Count(&count)
	if count > 0 {
		return errors.New("同名网站已存在（如需覆盖请勾选覆盖选项）")
	}

	createReq := CreateSiteReq{
		Name:   name,
		Domain: primary,
		Type:   typ,
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
		createReq.RuntimeVersion = "PHP " + v
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

	if err := webReload(); err != nil {
		return fmt.Errorf("重载 web 服务器失败: %w", err)
	}
	return nil
}

// downloadBTSitePackage 下载对端面板网站文件：优先用已有备份，否则现打包
func downloadBTSitePackage(client *BTClient, btSite map[string]any, dest string) error {
	name := toStr(btSite["name"])
	root := toStr(btSite["path"])
	siteID := strFromAny(btSite["id"])

	if siteID != "" {
		if backups, err := client.SiteBackupList(siteID); err == nil && len(backups) > 0 {
			b := backups[0]
			fname := strFromAny(b["filename"])
			if fname == "" {
				fname = strFromAny(b["name"])
			}
			if fname != "" {
				remotePath := filepath.Join("/www/backup/site", fname)
				if err := client.DownloadFile(remotePath, dest); err == nil {
					return nil
				}
			}
		}
	}

	if root == "" {
		return errors.New("无法获取对端面板网站根目录")
	}
	zfile := "kypanel_import_" + name + ".tar.gz"
	if err := client.ZipDir(root, "/www/backup", zfile, "tar.gz"); err != nil {
		return fmt.Errorf("对端面板打包网站失败: %w", err)
	}
	remotePath := filepath.Join("/www/backup", zfile)
	defer client.DeleteFile(remotePath)
	return client.DownloadFile(remotePath, dest)
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

	var fname string
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		backups, err := client.DatabaseBackupList(dbID)
		if err == nil && len(backups) > 0 {
			fname = strFromAny(backups[0]["name"])
			if fname != "" {
				break
			}
		}
		time.Sleep(3 * time.Second)
	}
	if fname == "" {
		return errors.New("等待对端面板数据库备份超时")
	}

	t.logf("下载数据库 %s 备份 ...", dbName)
	sqlGz := filepath.Join(workDir, dbName+".sql.gz")
	remotePath := filepath.Join("/www/backup/database", fname)
	if err := client.DownloadFile(remotePath, sqlGz); err != nil {
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
	if err := DatabaseImport("mysql", dbName, sqlGz); err != nil {
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
			out = strings.Split(d, ",")
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
