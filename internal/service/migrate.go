package service

// ==================== 网站搬家（迁移） ====================
// 支持：kypanel ↔ kypanel、kypanel → 对端面板
// 数据流：源端打包 → 生成迁移包(tar.gz) → 目标端下载 → 环境对比 → 恢复
// 迁移包目录结构（解压后）：
//   manifest.json
//   sites/<site_name>/site.json        # 站点配置快照
//   sites/<site_name>/wwwroot.tar.gz   # 网站根目录文件
//   databases/<db_name>.sql.gz         # 数据库导出（gzip）
//
// 说明：FTP 密码在面板中不存明文，迁出时仅记录用户名与家目录，
// 迁入时重建同名账号并重置密码（由用户输入或自动生成）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ---------------- 迁移包数据结构 ----------------

// MigrateSite 站点配置快照
type MigrateSite struct {
	Name           string   `json:"name"`
	Domain         string   `json:"domain"`
	Domains        string   `json:"domains"`
	Port           int      `json:"port"`
	Type           string   `json:"type"` // static / php / node / python / go / proxy
	Root           string   `json:"root"`
	RuntimeDir     string   `json:"runtime_dir"`
	RuntimeVersion string   `json:"runtime_version"`
	PhpVersion     string   `json:"php_version"` // 如 "74"（PHP 站点）
	SslEnabled     bool     `json:"ssl_enabled"`
	SslForce       bool     `json:"ssl_force"`
	SslCert        string   `json:"ssl_cert,omitempty"`
	SslKey         string   `json:"ssl_key,omitempty"`
	ConfigOverride string   `json:"config_override,omitempty"`
	ProxyPass      string   `json:"proxy_pass,omitempty"`
	ProxyPort      int      `json:"proxy_port,omitempty"`
	StartCommand   string   `json:"start_command,omitempty"`
	EnvVars        string   `json:"env_vars,omitempty"`
	Framework      string   `json:"framework,omitempty"`
	DefaultIndex   string   `json:"default_index,omitempty"`
	Rewrite        string   `json:"rewrite,omitempty"`
	RedirectURL    string   `json:"redirect_url,omitempty"`
	RedirectCode   int      `json:"redirect_code,omitempty"`
	Remark         string   `json:"remark,omitempty"`
	DBs            []string `json:"dbs"`
	FTPs           []string `json:"ftps"`
}

// MigrateDB 数据库快照（密码为明文，源面板记录）
type MigrateDB struct {
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
	Charset  string `json:"charset"`
}

// MigrateFTP FTP 账号快照（密码不可导出，迁入时重建）
type MigrateFTP struct {
	Username string `json:"username"`
	HomeDir  string `json:"home_dir"`
}

// MigrateManifest 迁移包清单
type MigrateManifest struct {
	SchemaVersion int           `json:"schema_version"`
	PanelType     string        `json:"panel_type"`
	PanelVersion  string        `json:"panel_version"`
	ExportedAt    string        `json:"exported_at"`
	Sites         []MigrateSite `json:"sites"`
	Databases     []MigrateDB   `json:"databases"`
	FTPs          []MigrateFTP  `json:"ftps"`
}

// MigrateExport 一个已生成的迁移包
type MigrateExport struct {
	ID        string          `json:"id"`
	CreatedAt string          `json:"created_at"`
	Size      int64           `json:"size"`
	File      string          `json:"file"` // tar.gz 文件名
	Manifest  MigrateManifest `json:"manifest"`
}

// ---------------- 数据目录 ----------------

func migrateRoot() string {
	return filepath.Join(config.Get().DataDir, "migrate")
}

func migrateExportDir(id string) string {
	return filepath.Join(migrateRoot(), id)
}

func migrateExportFile(id string) string {
	return migrateExportDir(id) + ".tar.gz"
}

// ---------------- 迁移包导出（源端） ----------------

