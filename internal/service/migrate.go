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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
	if missing == nil {
		missing = []MissingEnv{} // 避免 JSON 序列化为 null，前端 plan.missing.length 直接崩溃
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

// 任务类型：前端重新打开搬家工具时据此跳回对应的 Tab
const (
	TaskKindExport = "export" // 迁出到对端面板（网站迁出 Tab）
	TaskKindImport = "import" // 从源面板迁入本机（网站迁入 Tab）
)

// ItemResult 单个子任务的结果（用于前端按项展示成功/失败）
type ItemResult struct {
	Type    string `json:"type"`    // site / site-file / site-config / database / database-data / ftp
	Name    string `json:"name"`    // 网站名/库名/FTP 用户
	Status  string `json:"status"`  // success / failed / skipped
	Message string `json:"message"` // 详细信息（失败原因 / 备注）
}

// TaskProgress 迁移任务当前步骤的实时进度。
// 用于下载迁移包/网站文件、导入数据库等可能持续数分钟且原本毫无输出的步骤，
// 避免前端长时间停在一条日志上让用户误以为卡死。
type TaskProgress struct {
	Phase     string  `json:"phase"`      // 阶段标识（downloading_site / downloading_db / importing_db 等）
	Label     string  `json:"label"`      // 阶段名，如「下载网站文件」
	Name      string  `json:"name"`       // 当前对象名（网站名/库名）
	Done      int64   `json:"done"`       // 已完成字节数
	Total     int64   `json:"total"`      // 总字节数（0 表示源端未提供长度，前端按不确定模式显示）
	Percent   float64 `json:"percent"`    // 0-100（Total 为 0 时恒为 0）
	Speed     int64   `json:"speed"`      // 瞬时速度（字节/秒）
	SpeedText string  `json:"speed_text"` // 速度可读文本，如「1.2 MB/s」
	Detail    string  `json:"detail"`     // 一行完整描述，供日志/提示直接展示
}

// ImportTask 迁移任务状态
type ImportTask struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`              // export / import，见 TaskKind*
	Status    string        `json:"status"`            // running / success / failed / canceled
	Logs      []string      `json:"logs"`
	Error     string        `json:"error"`
	Items     []ItemResult  `json:"items"`             // 各子任务处理结果（建站/建库/伪静态/数据导入等）
	Progress  *TaskProgress `json:"progress,omitempty"` // 当前步骤实时进度（无进度时为 nil）
	UpdatedAt time.Time     `json:"updated_at"`
	mu        sync.Mutex
	ctx       context.Context    // 取消信号：用户点「取消迁移」后 Done 关闭
	cancel    context.CancelFunc // 中断下载等阻塞操作
	// 速度计算用：上次采样时的已完成字节数与时刻
	lastDone int64
	lastAt   time.Time
	// 上次往任务日志写下载进度的时刻（每 3 秒输出一条，替代前端进度条）
	lastProgLog time.Time
}

// addItem 记录一个子任务的结果（线程安全）
func (t *ImportTask) addItem(typ, name, status, msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Items = append(t.Items, ItemResult{Type: typ, Name: name, Status: status, Message: msg})
	t.UpdatedAt = time.Now()
}

// hasFailedItemsLocked 判断是否有失败项（调用方需持有 t.mu）
func (t *ImportTask) hasFailedItemsLocked() bool {
	for _, it := range t.Items {
		if it.Status == "failed" {
			return true
		}
	}
	return false
}

var (
	importTasks   = map[string]*ImportTask{}
	importTasksMu sync.Mutex
)

func newImportTask(id, kind string) *ImportTask {
	ctx, cancel := context.WithCancel(context.Background())
	t := &ImportTask{ID: id, Kind: kind, Status: "running", Logs: []string{}, UpdatedAt: time.Now(), ctx: ctx, cancel: cancel}
	importTasksMu.Lock()
	importTasks[id] = t
	importTasksMu.Unlock()
	return t
}

// canceled 任务是否已被用户取消。耗时的循环/下载应在合适位置调用它及时退出。
func (t *ImportTask) canceled() bool {
	if t.ctx == nil {
		return false
	}
	select {
	case <-t.ctx.Done():
		return true
	default:
		return false
	}
}

// Ctx 返回任务取消上下文，供下载等阻塞操作在取消时立刻中断
func (t *ImportTask) Ctx() context.Context {
	if t.ctx == nil {
		return context.Background()
	}
	return t.ctx
}

// setProgress 更新当前步骤的实时进度（下载迁移包/网站文件、导入数据库等）。
// 速度按相邻两次采样的增量计算；采样间隔不足 0.5s 时沿用上次速度，避免数值剧烈抖动。
func (t *ImportTask) setProgress(phase, label, name string, done, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	var speed int64
	switch {
	case t.lastAt.IsZero():
		t.lastDone, t.lastAt = done, now
	case now.Sub(t.lastAt).Seconds() >= 0.5:
		elapsed := now.Sub(t.lastAt).Seconds()
		if delta := done - t.lastDone; delta > 0 {
			speed = int64(float64(delta) / elapsed)
		}
		t.lastDone, t.lastAt = done, now
	case t.Progress != nil:
		speed = t.Progress.Speed
	}
	p := &TaskProgress{Phase: phase, Label: label, Name: name, Done: done, Total: total, Speed: speed}
	switch {
	case total > 0:
		p.Percent = float64(done) / float64(total) * 100
		if p.Percent > 100 {
			p.Percent = 100
		}
		p.Detail = fmt.Sprintf("%s %s：%s / %s（%.1f%%）%s", label, name, FormatBytes(done), FormatBytes(total), p.Percent, FormatBytes(speed)+"/s")
	case done > 0:
		p.Detail = fmt.Sprintf("%s %s：已传输 %s（%s）", label, name, FormatBytes(done), FormatBytes(speed)+"/s")
	default:
		p.Detail = fmt.Sprintf("%s %s 进行中...", label, name)
	}
	p.SpeedText = FormatBytes(speed) + "/s"
	t.Progress = p
	t.UpdatedAt = now
	// 下载进度每 3 秒打一条进任务日志（与日志框同屏展示，替代前端进度条）；
	// 仅在确有字节流动时输出，避免导入数据库等无进度阶段刷屏
	if done > 0 && now.Sub(t.lastProgLog) >= 3*time.Second {
		t.lastProgLog = now
		t.Logs = append(t.Logs, "["+now.Format("15:04:05")+"] "+p.Detail)
	}
}

// clearProgress 步骤结束后清除进度，避免前端继续展示上一个步骤的进度条
func (t *ImportTask) clearProgress() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Progress = nil
	t.lastDone, t.lastAt = 0, time.Time{}
	t.UpdatedAt = time.Now()
}

// CancelImportTask 取消进行中的迁移任务（前端「取消迁移」按钮）。
// 只置状态并触发 ctx 取消，真正的中断由下载/循环检查 ctx 完成，从而能立刻停止传输。
func CancelImportTask(id string) error {
	importTasksMu.Lock()
	t, ok := importTasks[id]
	importTasksMu.Unlock()
	if !ok {
		return errors.New("任务不存在")
	}
	t.mu.Lock()
	if t.Status != "running" {
		t.mu.Unlock()
		return errors.New("任务已结束，无需取消")
	}
	t.Status = "canceled"
	t.Error = "已取消"
	t.Progress = nil
	t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	t.logf("迁移已被用户取消")
	return nil
}

// progressWriter 统计已写入字节并按固定间隔回调进度，
// 避免 io.Copy 每 32KB 一块就回调一次导致频繁加锁与进度刷屏。
type progressWriter struct {
	w          io.Writer
	done       int64
	total      int64
	lastReport time.Time
	onProgress func(done, total int64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.done += int64(n)
	if pw.onProgress != nil && (time.Since(pw.lastReport) >= 500*time.Millisecond || err != nil) {
		pw.lastReport = time.Now()
		pw.onProgress(pw.done, pw.total)
	}
	return n, err
}

// downloadWithProgress 带进度回调与取消支持的断点续传下载。
//   - ctx 取消时立即中断传输并返回 ctx.Err()，用于「取消迁移」立刻停手；
//   - onProgress(done, total) 约每 500ms 回调一次；total 为 0 表示源端未提供总长度；
//   - 传输意外中断自动重试，已下载部分通过 Range 续传，不重复拉取。
func downloadWithProgress(ctx context.Context, rawURL, dest string, onProgress func(done, total int64)) error {
	client := &http.Client{
		Timeout:   0,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	var total int64
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return err
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempts < 3 {
				attempts++
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("下载失败: %w", err)
		}
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			resp.Body.Close()
			return nil // 已完整下载
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
		}
		// 总大小：续传响应用 Content-Range 末尾的 total，完整响应用 Content-Length
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if idx := strings.LastIndex(cr, "/"); idx >= 0 {
				fmt.Sscanf(cr[idx+1:], "%d", &total)
			}
		} else if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
		if total == 0 && resp.ContentLength > 0 {
			total = offset + resp.ContentLength
		}
		cur, _ := f.Seek(0, io.SeekEnd)
		offset = cur
		pw := &progressWriter{w: f, done: offset, total: total, lastReport: time.Now(), onProgress: onProgress}
		n, copyErr := io.Copy(pw, resp.Body)
		resp.Body.Close()
		offset += n
		if pw.onProgress != nil {
			pw.onProgress(offset, total)
		}
		if copyErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempts < 3 {
				attempts++
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("下载失败: %w", copyErr)
		}
		return nil
	}
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
	var prog *TaskProgress
	if t.Progress != nil {
		cp := *t.Progress
		prog = &cp
	}
	return &ImportTask{
		ID:        t.ID,
		Kind:      t.Kind,
		Status:    t.Status,
		Logs:      append([]string{}, t.Logs...),
		Error:     t.Error,
		Items:     append([]ItemResult{}, t.Items...),
		Progress:  prog,
		UpdatedAt: t.UpdatedAt,
	}, nil
}

// ListImportTasks 列出全部迁移任务（按最近更新倒序）。
// 前端重新打开搬家工具时用它找出「仍在进行中」的任务，直接跳回进度页继续查看，
// 而不是每次都从第 1 步重新开始。
func ListImportTasks() []*ImportTask {
	importTasksMu.Lock()
	defer importTasksMu.Unlock()
	out := make([]*ImportTask, 0, len(importTasks))
	for _, t := range importTasks {
		t.mu.Lock()
		var prog *TaskProgress
		if t.Progress != nil {
			cp := *t.Progress
			prog = &cp
		}
		out = append(out, &ImportTask{
			ID:        t.ID,
			Kind:      t.Kind,
			Status:    t.Status,
			Logs:      append([]string{}, t.Logs...),
			Error:     t.Error,
			Progress:  prog,
			UpdatedAt: t.UpdatedAt,
		})
		t.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
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
	task := newImportTask(id, TaskKindImport)
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

		// 3. 下载迁移包（带实时进度，取消时立即中断）
		local, err := remote.Download(t.Ctx(), pkgID, func(done, total int64) {
			t.setProgress("downloading_package", "下载迁移包", "", done, total)
		})
		t.clearProgress()
		if err != nil {
			if t.canceled() {
				return
			}
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
	} else if len(CompareEnvForMigration(manifest, MigrateEnvStatus())) > 0 {
		t.logf("注意：仍有缺失环境未勾选自动安装，迁移可能失败")
	}

	// 6. 解压迁移包
	if t.canceled() {
		return
	}
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
		if t.canceled() {
			return
		}
		if !containsStr(req.Sites, s.Name) {
			continue
		}
		// 目标已存在处理：覆盖=删旧重建，不覆盖=跳过该网站（不影响其他项）
		var oldSite model.Site
		if model.DB.Where("name = ?", s.Name).First(&oldSite).Error == nil {
			if !req.Overwrite {
				t.logf("网站 %s 已存在于目标面板，按选择跳过", s.Name)
				t.addItem("site", s.Name, "skipped", "目标已存在，未勾选覆盖")
				continue
			}
			t.logf("网站 %s 已存在，按覆盖删除旧站点后重建...", s.Name)
			if _, err := DeleteSite(DeleteSiteReq{ID: oldSite.ID}); err != nil {
				t.logf("删除旧网站 %s 失败: %v", s.Name, err)
				t.addItem("site", s.Name, "failed", "删除旧网站失败："+err.Error())
				continue
			}
		}
		t.logf("恢复网站 %s（%s）...", s.Name, s.Type)
		if err := restoreSite(t, workDir, &s, req); err != nil {
			if t.canceled() {
				return
			}
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
		if t.canceled() {
			return
		}
		if !containsStr(req.Databases, db.Name) {
			continue
		}
		// 目标已存在处理：覆盖=删旧库重建，不覆盖=跳过导入（不影响其他项）
		dbExists := false
		if dbs, err := ListDatabases(); err == nil {
			for _, d := range dbs {
				if d.Name == db.Name {
					dbExists = true
					break
				}
			}
		}
		if dbExists {
			if !req.Overwrite {
				t.logf("数据库 %s 已存在，按选择跳过", db.Name)
				t.addItem("database", db.Name, "skipped", "目标已存在，未勾选覆盖")
				continue
			}
			t.logf("数据库 %s 已存在，按覆盖删除旧库后重建...", db.Name)
			if err := DeleteDatabase(db.Name); err != nil {
				t.logf("删除旧数据库 %s 失败: %v", db.Name, err)
				t.addItem("database", db.Name, "failed", "删除旧库失败："+err.Error())
				continue
			}
		}
		t.logf("恢复数据库 %s ...", db.Name)
		if err := restoreDatabase(t, workDir, &db, req.NewDBPassword); err != nil {
			if t.canceled() {
				return
			}
			t.fail("数据库 " + db.Name + " 恢复失败: " + err.Error())
			return
		}
		t.logf("数据库 %s 恢复完成", db.Name)
	}

	// 9. 恢复 FTP
	for _, f := range manifest.FTPs {
		if t.canceled() {
			return
		}
		if !containsStr(req.FTPs, f.Username) {
			continue
		}
		// 目标已存在处理：覆盖=删旧账号重建，不覆盖=跳过（不影响其他项）
		var oldFTP model.FtpUser
		if model.DB.Where("username = ?", f.Username).First(&oldFTP).Error == nil {
			if !req.Overwrite {
				t.logf("FTP 账号 %s 已存在，按选择跳过", f.Username)
				t.addItem("ftp", f.Username, "skipped", "目标已存在，未勾选覆盖")
				continue
			}
			t.logf("FTP 账号 %s 已存在，按覆盖删除后重建...", f.Username)
			if err := DeleteFtpUser(oldFTP.ID); err != nil {
				t.logf("删除旧 FTP 账号 %s 失败: %v", f.Username, err)
				t.addItem("ftp", f.Username, "failed", "删除旧FTP失败："+err.Error())
				continue
			}
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
			if t.canceled() {
				return
			}
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
	// 注：同名网站的「覆盖删除 / 跳过」已在 runImport 循环里统一处理，
	// 此处仅负责把网站重建出来（旧站若需覆盖，外层已先删除）。

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
		Rewrite:        ms.Rewrite,
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
	PanelType  string   `json:"panel_type"`  // 源面板类型：kypanel
	PanelURL   string   `json:"panel_url"`
	PanelToken string   `json:"panel_token"`
	Sites      []string `json:"sites"`      // 选中的网站
	Databases  []string `json:"databases"`  // 选中的数据库（用于同名检测）
	FTPs       []string `json:"ftps"`       // 选中的 FTP（用于同名检测）
}

// ImportPlanResult 迁入计划结果
type ImportPlanResult struct {
	PanelVersion   string           `json:"panel_version"`
	Env            MigrateEnvInfo   `json:"env"`
	Sites          []map[string]any `json:"sites"`
	Missing        []MissingEnv     `json:"missing"`
	ExistingSites  []string         `json:"existing_sites"`  // 本机已存在的同名网站（与选中网站对比）
	ExistingDBs    []string         `json:"existing_dbs"`    // 本机已存在的同名数据库
	ExistingFTPs   []string         `json:"existing_ftps"`   // 本机已存在的同名 FTP 账号
	DBsCount       int              `json:"dbs_count"`
	FTPsCount      int              `json:"ftps_count"`
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
		res, err := BTEnvCompare(req.PanelURL, req.PanelToken, req.Sites, req.Databases)
		if err != nil {
			return nil, err
		}
		return map[string]any{"target": "bt", "result": res}, nil
	default:
		return nil, errors.New("不支持的目标面板类型")
	}
}

// FetchRemoteSites 拉取源面板的网站/数据库/FTP 全量列表（不对比环境）
// 返回结构：{sites: [], databases: [], ftps: []}，供前端三列多选使用
func FetchRemoteSites(req ImportPlanRequest) (map[string]any, error) {
	if req.PanelURL == "" || req.PanelToken == "" {
		return nil, errors.New("请填写源面板地址和 API 密钥")
	}
	switch req.PanelType {
	case "kypanel":
		remote := NewRemotePanel(req.PanelURL, req.PanelToken)
		if err := remote.Ping(); err != nil {
			return nil, err
		}
		sites, err := remote.GetSites()
		if err != nil {
			return nil, err
		}
		// kypanel 源面板：databases/ftps 没有独立接口，按 sites.dbs/sites.ftps 聚合去重
		dbSet := map[string]struct{}{}
		ftpSet := map[string]struct{}{}
		for _, s := range sites {
			for _, d := range toStringSliceAny(s["dbs"]) {
				dbSet[d] = struct{}{}
			}
			for _, f := range toStringSliceAny(s["ftps"]) {
				ftpSet[f] = struct{}{}
			}
		}
		var dbsList, ftpsList []map[string]any
		for d := range dbSet {
			dbsList = append(dbsList, map[string]any{"name": d, "type": "mysql"})
		}
		for f := range ftpSet {
			ftpsList = append(ftpsList, map[string]any{"username": f})
		}
		return map[string]any{"sites": sites, "databases": dbsList, "ftps": ftpsList}, nil
	case "bt":
		client := NewBTClient(req.PanelURL, req.PanelToken)
		sites, err := client.SiteList()
		if err != nil {
			return nil, fmt.Errorf("获取对端面板网站列表失败: %w", err)
		}
		dbs, _ := client.DatabaseList()
		ftps, _ := client.FtpUserList()

		// 整理 sites（含每个站点的 dbs/ftps 关联）
		type btSiteView struct {
			m           map[string]any
			root, name  string
		}
		var siteViews []btSiteView
		var siteOuts []map[string]any
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
			siteOuts = append(siteOuts, map[string]any{
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
			siteViews = append(siteViews, btSiteView{m: s, root: root, name: name})
		}

		// 整理全量 databases 列表（去重）
		dbSeen := map[string]bool{}
		var dbOut []map[string]any
		for _, d := range dbs {
			n := toStr(d["name"])
			if n == "" || dbSeen[n] {
				continue
			}
			dbSeen[n] = true
			dbOut = append(dbOut, map[string]any{
				"name":     n,
				"type":     "mysql",
				"username": toStr(d["db_user"]),
			})
		}
		// 整理全量 ftps 列表（去重）
		ftpSeen := map[string]bool{}
		var ftpOut []map[string]any
		for _, f := range ftps {
			n := toStr(f["username"])
			if n == "" || ftpSeen[n] {
				continue
			}
			ftpSeen[n] = true
			ftpOut = append(ftpOut, map[string]any{
				"username": n,
				"path":     toStr(f["path"]),
			})
		}
		_ = siteViews // 占位防止 unused
		return map[string]any{"sites": siteOuts, "databases": dbOut, "ftps": ftpOut}, nil
	default:
		return nil, errors.New("不支持的面板类型，请先连接并识别面板")
	}
}

// toStringSliceAny 把 any 转 []string（用于 sites 中 dbs/ftps 字段聚合）
func toStringSliceAny(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			out = append(out, fmt.Sprint(it))
		}
		return out
	}
	return nil
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
	}
	// 环境对比只针对「用户实际勾选」的数据库/FTP，不把网站关联项算进去
	// 否则用户只选了 PHP 网站、没勾 FTP 时也会被提示「缺少 FTP 服务」
	for _, n := range req.Databases {
		manifest.Databases = append(manifest.Databases, MigrateDB{Name: n, User: n})
	}
	for _, n := range req.FTPs {
		manifest.FTPs = append(manifest.FTPs, MigrateFTP{Username: n})
	}
	// 只有勾选了网站却一个都没匹配上才报错；允许只迁移数据库/FTP 而不迁网站
	if len(req.Sites) > 0 && len(picked) == 0 {
		return nil, errors.New("源面板上未找到选中的网站")
	}

	env := MigrateEnvStatus()
	missing := CompareEnvForMigration(&manifest, env)
	exSites, exDBs, exFTPs := resolveImportConflicts(req.Sites, req.Databases, req.FTPs)
	return &ImportPlanResult{
		PanelVersion:  remote.Version,
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

// resolveImportConflicts 与本机现有同名项目做对比，返回已存在的子集（保持顺序）
func resolveImportConflicts(sites, dbs, ftps []string) (exSites, exDBs, exFTPs []string) {
	for _, n := range sites {
		if _, ok := model.GetSiteByName(n); ok {
			exSites = append(exSites, n)
		}
	}
	if len(dbs) > 0 {
		var rows []model.DatabaseAccount
		model.DB.Where("db_name IN ?", dbs).Find(&rows)
		for _, n := range dbs {
			for _, r := range rows {
				if r.DbName == n {
					exDBs = append(exDBs, n)
					break
				}
			}
		}
	}
	if len(ftps) > 0 {
		var rows []model.FtpUser
		model.DB.Where("username IN ?", ftps).Find(&rows)
		for _, n := range ftps {
			for _, r := range rows {
				if r.Username == n {
					exFTPs = append(exFTPs, n)
					break
				}
			}
		}
	}
	return
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
