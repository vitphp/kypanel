package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============================ 全局默认页面 ============================
//
// {DataDir}/default_pages/ 下维护 4 个全局页面：
//
//	index.html  全局默认首页模板 —— 新建站点时自动复制到站点根目录（仅影响新建站点）
//	404.html    全局默认 404 模板 —— 新建站点时自动复制到站点根目录（仅影响新建站点）
//	stop.html   站点被「停止」后，访问该站点域名时显示的页面
//	nosite.html 访问未绑定域名、且未设置默认站点时，default_server 显示的页面
//
// 另配合「默认站点」：任意站点可设为默认，设了之后未绑定域名直接访问该默认站点
// （其 server 块 listen 增加 default_server 标记），不设则走 nosite.html 兜底块。

const (
	DefaultPageIndex  = "index.html"
	DefaultPage404    = "404.html"
	DefaultPageStop   = "stop.html"
	DefaultPageNosite = "nosite.html"

	// Setting 表键：当前默认站点 ID（字符串；空 = 未设置）
	settingDefaultSite = "default_site_id"
)

// defaultPageKinds 全部默认页面文件名（用于初始化/校验）
var defaultPageKinds = []string{DefaultPageIndex, DefaultPage404, DefaultPageStop, DefaultPageNosite}

// DefaultPagesDir 全局默认页面目录
func DefaultPagesDir() string {
	return filepath.Join(config.Get().DataDir, "default_pages")
}

func defaultPagePath(kind string) string {
	return filepath.Join(DefaultPagesDir(), kind)
}

// InitDefaultPages 启动时初始化：确保 4 个全局默认页面存在，并应用 default_server 兜底配置
func InitDefaultPages() error {
	if err := EnsureDefaultPages(); err != nil {
		return err
	}
	return applyDefaultServerConf()
}

// EnsureDefaultPages 确保 4 个全局默认页面存在（缺失时写入内置模板）
func EnsureDefaultPages() error {
	dir := DefaultPagesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, kind := range defaultPageKinds {
		path := defaultPagePath(kind)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if tpl, ok := defaultPageTemplates[kind]; ok {
				if err := os.WriteFile(path, []byte(tpl), 0o644); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// DefaultPagesInfo 默认页面设置（含当前默认站点）
type DefaultPagesInfo struct {
	Index          string `json:"index"`
	Page404        string `json:"page404"`
	Stop           string `json:"stop"`
	Nosite         string `json:"nosite"`
	DefaultSiteID  uint   `json:"default_site_id"`
	DefaultSiteName string `json:"default_site_name"`
}

// GetDefaultPages 返回 4 个页面内容 + 当前默认站点信息
func GetDefaultPages() DefaultPagesInfo {
	info := DefaultPagesInfo{}
	read := func(kind string) string {
		if b, err := os.ReadFile(defaultPagePath(kind)); err == nil {
			return string(b)
		}
		return defaultPageTemplates[kind]
	}
	info.Index = read(DefaultPageIndex)
	info.Page404 = read(DefaultPage404)
	info.Stop = read(DefaultPageStop)
	info.Nosite = read(DefaultPageNosite)
	info.DefaultSiteID = DefaultSiteID()
	if info.DefaultSiteID != 0 {
		if s, err := getSiteOrErr(info.DefaultSiteID); err == nil {
			info.DefaultSiteName = s.Name
		}
	}
	return info
}

// SaveDefaultPage 保存指定全局默认页面
func SaveDefaultPage(kind, content string) error {
	switch kind {
	case DefaultPageIndex, DefaultPage404, DefaultPageStop, DefaultPageNosite:
	default:
		return fmt.Errorf("未知的页面类型: %s", kind)
	}
	if err := os.MkdirAll(DefaultPagesDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(defaultPagePath(kind), []byte(content), 0o644)
}

// DefaultSiteID 当前默认站点 ID（0 = 未设置）
func DefaultSiteID() uint {
	v := strings.TrimSpace(model.GetSetting(settingDefaultSite))
	id, _ := strconv.ParseUint(v, 10, 32)
	return uint(id)
}

// SetDefaultSite 设置/切换默认站点（id=0 时清除）。
// 除更新 setting 与 default_server 兜底块外，还必须重新生成受影响站点的
// nginx 配置（默认站点 listen 加 default_server 标记、原默认站点移除标记），
// 否则 server 块不会带/去 default_server，未绑定域名仍落到旧的兜底块。
func SetDefaultSite(id uint) error {
	if id != 0 {
		if _, err := getSiteOrErr(id); err != nil {
			return err
		}
	}
	oldID := DefaultSiteID()
	if err := model.SetSetting(settingDefaultSite, strconv.FormatUint(uint64(id), 10)); err != nil {
		return err
	}
	// 先按目标状态落盘 default_server 兜底文件（不校验，避免与站点标记互相干扰）
	path := filepath.Join(nginxConfDir, "lp_default.conf")
	if id != 0 {
		_ = os.Remove(path)
	} else {
		if err := os.MkdirAll(nginxConfDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(genDefaultServerConf()), 0o644); err != nil {
			return err
		}
	}
	// 重新生成受影响站点的配置（内部含 nginx -t 校验与失败回滚）
	affected := map[uint]bool{}
	if oldID != 0 {
		affected[oldID] = true
	}
	if id != 0 {
		affected[id] = true
	}
	for sid := range affected {
		var s model.Site
		if err := model.DB.First(&s, sid).Error; err != nil {
			continue
		}
		if err := writeSiteConf(&s); err != nil {
			return err
		}
	}
	// 统一校验（若与系统自带 default_server 冲突则中和后重试）并热加载
	if err := webConfigTest(); err != nil {
		neutralizeSystemDefaultServer()
		if err2 := webConfigTest(); err2 != nil {
			return err2
		}
	}
	return webReload()
}

// applyDefaultServerConf 重新生成 default_server 兜底配置并热加载（nginx）。
//   - 已设置默认站点：删除兜底块（default_server 由默认站点的 listen 承担）
//   - 未设置默认站点：写入 lp_default.conf，未绑定域名显示 nosite.html
//   - Apache：不适用，直接跳过
func applyDefaultServerConf() error {
	if WebServerType() == webApache {
		return nil
	}
	if !nginxInstalledBin() {
		return nil
	}
	// 中和系统自带的 default_server（Debian/Ubuntu 的 sites-enabled/default、
	// 部分发行版的 conf.d/default.conf），避免与面板 default_server 冲突导致校验失败
	neutralizeSystemDefaultServer()

	path := filepath.Join(nginxConfDir, "lp_default.conf")
	if DefaultSiteID() != 0 {
		_ = os.Remove(path)
	} else {
		if err := os.MkdirAll(nginxConfDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(genDefaultServerConf()), 0o644); err != nil {
			return err
		}
	}
	if err := webConfigTest(); err != nil {
		// 校验失败且与 default_server 冲突相关时，中和后再试一次
		neutralizeSystemDefaultServer()
		if err2 := webConfigTest(); err2 != nil {
			return err2
		}
	}
	return webReload()
}

// neutralizeSystemDefaultServer 禁用系统自带的 default_server 站点配置。
// 这些文件自带的 `listen ... default_server` 会与面板写入/改写的 default_server
// 冲突，导致 nginx -t 报 duplicate default server、整站 reload 失败。
//   - /etc/nginx/sites-enabled/ 按 `*` 通配 include：重命名仍会被加载，
//     因此必须删除（该目录下的条目通常是软链，实体仍保留在 sites-available/ 可恢复）
//   - /etc/nginx/conf.d/default.conf 按 `*.conf` 通配 include：重命名后缀即可禁用
// 仅处理名为 default 的系统默认文件（含上一轮残留的 *.kypanel-disabled），
// 不触碰其他用户站点配置。
func neutralizeSystemDefaultServer() {
	entries, err := os.ReadDir("/etc/nginx/sites-enabled")
	if err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "default") {
				continue
			}
			p := filepath.Join("/etc/nginx/sites-enabled", e.Name())
			if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "default_server") {
				_ = os.Remove(p)
			}
		}
	}
	for _, p := range []string{"/etc/nginx/conf.d/default.conf"} {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "default_server") {
			_ = os.Rename(p, p+".kypanel-disabled")
		}
	}
}