// ExportMigration 本机打包选中的网站/数据库/FTP，生成迁移包
func ExportMigration(siteNames, dbNames, ftpNames []string) (*MigrateExport, error) {
	_ = os.MkdirAll(migrateRoot(), 0o755)
	id := "export-" + time.Now().Format("20060102150405")
	dir := migrateExportDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errors.New("创建迁移目录失败: " + err.Error())
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	manifest := MigrateManifest{
		SchemaVersion: 1,
		PanelType:     "kypanel",
		PanelVersion:  PanelVersion(),
		ExportedAt:    time.Now().Format("2006-01-02 15:04:05"),
	}

	// ---- 打包网站 ----
	for _, name := range siteNames {
		var s model.Site
		if err := model.DB.Where("name = ?", name).First(&s).Error; err != nil {
			cleanup()
			return nil, fmt.Errorf("网站 %s 不存在", name)
		}
		ms, err := snapshotSite(&s)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("网站 %s 快照失败: %w", name, err)
		}
		// 打包网站根目录文件
		if s.Root != "" && dirExists(s.Root) {
			dst := filepath.Join(dir, "sites", name)
			if err := os.MkdirAll(dst, 0o755); err != nil {
				cleanup()
				return nil, err
			}
			tarGzDir(s.Root, filepath.Join(dst, "wwwroot.tar.gz"))
		}
		// 记录 site.json
		data, _ := json.Marshal(ms)
		_ = os.MkdirAll(filepath.Join(dir, "sites", name), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "sites", name, "site.json"), data, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		manifest.Sites = append(manifest.Sites, ms)
	}

	// ---- 打包数据库 ----
	_ = os.MkdirAll(filepath.Join(dir, "databases"), 0o755)
	for _, name := range dbNames {
		if err := backupMysqlDatabase(name, filepath.Join(dir, "databases", name+".sql.gz")); err != nil {
			cleanup()
			return nil, fmt.Errorf("数据库 %s 导出失败: %w", name, err)
		}
		info := mysqlDatabaseInfo(name)
		manifest.Databases = append(manifest.Databases, info)
	}

	// ---- 记录 FTP 账号 ----
	for _, username := range ftpNames {
		var fu model.FtpUser
		if err := model.DB.Where("username = ?", username).First(&fu).Error; err != nil {
			cleanup()
			return nil, fmt.Errorf("FTP 账号 %s 不存在", username)
		}
		manifest.FTPs = append(manifest.FTPs, MigrateFTP{Username: fu.Username, HomeDir: fu.HomeDir})
	}

	// ---- 写清单 ----
	md, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), md, 0o644); err != nil {
		cleanup()
		return nil, err
	}

	// ---- 打成 tar.gz ----
	if err := tarGzDir(dir, migrateExportFile(id)); err != nil {
		cleanup()
		return nil, err
	}
	_ = os.RemoveAll(dir)

	st, err := os.Stat(migrateExportFile(id))
	if err != nil {
		cleanup()
		return nil, err
	}
	exp := &MigrateExport{
		ID:        id,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		Size:      st.Size(),
		File:      id + ".tar.gz",
		Manifest:  manifest,
	}
	return exp, nil
}

// ListMigrationExports 列出已生成的迁移包
func ListMigrationExports() []MigrateExport {
	_ = os.MkdirAll(migrateRoot(), 0o755)
	entries, err := os.ReadDir(migrateRoot())
	if err != nil {
		return nil
	}
	var list []MigrateExport
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".tar.gz")
		info, err := e.Info()
		if err != nil {
			continue
		}
		exp := MigrateExport{ID: id, CreatedAt: info.ModTime().Format("2006-01-02 15:04:05"), Size: info.Size(), File: e.Name()}
		// 尝试读取清单
		if mf, err := readManifestFromArchive(migrateExportFile(id)); err == nil {
			exp.Manifest = *mf
		}
		list = append(list, exp)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	return list
}

// DeleteMigrationExport 删除迁移包
func DeleteMigrationExport(id string) error {
	if !strings.HasPrefix(id, "export-") {
		return errors.New("非法迁移包 ID")
	}
	if err := os.Remove(migrateExportFile(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MigrationPackagePath 返回迁移包 tar.gz 绝对路径
func MigrationPackagePath(id string) (string, error) {
	if !strings.HasPrefix(id, "export-") {
		return "", errors.New("非法迁移包 ID")
	}
	p := migrateExportFile(id)
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("迁移包不存在")
	}
	return p, nil
}

// ---------------- 辅助函数 ----------------

func snapshotSite(s *model.Site) (MigrateSite, error) {
	ms := MigrateSite{
		Name:           s.Name,
		Domain:         s.Domain,
		Domains:        s.Domains,
		Port:           s.Port,
		Type:           s.Type,
		Root:           s.Root,
		RuntimeDir:     s.RuntimeDir,
		RuntimeVersion: s.RuntimeVersion,
		PhpVersion:     phpVersionFromFpm(s.PhpFpm),
		SslEnabled:     s.SslEnabled,
		SslForce:       s.SslForce,
		ConfigOverride: s.ConfigOverride,
		ProxyPass:      s.ProxyPass,
		ProxyPort:      s.ProxyPort,
		StartCommand:   s.StartCommand,
		EnvVars:        s.EnvVars,
		Framework:      s.Framework,
		DefaultIndex:   s.DefaultIndex,
		Rewrite:        s.Rewrite,
		RedirectURL:    s.RedirectURL,
		RedirectCode:   s.RedirectCode,
		Remark:         s.Remark,
	}
	// 读取 SSL 证书内容
	if s.SslEnabled {
		certPath, keyPath := siteSSLPath(s.Name)
		if cert, err := os.ReadFile(certPath); err == nil {
			ms.SslCert = string(cert)
		}
		if key, err := os.ReadFile(keyPath); err == nil {
			ms.SslKey = string(key)
		}
	}
	// 关联数据库与 FTP（按命名规范自动关联：网站名与库名/账号名相同或 HomeDir 指向站点目录）
	ms.DBs = relatedDBsForSite(s.Name)
	ms.FTPs = relatedFTPsForSite(s.Name, s.Root)
	return ms, nil
}

// phpVersionFromFpm 从 PhpFpm（如 unix:/run/php-fpm74.sock 或 127.0.0.1:9074）解析 PHP 版本
func phpVersionFromFpm(fpm string) string {
	if fpm == "" {
		return ""
	}
	n := strings.ToLower(fpm)
	if idx := strings.Index(n, "php-fpm"); idx >= 0 {
		rest := n[idx+len("php-fpm"):]
		var digits strings.Builder
		for _, r := range rest {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			} else {
				break
			}
		}
		return digits.String()
	}
	if idx := strings.Index(n, "php"); idx >= 0 {
		rest := n[idx+len("php"):]
		if len(rest) >= 2 && rest[0] >= '0' && rest[0] <= '9' && rest[1] >= '0' && rest[1] <= '9' {
			return rest[:2]
		}
	}
	return ""
}

// relatedDBsForSite 通过数据库账号的库名/用户名关联网站（库名或用户名包含网站名）
func relatedDBsForSite(siteName string) []string {
	infos, err := ListDatabasesByType(string(DBTypeMySQL))
	if err != nil {
		return nil
	}
	var out []string
	base := strings.ReplaceAll(siteName, ".", "_")
	for _, it := range infos {
		name, _ := it["name"].(string)
		user, _ := it["user"].(string)
		if name == siteName || name == base || user == siteName || user == base {
			out = append(out, name)
		}
	}
	return out
}

// relatedFTPsForSite 通过 FTP 家目录关联网站
func relatedFTPsForSite(siteName, siteRoot string) []string {
	var users []model.FtpUser
	model.DB.Find(&users)
	var out []string
	for _, u := range users {
		if u.HomeDir == siteRoot || u.HomeDir == filepath.Join(webRootBase, siteName) || u.HomeDir == siteName {
			out = append(out, u.Username)
		}
	}
	return out
}

// mysqlDatabaseInfo 获取数据库账号信息（明文密码）
func mysqlDatabaseInfo(dbName string) MigrateDB {
	info := MigrateDB{Name: dbName, Charset: "utf8mb4"}
	var acct model.DatabaseAccount
	if err := model.DB.Where("db_name = ?", dbName).Or("db_name = ?", dbName).First(&acct).Error; err == nil {
		info.User = acct.Username
		info.Password = acct.Password
	}
	if info.User == "" {
		info.User = dbName
	}
	return info
}

// ---------------- 环境状态与对比 ----------------

// MigrateEnvInfo 本机环境信息
type MigrateEnvInfo struct {
	WebServer   string   `json:"web_server"` // nginx / apache / none
	PHPVersions []string `json:"php_versions"`
	MySQL       bool     `json:"mysql"`
	FTP         bool     `json:"ftp"`
}

// MigrateEnvStatus 本机环境状态
func MigrateEnvStatus() MigrateEnvInfo {
	info := MigrateEnvInfo{WebServer: detectWebServer(), PHPVersions: listInstalledPHPVersions()}
	info.MySQL = appInstalled("mysql")
	info.FTP = appInstalled("ftp")
	return info
}

// appInstalled 通过应用记录判断某应用是否已安装
func appInstalled(key string) bool {
	rec, err := model.GetAppRecord(key)
	return err == nil && rec.Status == model.AppInstalled
}

// listInstalledPHPVersions 已安装的 PHP 版本列表（如 ["74","80"]）
func listInstalledPHPVersions() []string {
	var out []string
	for _, p := range phpVersions {
		if appInstalled(p.Key) {
			out = append(out, strings.TrimPrefix(p.Key, "php"))
		}
	}
	sort.Strings(out)
	return out
}

// MissingEnv 缺失的环境项
type MissingEnv struct {
	Name        string `json:"name"`
	Key         string `json:"key"` // 对应应用 key（php74 / mysql / nginx / ftp）
	Kind        string `json:"kind"` // php / mysql / web / ftp
	Installable bool   `json:"installable"`
}

// CompareEnvForMigration 对比目标机环境，返回缺失项
func CompareEnvForMigration(manifest *MigrateManifest, env MigrateEnvInfo) []MissingEnv {
	var missing []MissingEnv
	add := func(name, key, kind string) {
		for i := range missing {
			if missing[i].Key == key {
				return
			}
		}
		_, installable := findApp(key)
		missing = append(missing, MissingEnv{Name: name, Key: key, Kind: kind, Installable: installable})
	}

	for _, s := range manifest.Sites {
		if env.WebServer == "none" {
			add("Web 服务器", "nginx", "web")
		}
		switch s.Type {
		case model.SiteTypePHP:
			v := s.PhpVersion
			if v == "" {
				v = "74"
			}
			key := "php" + v
			found := false
			for _, pv := range env.PHPVersions {
				if pv == v {
					found = true
					break
				}
			}
			if !found {
				add("PHP "+v, key, "php")
			}
		case model.SiteTypeNode:
			if !appInstalled("node") {
				add("Node.js 运行时", "node", "node")
			}
		case model.SiteTypePython:
			if !appInstalled("python") {
				add("Python 运行时", "python", "python")
			}
		case model.SiteTypeGo:
			if !appInstalled("golang") {
				add("Go 运行时", "golang", "go")
			}
		}
	}
	if len(manifest.Databases) > 0 && !env.MySQL {
		add("MySQL 数据库", "mysql", "mysql")
	}
	if len(manifest.FTPs) > 0 && !env.FTP {
		add("FTP 服务", "ftp", "ftp")
	}
	return missing
}

// ---------------- 导入任务（目标端） ----------------

// ImportRunRequest 迁入请求
type ImportRunRequest struct {
	PanelType      string   `json:"panel_type"`       // kypanel / bt（源面板类型）
	PanelURL       string   `json:"panel_url"`        // 源面板地址
	PanelToken     string   `json:"panel_token"`      // 源面板 API 密钥
	Sites          []string `json:"sites"`            // 选中的网站
	Databases      []string `json:"databases"`        // 选中的数据库
	FTPs           []string `json:"ftps"`             // 选中的 FTP
	AutoInstall    []string `json:"auto_install"`     // 勾选自动安装的环境 key
	Overwrite      bool     `json:"overwrite"`        // 站点已存在时是否覆盖
	NewDBPassword  string   `json:"new_db_password"`  // 覆盖数据库密码（空用源端）
	NewFTPPassword string   `json:"new_ftp_password"` // FTP 新密码（必须设置）
}

// ImportTask 迁移任务状态
type ImportTask struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // running / success / failed
	Logs      []string  `json:"logs"`
	Error     string    `json:"error"`
	UpdatedAt time.Time `json:"updated_at"`
	mu        sync.Mutex
}