// genDefaultServerConf 生成未绑定域名的兜底 server 块（未设置默认站点时）
func genDefaultServerConf() string {
	var sb strings.Builder
	sb.WriteString("# kypanel default server: 未绑定域名兜底（未设置默认站点时生效）\n")
	sb.WriteString("server {\n")
	sb.WriteString("    listen 80 default_server;\n")
	sb.WriteString("    server_name _;\n")
	fmt.Fprintf(&sb, "    root %s;\n", DefaultPagesDir())
	sb.WriteString("    index nosite.html;\n")
	sb.WriteString("    error_page 404 /404.html;\n")
	sb.WriteString("    location / {\n")
	sb.WriteString("        try_files $uri $uri/ =404;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String()
}

// defaultPageTemplates 内置默认页面模板（首次初始化写入，之后以面板编辑内容为准）
var defaultPageTemplates = map[string]string{
	DefaultPageIndex: `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>网站创建成功</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#667eea,#764ba2);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
h1{font-size:42px;margin-bottom:12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<h1>网站创建成功</h1>
<p>本页面由 kypanel 自动生成，请在文件管理器中替换为您自己的首页内容。</p>
</div>
</body>
</html>
`,
	DefaultPage404: `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>页面不存在</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#2d3748,#4a5568);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
.code{font-size:72px;font-weight:700;margin-bottom:8px;opacity:.9}
h1{font-size:28px;margin:0 0 12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<div class="code">404</div>
<h1>页面不存在</h1>
<p>您访问的页面不存在或已被移动，请检查地址后重试。</p>
</div>
</body>
</html>
`,
	DefaultPageStop: `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>网站已暂停</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#b45309,#92400e);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
.icon{font-size:64px;margin-bottom:8px}
h1{font-size:28px;margin:0 0 12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<div class="icon">&#9888;</div>
<h1>网站已暂停服务</h1>
<p>该网站已被管理员停止，请稍后再访问。</p>
</div>
</body>
</html>
`,
	DefaultPageNosite: `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>网站不存在</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#334155,#1e293b);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
.code{font-size:72px;font-weight:700;margin-bottom:8px;opacity:.9}
h1{font-size:28px;margin:0 0 12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<div class="code">403</div>
<h1>网站不存在</h1>
<p>您访问的域名未绑定任何网站，请联系管理员。</p>
</div>
</body>
</html>
`,
}