var (
	importTasks   = map[string]*ImportTask{}
	importTasksMu sync.Mutex
)

func newImportTask(id string) *ImportTask {
	t := &ImportTask{ID: id, Status: "running", Logs: []string{}, UpdatedAt: time.Now()}
	importTasksMu.Lock()
	importTasks[id] = t
	importTasksMu.Unlock()
	return t
}

// ImportTaskStatus 查询迁移任务状态（返回加锁后的副本，避免并发读写竞态）
func ImportTaskStatus(id string) (*ImportTask, error) {
	importTasksMu.Lock()
	defer importTasksMu.Unlock()
	t, ok := importTasks[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &ImportTask{
		ID:        t.ID,
		Status:    t.Status,
		Logs:      append([]string{}, t.Logs...),
		Error:     t.Error,
		UpdatedAt: t.UpdatedAt,
	}, nil
}

// StartImport 开始迁入（异步执行，返回任务 ID）
func StartImport(req ImportRunRequest) (string, error) {
	if req.PanelType != "kypanel" && req.PanelType != "bt" {
		return "", errors.New("不支持的面板类型")
	}
	if len(req.Sites) == 0 && len(req.Databases) == 0 && len(req.FTPs) == 0 {
		return "", errors.New("请至少选择一个迁移对象")
	}
	id := "import-" + time.Now().Format("20060102150405")
	task := newImportTask(id)
	go runImport(task, req)
	return id, nil
}

func (t *ImportTask) logf(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Logs = append(t.Logs, "["+time.Now().Format("15:04:05")+"] "+fmt.Sprintf(format, args...))
	t.UpdatedAt = time.Now()
}

func runImport(t *ImportTask, req ImportRunRequest) {
	defer func() {
		if r := recover(); r != nil {
			t.mu.Lock()
			t.Status = "failed"
			t.Error = fmt.Sprintf("%v", r)
			t.mu.Unlock()
			t.logf("迁移失败: %v", r)
		}
	}()

	// 1. 连接源面板
	t.logf("正在连接源面板 %s ...", req.PanelURL)
	var manifest *MigrateManifest
	var pkgPath string
	if req.PanelType == "kypanel" {
		remote := NewRemotePanel(req.PanelURL, req.PanelToken)
		if err := remote.Ping(); err != nil {
			t.mu.Lock()
			t.Status = "failed"
			t.Error = "连接源面板失败: " + err.Error()
			t.mu.Unlock()
			t.logf("%s", t.Error)
			return
		}
		t.logf("连接成功（%s）", remote.Version)

		// 2. 请求源端打包
		t.logf("正在请求源面板打包（网站 %d 个、数据库 %d 个、FTP %d 个）...", len(req.Sites), len(req.Databases), len(req.FTPs))
		pkgID, err := remote.Pack(req.Sites, req.Databases, req.FTPs)
		if err != nil {
			t.fail("请求源端打包失败: " + err.Error())
			return
		}
		t.logf("源端打包完成，正在下载迁移包...")

		// 3. 下载迁移包
		local, err := remote.Download(pkgID)
		if err != nil {
			t.fail("下载迁移包失败: " + err.Error())
			return
		}
		pkgPath = local
		t.logf("迁移包下载完成：%s", filepath.Base(local))

		// 4. 解析清单
		m, err := readManifestFromArchive(local)
		if err != nil {
			t.fail("解析迁移清单失败: " + err.Error())
			return
		}
		manifest = m
	} else if req.PanelType == "bt" {
		runImportFromBT(t, req)
		return
	} else {
		t.fail("未知源面板类型，请先连接并识别面板")
		return
	}

	// 5. 安装缺失环境
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
	} else if len(CompareEnvForMigration(manifest, MigrateEnvStatus())) > 0 {
		t.logf("注意：仍有缺失环境未勾选自动安装，迁移可能失败")
	}

	// 6. 解压迁移包
	workDir := filepath.Join(migrateRoot(), "import-"+t.ID)
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)
	if err := ungzTar(pkgPath, workDir); err != nil {
		t.fail("解压迁移包失败: " + err.Error())
		return
	}
	t.logf("迁移包解压完成")

	// 7. 恢复网站
	restored := 0
	for _, s := range manifest.Sites {
		if !containsStr(req.Sites, s.Name) {
			continue
		}
		t.logf("恢复网站 %s（%s）...", s.Name, s.Type)
		if err := restoreSite(t, workDir, &s, req); err != nil {
			if req.Overwrite {
				t.logf("网站 %s 恢复失败: %v", s.Name, err)
				continue
			}
			t.fail("网站 " + s.Name + " 恢复失败: " + err.Error())
			return
		}
		restored++
		t.logf("网站 %s 恢复完成", s.Name)
	}

	// 8. 恢复数据库
	for _, db := range manifest.Databases {
		if !containsStr(req.Databases, db.Name) {
			continue
		}
		t.logf("恢复数据库 %s ...", db.Name)
		if err := restoreDatabase(t, workDir, &db, req.NewDBPassword); err != nil {
			t.fail("数据库 " + db.Name + " 恢复失败: " + err.Error())
			return
		}
		t.logf("数据库 %s 恢复完成", db.Name)
	}

	// 9. 恢复 FTP
	for _, f := range manifest.FTPs {
		if !containsStr(req.FTPs, f.Username) {
			continue
		}
		pass := req.NewFTPPassword
		if pass == "" {
			pass = randomPassword(12)
		}
		t.logf("创建 FTP 账号 %s（密码 %s）...", f.Username, pass)
		home := f.HomeDir
		if home == "" {
			home = filepath.Join(webRootBase, f.Username)
		}
		if err := CreateFtpUser(CreateFtpUserReq{Username: f.Username, Password: pass, HomeDir: home}); err != nil {
			t.fail("FTP 账号 " + f.Username + " 创建失败: " + err.Error())
			return
		}
		t.logf("FTP 账号 %s 创建完成", f.Username)
	}

	t.mu.Lock()
	t.Status = "success"
	t.mu.Unlock()
	t.logf("迁移完成：网站 %d 个、数据库 %d 个、FTP %d 个", restored, len(req.Databases), len(req.FTPs))
}

func (t *ImportTask) fail(msg string) {
	t.mu.Lock()
	t.Status = "failed"
	t.Error = msg
	t.mu.Unlock()
	t.logf("%s", msg)
}

// restoreSite 恢复单个网站
func restoreSite(t *ImportTask, workDir string, ms *MigrateSite, req ImportRunRequest) error {
	// 检查是否已存在
	var count int64
	model.DB.Model(&model.Site{}).Where("name = ?", ms.Name).Count(&count)
	if count > 0 {
		return errors.New("同名网站已存在（如需覆盖请勾选覆盖选项）")
	}

	// 创建站点（Root 留空由目标面板按自身规则推断，保证目录存在）
	createReq := CreateSiteReq{
		Name:           ms.Name,
		Domain:         ms.Domain,
		Port:           ms.Port,
		Type:           ms.Type,
		RuntimeVersion: ms.RuntimeVersion,
		StartCommand:   ms.StartCommand,
		EnvVars:        ms.EnvVars,
		ProxyPort:      ms.ProxyPort,
		ProxyPass:      ms.ProxyPass,
		Framework:      ms.Framework,
		Remark:         ms.Remark,
	}
	if ms.Domains != "" {
		createReq.Domain = ms.Domain + "," + ms.Domains
	}
	// PHP 站点需要指定 PHP 版本
	if ms.Type == model.SiteTypePHP {
		v := ms.PhpVersion
		if v == "" {
			v = "74"
		}
		createReq.RuntimeVersion = "PHP " + v
	}
	s, err := CreateSite(createReq)
	if err != nil {
		return err
	}

	// 恢复网站文件
	srcWWW := filepath.Join(workDir, "sites", ms.Name, "wwwroot.tar.gz")
	if s.Root != "" && fileExists(srcWWW) {
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return err
		}
		if err := ungzTar(srcWWW, s.Root); err != nil {
			return fmt.Errorf("解压网站文件失败: %w", err)
		}
	}

	// 恢复 SSL 证书
	if ms.SslEnabled && ms.SslCert != "" && ms.SslKey != "" {
		certPath, keyPath := siteSSLPath(s.Name)
		if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(certPath, []byte(ms.SslCert), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(keyPath, []byte(ms.SslKey), 0o600); err != nil {
			return err
		}
		s.SslEnabled = true
		s.SslForce = ms.SslForce
		s.SslCertPath = certPath
		s.SslKeyPath = keyPath
	}

	// 恢复自定义配置
	if ms.ConfigOverride != "" {
		s.ConfigOverride = ms.ConfigOverride
	}
	model.DB.Save(&s)

	// 重载 web 服务器
	if err := webReload(); err != nil {
		return fmt.Errorf("重载 web 服务器失败: %w", err)
	}
	return nil
}

// restoreDatabase 恢复单个数据库
func restoreDatabase(t *ImportTask, workDir string, db *MigrateDB, newPassword string) error {
	pass := newPassword
	if pass == "" {
		pass = db.Password
	}
	if pass == "" {
		pass = randomPassword(16)
	}
	// 创建数据库
	err := CreateDatabase(CreateDatabaseReq{Name: db.Name, User: db.User, Password: pass, Charset: db.Charset})
	if err != nil && !strings.Contains(err.Error(), "已存在") {
		return err
	}
	// 导入 SQL
	sqlFile := filepath.Join(workDir, "databases", db.Name+".sql.gz")
	if fileExists(sqlFile) {
		return DatabaseImport("mysql", db.Name, sqlFile)
	}
	return nil
}

// ---------------- tar / 解压 工具 ----------------

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// tarGzDir 用系统 tar 压缩目录 dir 到 dest（dest 含 .tar.gz）
func tarGzDir(dir, dest string) error {
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	cmd := fmt.Sprintf("tar -czf %s -C %s .", shellQuote(dest), shellQuote(dir))
	res, err := ExecCommand(cmd, 600*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("tar 失败: %s %s", res.Stdout, res.Stderr)
	}
	return nil
}

// ungzTar 解压 tar.gz 到 dir
func ungzTar(src, dir string) error {
	_ = os.MkdirAll(dir, 0o755)
	cmd := fmt.Sprintf("tar -xzf %s -C %s", shellQuote(src), shellQuote(dir))
	res, err := ExecCommand(cmd, 600*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("解压失败: %s %s", res.Stdout, res.Stderr)
	}
	return nil
}

// readManifestFromArchive 从 tar.gz 中读取 manifest.json
func readManifestFromArchive(pkg string) (*MigrateManifest, error) {
	// 解压到临时目录
	tmp := filepath.Join(migrateRoot(), ".tmp-manifest-"+fmt.Sprint(time.Now().UnixNano()))
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	if err := ungzTar(pkg, tmp); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m MigrateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func randomPassword(n int) string {
	const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// installAppSync 同步安装应用（等安装完成）
func installAppSync(key string) error {
	if _, ok := findApp(key); !ok {
		return fmt.Errorf("应用 %s 不存在", key)
	}
	if appInstalled(key) {
		return nil
	}
	// 复用 InstallApp 异步安装 + 轮询
	if err := InstallApp(key, "", ""); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		if appInstalled(key) {
			return nil
		}
		if rec, err := model.GetAppRecord(key); err == nil && rec.Status == model.AppFailed {
			return fmt.Errorf("应用 %s 安装失败", key)
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("应用 %s 安装超时", key)
}

// PanelVersion 当前面板版本
func PanelVersion() string {
	return "1.3.0"
}

// ---------------- 迁入计划（环境对比） ----------------

// ImportPlanRequest 迁入计划请求
type ImportPlanRequest struct {
	PanelType  string   `json:"panel_type"` // 源面板类型：kypanel
	PanelURL   string   `json:"panel_url"`
	PanelToken string   `json:"panel_token"`
	Sites      []string `json:"sites"` // 选中的网站
}

// ImportPlanResult 迁入计划结果
type ImportPlanResult struct {
	PanelVersion string           `json:"panel_version"`
	Env          MigrateEnvInfo   `json:"env"`
	Sites        []map[string]any `json:"sites"`
	Missing      []MissingEnv     `json:"missing"`
	DBsCount     int              `json:"dbs_count"`
	FTPsCount    int              `json:"ftps_count"`
}

// ExportPrecheckRequest 迁出前环境预检请求
type ExportPrecheckRequest struct {
	PanelType  string   `json:"panel_type"`  // 目标面板类型：kypanel / bt
	PanelURL   string   `json:"panel_url"`   // 目标面板地址
	PanelToken string   `json:"panel_token"` // 目标面板 API 密钥
	Sites      []string `json:"sites"`
	Databases  []string `json:"databases"`
	FTPs       []string `json:"ftps"`
}

// ExportPrecheck 迁出前对比目标面板环境，返回缺失项
func ExportPrecheck(req ExportPrecheckRequest) (map[string]any, error) {
	if len(req.Sites) == 0 && len(req.Databases) == 0 && len(req.FTPs) == 0 {
		return nil, errors.New("请先选择要迁出的对象")
	}
	if req.PanelURL == "" || req.PanelToken == "" {
		return nil, errors.New("请填写目标面板地址和 API 密钥")
	}
	// 本机选中对象的快照清单（用于环境对比）
	var manifest MigrateManifest
	for _, name := range req.Sites {
		var s model.Site
		if err := model.DB.Where("name = ?", name).First(&s).Error; err != nil {
			continue
		}
		ms, err := snapshotSite(&s)
		if err != nil {
			continue
		}
		manifest.Sites = append(manifest.Sites, ms)
	}
	for _, n := range req.Databases {
		manifest.Databases = append(manifest.Databases, mysqlDatabaseInfo(n))
	}
	for _, u := range req.FTPs {
		var fu model.FtpUser
		if model.DB.Where("username = ?", u).First(&fu).Error == nil {
			manifest.FTPs = append(manifest.FTPs, MigrateFTP{Username: fu.Username, HomeDir: fu.HomeDir})
		}
	}

	switch req.PanelType {
	case "kypanel":
		remote := NewRemotePanel(req.PanelURL, req.PanelToken)
		if err := remote.Ping(); err != nil {
			return nil, err
		}
		env, err := remote.GetEnv()
		if err != nil {
			return nil, err
		}
		missing := CompareEnvForMigration(&manifest, env)
		return map[string]any{
			"target":     "kypanel",
			"version":    remote.Version,
			"env":        env,
			"missing":    missing,
			"all_ready":  len(missing) == 0,
		}, nil
	case "bt":
		res, err := BTEnvCompare(req.PanelURL, req.PanelToken, req.Sites)
		if err != nil {
			return nil, err
		}
		return map[string]any{"target": "bt", "result": res}, nil
	default:
		return nil, errors.New("不支持的目标面板类型")
	}
}

// FetchRemoteSites 拉取源面板网站列表（不对比环境）
func FetchRemoteSites(req ImportPlanRequest) ([]map[string]any, error) {
	if req.PanelURL == "" || req.PanelToken == "" {
		return nil, errors.New("请填写源面板地址和 API 密钥")
	}
	switch req.PanelType {
	case "kypanel":
		remote := NewRemotePanel(req.PanelURL, req.PanelToken)
		if err := remote.Ping(); err != nil {
			return nil, err
		}
		return remote.GetSites()
	case "bt":
		return FetchBTSites(req.PanelURL, req.PanelToken)
	default:
		return nil, errors.New("不支持的面板类型，请先连接并识别面板")
	}
}

// BuildImportPlan 连接源面板，拉取选中网站配置并对比本机环境
func BuildImportPlan(req ImportPlanRequest) (*ImportPlanResult, error) {
	if req.PanelURL == "" || req.PanelToken == "" {
		return nil, errors.New("请填写源面板地址和 API 密钥")
	}
	if req.PanelType == "bt" {
		return BuildBTImportPlan(req)
	}
	if req.PanelType != "kypanel" {
		return nil, errors.New("不支持的面板类型，请先连接并识别面板")
	}
	remote := NewRemotePanel(req.PanelURL, req.PanelToken)
	if err := remote.Ping(); err != nil {
		return nil, err
	}
	all, err := remote.GetSites()
	if err != nil {
		return nil, err
	}

	var manifest MigrateManifest
	var picked []map[string]any
	for _, s := range all {
		name, _ := s["name"].(string)
		if !containsStr(req.Sites, name) {
			continue
		}
		picked = append(picked, s)
		ms := MigrateSite{
			Name:           name,
			Domain:         toStr(s["domain"]),
			Domains:        toStr(s["domains"]),
			Port:           toInt(s["port"]),
			Type:           toStr(s["type"]),
			Root:           toStr(s["root"]),
			PhpVersion:     toStr(s["php_version"]),
			RuntimeVersion: toStr(s["runtime_version"]),
			SslEnabled:     toBool(s["ssl_enabled"]),
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
		return nil, errors.New("源面板上未找到选中的网站")
	}

	env := MigrateEnvStatus()
	missing := CompareEnvForMigration(&manifest, env)
	return &ImportPlanResult{
		PanelVersion: remote.Version,
		Env:          env,
		Sites:        picked,
		Missing:      missing,
		DBsCount:     len(manifest.Databases),
		FTPsCount:    len(manifest.FTPs),
	}, nil
}

// ---- 类型转换辅助 ----
func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func toStringSlice(v any) []string {
	var out []string
	switch list := v.(type) {
	case []any:
		for _, item := range list {
			out = append(out, fmt.Sprint(item))
		}
	case []string:
		out = list
	}
	return out
}

// panelPagePathSuffixes 用户复制面板地址时常把页面路径一起粘进来（如 /site、/database），
// 这些不是面板根地址，需要自动去掉；安全入口（随机字符串路径）则保留。
var panelPagePathSuffixes = []string{
	"site", "data", "database", "ftp", "files", "config", "control",
	"admin", "login", "home", "index", "soft", "crontab", "firewall",
	"ssl", "setting", "settings", "panel", "users", "user", "plugins",
	"safe", "logs", "tasks", "apps", "monitor", "firewalls", "websites",
}

// NormalizePanelURL 清洗面板地址：去掉误带的页面路径，保留安全入口。
func NormalizePanelURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for len(parts) > 0 {
		last := strings.ToLower(parts[len(parts)-1])
		last = strings.TrimSuffix(last, ".html")
		last = strings.TrimSuffix(last, ".php")
		last = strings.TrimSuffix(last, ".asp")
		last = strings.TrimSuffix(last, ".aspx")
		found := false
		for _, s := range panelPagePathSuffixes {
			if last == s {
				found = true
				break
			}
		}
		if !found {
			break
		}
		parts = parts[:len(parts)-1]
	}
	u.Path = "/" + strings.Join(parts, "/")
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}
