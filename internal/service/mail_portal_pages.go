package service

import (
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"kypanel/internal/model"
)

// mailPortalLogoRe 匹配上传 Logo 的引用地址（/api/mail-portal/logo/<domainID>?...）
var mailPortalLogoRe = regexp.MustCompile(`^/api/mail-portal/logo/(\d+)`)

// materializePortalLogo 把上传到面板数据目录的 Logo 复制到门户站点根目录，
// 并返回站点内可直接静态访问的路径（/logo.xxx）；外链或自定义路径原样返回。
func materializePortalLogo(site *model.Site, logo string) string {
	logo = strings.TrimSpace(logo)
	m := mailPortalLogoRe.FindStringSubmatch(logo)
	if m == nil {
		return logo
	}
	id, err := strconv.ParseUint(m[1], 10, 32)
	if err != nil || id == 0 {
		return ""
	}
	dir := mailPortalLogoDir()
	prefix := strconv.FormatUint(id, 10) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		dstName := "logo" + strings.ToLower(filepath.Ext(e.Name()))
		if err := os.WriteFile(filepath.Join(site.Root, dstName), data, 0o644); err != nil {
			return ""
		}
		return "/" + dstName
	}
	// 上传记录已丢失：不输出无效地址，交给页面用首字母徽标兜底
	return ""
}

// writeMailPortalFiles 在门户站点根目录生成 index.html（官网）与 webmail.html（自包含 webmail）。
func writeMailPortalFiles(dom *model.MailDomain, site *model.Site, portalDomain string, extras []string,
	title, name, logo, footer string, register bool) error {
	if err := os.MkdirAll(site.Root, 0o755); err != nil {
		return err
	}
	// 上传的 Logo 同步复制到站点根目录，页面改用站点内静态路径引用，
	// 不依赖「/api/mail-portal」反代（Apache 等场景也能正常显示）。
	logo = materializePortalLogo(site, logo)
	indexHTML := renderMailPortalIndex(portalDomain, dom.Domain, title, name, logo, footer, register)
	if err := os.WriteFile(filepath.Join(site.Root, "index.html"), []byte(indexHTML), 0o644); err != nil {
		return err
	}
	webmailHTML := renderMailPortalWebmail(title, name, logo, footer, register)
	if err := os.WriteFile(filepath.Join(site.Root, "webmail.html"), []byte(webmailHTML), 0o644); err != nil {
		return err
	}
	_ = ChownToWebUser(site.Root, true)
	return nil
}

// portalBadgeLetter 取站点名首字作为默认徽标文字。
func portalBadgeLetter(name string) string {
	if r := []rune(strings.TrimSpace(name)); len(r) > 0 {
		return string(r[0])
	}
	return "邮"
}

// portalLogoFallback 生成首字母渐变徽标的内联 SVG（data URI）：
// 既用作默认的浏览器标签页图标，也用作 Logo 图片加载失败时的兜底，
// 避免出现浏览器默认的「破图」图标。
func portalLogoFallback(name string) string {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">` +
		`<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">` +
		`<stop offset="0" stop-color="#6366f1"/><stop offset="1" stop-color="#8b5cf6"/>` +
		`</linearGradient></defs>` +
		`<rect width="64" height="64" rx="14" fill="url(#g)"/>` +
		`<text x="32" y="44" font-size="34" font-weight="700" fill="#ffffff" text-anchor="middle" font-family="Arial,Helvetica,sans-serif">` +
		html.EscapeString(portalBadgeLetter(name)) + `</text></svg>`
	return "data:image/svg+xml," + strings.ReplaceAll(url.QueryEscape(svg), "+", "%20")
}

// portalFavicon 浏览器标签页图标：已上传 Logo 用 Logo，否则用首字母徽标。
func portalFavicon(logo, name string) string {
	logo = strings.TrimSpace(logo)
	if logo != "" {
		return html.EscapeString(logo)
	}
	return portalLogoFallback(name)
}

// portalLogoHTML 生成 Logo 展示片段：有图用图（加载失败自动兜底为首字母徽标），无图用首字母徽标。
func portalLogoHTML(logo, name, cls string) string {
	logo = strings.TrimSpace(logo)
	if logo != "" {
		return `<img class="` + cls + `" src="` + html.EscapeString(logo) +
			`" alt="logo" onerror="this.onerror=null;this.src='` + portalLogoFallback(name) + `'">`
	}
	return `<span class="` + cls + ` logo-letter">` + html.EscapeString(portalBadgeLetter(name)) + `</span>`
}

// renderMailPortalIndex 生成官网首页。
func renderMailPortalIndex(portalDomain, mailDomain, title, name, logo, footer string, register bool) string {
	regBtn := ""
	if register {
		regBtn = `<a href="/webmail.html#register" class="btn primary">免费注册</a>`
	}
	logoHTML := portalLogoHTML(logo, name, "logo")
	repl := strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__NAME__", html.EscapeString(name),
		"__DOMAIN__", html.EscapeString(mailDomain),
		"__FOOTER__", html.EscapeString(footer),
		"__LOGO__", logoHTML,
		"__ICON__", portalFavicon(logo, name),
		"__REG__", regBtn,
	)
	return repl.Replace(mailPortalIndexTpl)
}

// renderMailPortalWebmail 生成 webmail 单页。
func renderMailPortalWebmail(title, name, logo, footer string, register bool) string {
	logoHTML := portalLogoHTML(logo, name, "logo-sm")
	// 邮箱界面底部固定版权条（未配置版权时整块不渲染）
	appFoot := ""
	if f := strings.TrimSpace(footer); f != "" {
		appFoot = `<div class="app-foot">` + html.EscapeString(f) + `</div>`
	}
	repl := strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__NAME__", html.EscapeString(name),
		"__FOOTER__", html.EscapeString(footer),
		"__APPFOOT__", appFoot,
		"__LOGO__", logoHTML,
		"__ICON__", portalFavicon(logo, name),
		"__REGISTER__", boolJS(register),
	)
	return repl.Replace(mailPortalWebmailTpl)
}

func boolJS(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

const mailPortalSharedCSS = `
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;color:#1f2937;background:#f7f8fc}
a{color:inherit;text-decoration:none}
.logo{height:34px;width:auto;border-radius:8px}
.logo-letter{display:inline-flex;align-items:center;justify-content:center;height:34px;width:34px;border-radius:9px;background:linear-gradient(135deg,#6366f1,#8b5cf6);color:#fff;font-weight:700;font-size:18px}
.logo-sm{height:26px;width:auto;border-radius:7px}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:9px 18px;border-radius:9px;font-size:14px;font-weight:600;cursor:pointer;border:1px solid transparent;transition:.18s}
.btn.primary{background:linear-gradient(135deg,#6366f1,#8b5cf6);color:#fff}
.btn.primary:hover{filter:brightness(1.07);box-shadow:0 6px 18px rgba(99,102,241,.35)}
.btn.ghost{background:#fff;border-color:#e5e7eb;color:#374151}
.btn.ghost:hover{border-color:#c7cbd4}
.btn.lg{padding:13px 30px;font-size:16px}
.btn[disabled]{opacity:.55;cursor:not-allowed}
`

// ---- 官网首页 ----
const mailPortalIndexTpl = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>__TITLE__</title>
<link rel="icon" href="__ICON__">
<style>` + mailPortalSharedCSS + `
body{background:#fff;color:#0f172a;overflow-x:hidden}
.nav{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px 32px;background:rgba(255,255,255,.78);backdrop-filter:blur(14px);position:sticky;top:0;z-index:20;border-bottom:1px solid #eef0f5}
.brand{display:flex;align-items:center;gap:10px;font-weight:700;font-size:17px;white-space:nowrap}
.nav nav{display:flex;align-items:center;gap:10px}
.navlink{font-size:14px;color:#4b5563;padding:8px 10px;border-radius:8px;font-weight:500}
.navlink:hover{color:#4f46e5;background:#f4f4ff}
.hero{position:relative;max-width:1120px;margin:0 auto;padding:76px 24px 0;text-align:center}
.hero:before{content:"";position:absolute;top:-200px;left:50%;transform:translateX(-50%);width:900px;height:560px;background:radial-gradient(closest-side,rgba(99,102,241,.20),rgba(139,92,246,.10) 60%,transparent);z-index:-1;pointer-events:none}
.pill{display:inline-flex;align-items:center;gap:7px;font-size:13px;font-weight:600;color:#4f46e5;background:#eef0ff;border:1px solid #e0e3ff;border-radius:999px;padding:6px 14px;margin-bottom:22px}
.pill i{width:6px;height:6px;border-radius:50%;background:#4f46e5;display:inline-block}
.hero h1{font-size:46px;line-height:1.22;letter-spacing:-.8px;font-weight:800;color:#0f172a;margin-bottom:18px}
.hero h1 em{font-style:normal;background:linear-gradient(120deg,#4f46e5,#9333ea 60%,#db2777);-webkit-background-clip:text;background-clip:text;color:transparent}
.hero .sub{font-size:17px;line-height:1.8;color:#64748b;max-width:620px;margin:0 auto 30px}
.cta{display:flex;gap:14px;justify-content:center;flex-wrap:wrap}
.note{font-size:13px;color:#94a3b8;margin-top:16px}
.mock{max-width:880px;margin:56px auto 0;background:#fff;border:1px solid #e8ebf3;border-radius:18px;box-shadow:0 30px 70px -24px rgba(30,41,59,.30);overflow:hidden;text-align:left}
.mock-bar{display:flex;align-items:center;gap:7px;padding:12px 16px;background:#f8f9fd;border-bottom:1px solid #eef0f5}
.mock-bar i{width:11px;height:11px;border-radius:50%;background:#e2e5ee;display:block}
.mock-bar i:nth-child(1){background:#ffd7d5}
.mock-bar i:nth-child(2){background:#ffeec2}
.mock-bar i:nth-child(3){background:#cdf0d4}
.mock-bar .url{margin-left:12px;font-size:12.5px;color:#94a3b8;background:#fff;border:1px solid #eef0f5;border-radius:7px;padding:4px 12px}
.mock-body{display:grid;grid-template-columns:150px 1fr;min-height:250px}
.mock-side{padding:16px 12px;background:#fbfcff;border-right:1px solid #eef0f5}
.mock-side span{display:block;font-size:13px;color:#64748b;padding:8px 11px;border-radius:8px;margin-bottom:3px}
.mock-side span.on{background:#eef0ff;color:#4f46e5;font-weight:600}
.mock-list{padding:6px 0}
.mock-item{display:flex;align-items:center;gap:12px;padding:13px 20px;border-bottom:1px solid #f5f6fa}
.mock-item:last-child{border-bottom:0}
.mock-item .av{width:32px;height:32px;border-radius:9px;flex:0 0 auto;display:flex;align-items:center;justify-content:center;font-size:13px;font-weight:700;color:#fff}
.mock-item .tx{min-width:0}
.mock-item .tx b{display:block;font-size:13.5px;color:#111827;margin-bottom:2px}
.mock-item .tx span{display:block;font-size:12.5px;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.section{max-width:1120px;margin:0 auto;padding:84px 24px 0}
.section h2{font-size:29px;letter-spacing:-.4px;text-align:center;margin-bottom:10px;color:#0f172a}
.section .lead{text-align:center;color:#64748b;font-size:15.5px;margin-bottom:44px}
.features{display:grid;grid-template-columns:repeat(3,1fr);gap:18px}
.card{background:#fff;border:1px solid #eef0f5;border-radius:16px;padding:26px 22px;transition:.2s}
.card:hover{border-color:#dfe3f5;box-shadow:0 16px 36px -18px rgba(79,70,229,.35);transform:translateY(-3px)}
.ic{width:42px;height:42px;border-radius:12px;background:linear-gradient(135deg,#eef0ff,#f6eeff);color:#4f46e5;display:flex;align-items:center;justify-content:center;margin-bottom:16px}
.ic svg{width:21px;height:21px;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.card h3{font-size:16px;margin-bottom:9px;color:#111827}
.card p{font-size:14px;color:#64748b;line-height:1.75}
.stats{display:grid;grid-template-columns:repeat(3,1fr);gap:18px;margin-top:56px;background:linear-gradient(135deg,#f7f8ff,#fbf7ff);border:1px solid #eef0f8;border-radius:18px;padding:30px 22px}
.stats div{text-align:center}
.stats b{display:block;font-size:24px;letter-spacing:-.5px;background:linear-gradient(120deg,#4f46e5,#9333ea);-webkit-background-clip:text;background-clip:text;color:transparent;margin-bottom:6px}
.stats span{font-size:13.5px;color:#64748b}
.cta-band{max-width:1120px;margin:84px auto 0;padding:0 24px}
.cta-band .in{background:linear-gradient(120deg,#4f46e5,#7c3aed 55%,#9333ea);border-radius:22px;padding:54px 30px;text-align:center;color:#fff;box-shadow:0 26px 60px -26px rgba(79,70,229,.75)}
.cta-band h2{font-size:28px;margin-bottom:12px;letter-spacing:-.4px}
.cta-band p{color:rgba(255,255,255,.85);font-size:15.5px;margin-bottom:26px}
.cta-band .btn.primary{background:#fff;color:#4f46e5}
.cta-band .btn.primary:hover{box-shadow:0 10px 26px rgba(0,0,0,.22)}
.cta-band .btn.ghost{background:rgba(255,255,255,.14);border-color:rgba(255,255,255,.4);color:#fff}
.cta-band .btn.ghost:hover{background:rgba(255,255,255,.24)}
footer{text-align:center;color:#94a3b8;font-size:13px;padding:54px 24px 40px;margin-top:70px;border-top:1px solid #eef0f5}
@media(max-width:860px){
.hero{padding-top:56px}
.hero h1{font-size:32px}
.features,.stats{grid-template-columns:1fr}
.mock-body{grid-template-columns:1fr}
.mock-side{display:none}
.nav{padding:12px 16px}
.navlink{display:none}
.section{padding-top:60px}
.cta-band{margin-top:60px}
}
</style>
</head>
<body>
<header class="nav">
  <div class="brand">__LOGO__<span>__NAME__</span></div>
  <nav>
    <a href="#features" class="navlink">产品功能</a>
    <a href="/webmail.html" class="btn ghost">登录邮箱</a>
    __REG__
  </nav>
</header>

<main>
  <section class="hero">
    <span class="pill"><i></i>专属域名邮箱服务</span>
    <h1>用 <em>@__DOMAIN__</em> 的邮箱<br>让每一次沟通更专业</h1>
    <p class="sub">__NAME__ 为 __DOMAIN__ 提供安全稳定的企业级邮件服务，支持 SPF / DKIM / DMARC 全链路防伪，网页即可收发，无需任何配置。</p>
    <div class="cta">
      <a href="/webmail.html" class="btn primary lg">登录邮箱</a>
      __REG__
    </div>
    <div class="note">__DOMAIN__ 后缀 · 独立密码 · 数据自主可控</div>

    <div class="mock">
      <div class="mock-bar"><i></i><i></i><i></i><span class="url">__DOMAIN__ / webmail</span></div>
      <div class="mock-body">
        <div class="mock-side">
          <span class="on">收件箱</span><span>已发送</span><span>写邮件</span>
        </div>
        <div class="mock-list">
          <div class="mock-item"><div class="av" style="background:linear-gradient(135deg,#6366f1,#8b5cf6)">验</div><div class="tx"><b>账号安全中心</b><span>您的登录验证码为 826 431，5 分钟内有效</span></div></div>
          <div class="mock-item"><div class="av" style="background:linear-gradient(135deg,#0ea5e9,#22d3ee)">合</div><div class="tx"><b>合作方 · 合同确认</b><span>附件：2026 年度服务协议.pdf</span></div></div>
          <div class="mock-item"><div class="av" style="background:linear-gradient(135deg,#f59e0b,#f97316)">通</div><div class="tx"><b>系统通知</b><span>欢迎使用 __DOMAIN__ 域名邮箱，祝您使用愉快</span></div></div>
        </div>
      </div>
    </div>
  </section>

  <section class="section" id="features">
    <h2>为专业场景而生的邮箱</h2>
    <p class="lead">从品牌形象到收发体验，__DOMAIN__ 邮件服务都已为你准备好</p>
    <div class="features">
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><path d="M3 7h18v10H3z"/><path d="M3 7l9 6 9-6"/></svg></div>
        <h3>专属域名</h3>
        <p>使用 __DOMAIN__ 作为邮箱后缀，统一对外形象，让每一封邮件都成为品牌名片。</p>
      </div>
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><path d="M12 3l7 3v6c0 4.4-3 8-7 9-4-1-7-4.6-7-9V6z"/><path d="M9 12l2 2 4-4"/></svg></div>
        <h3>安全防伪</h3>
        <p>SPF / DKIM / DMARC 全链路校验，有效防止伪造与钓鱼，让邮件可信可追溯。</p>
      </div>
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><path d="M13 2L4 14h6l-1 8 9-12h-6z"/></svg></div>
        <h3>秒级送达</h3>
        <p>直连投递通道，收发无中转等待，重要邮件第一时间触达收件人。</p>
      </div>
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/></svg></div>
        <h3>网页即用</h3>
        <p>无需安装任何客户端，浏览器打开即可收发、回复与转发，多端体验一致。</p>
      </div>
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><path d="M4 6h16v12H4z"/><path d="M4 10h16"/><path d="M9 14h4"/></svg></div>
        <h3>附件支持</h3>
        <p>支持常见格式附件收发与在线下载，文档、图片一键直达。</p>
      </div>
      <div class="card">
        <div class="ic"><svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="3.4"/><path d="M5 20c0-3.6 3.1-6 7-6s7 2.4 7 6"/></svg></div>
        <h3>自助注册</h3>
        <p>开放注册时可自助开通账号，管理员可随时管理账号与容量，灵活可控。</p>
      </div>
    </div>

    <div class="stats">
      <div><b>99.9%</b><span>服务可用性</span></div>
      <div><b>7 × 24</b><span>稳定投递</span></div>
      <div><b>0 配置</b><span>打开即用</span></div>
    </div>
  </section>

  <section class="cta-band">
    <div class="in">
      <h2>现在就用 __DOMAIN__ 邮箱</h2>
      <p>登录网页邮箱，或注册一个属于你的专属账号</p>
      <div class="cta">
        <a href="/webmail.html" class="btn primary lg">登录邮箱</a>
        __REG__
      </div>
    </div>
  </section>
</main>

<footer>__FOOTER__</footer>
</body>
</html>`

// ---- webmail 单页 ----
const mailPortalWebmailTpl = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>__TITLE__</title>
<link rel="icon" href="__ICON__">
<style>` + mailPortalSharedCSS + `
body{background:#eef1f8;min-height:100vh}
.center{min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;background:radial-gradient(900px 520px at 50% -8%,#e6ebff 0%,rgba(230,235,255,0) 62%),linear-gradient(165deg,#eff2fb 0%,#e6ebf7 60%,#eaf1fb 100%)}
.auth-card{width:100%;max-width:404px;background:#fff;border-radius:20px;padding:36px 32px;box-shadow:0 26px 60px -20px rgba(30,41,90,.26);border:1px solid rgba(255,255,255,.8)}
.auth-brand{display:flex;flex-direction:column;align-items:center;gap:12px;margin-bottom:22px}
.auth-brand .logo-letter{height:52px;width:52px;font-size:24px;border-radius:14px}
.auth-brand .logo{height:52px}
.auth-brand b{font-size:19px}
.tabs{display:flex;background:#f3f4f8;border-radius:11px;padding:4px;margin-bottom:22px}
.tabs button{flex:1;border:0;background:transparent;padding:9px;border-radius:8px;font-size:14px;font-weight:600;color:#6b7280;cursor:pointer}
.tabs button.on{background:#fff;color:#4f46e5;box-shadow:0 2px 8px rgba(17,24,39,.08)}
label{display:block;font-size:13px;color:#4b5563;margin:14px 0 6px;font-weight:600}
input{width:100%;padding:11px 13px;border:1px solid #e2e5ec;border-radius:10px;font-size:14px;outline:none;transition:.15s}
input:focus{border-color:#818cf8;box-shadow:0 0 0 3px rgba(99,102,241,.14)}
.full{width:100%;margin-top:22px}
.tip{font-size:12.5px;color:#9ca3af;text-align:center;margin-top:18px}
.err{background:#fef2f2;color:#dc2626;border:1px solid #fecaca;border-radius:9px;padding:9px 12px;font-size:13px;margin-top:14px;display:none}
.ok{background:#f0fdf4;color:#16a34a;border:1px solid #bbf7d0;border-radius:9px;padding:9px 12px;font-size:13px;margin-top:14px;display:none}
/* ===== 应用外壳 ===== */
.app{display:none;height:100vh;grid-template-rows:auto 1fr auto;background:#eef1f8}
.app-foot{grid-row:3;text-align:center;font-size:12.5px;color:#9aa5b8;padding:10px 16px;border-top:1px solid #e9edf5;background:#fff;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.app .top{display:flex;align-items:center;justify-content:space-between;gap:12px;background:#fff;border-bottom:1px solid #e9edf5;padding:10px 22px;box-shadow:0 1px 3px rgba(15,23,42,.04)}
.app .top .brand{display:flex;align-items:center;gap:10px;font-weight:700;font-size:16px;color:#0f172a;min-width:0}
.app .top .brand span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.app .top .right{display:flex;align-items:center;gap:10px;font-size:13.5px;color:#64748b;flex:0 0 auto}
.who{display:flex;align-items:center;gap:8px;min-width:0;margin-right:4px}
.who em{font-style:normal;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:190px}
.avatar{flex:0 0 auto;width:30px;height:30px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;font-size:13px;font-weight:700;color:#fff;background:linear-gradient(135deg,#6366f1,#8b5cf6);text-transform:uppercase;letter-spacing:.5px}

/* 两列布局：左侧文件夹 + 右侧（邮件列表 / 邮件详情 二选一） */
.main{display:grid;grid-template-columns:210px 1fr;min-height:0}
.side{background:#fff;border-right:1px solid #e9edf5;padding:14px 12px;overflow:auto}
.side a{display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:10px;font-size:14px;color:#475569;margin-bottom:3px;cursor:pointer;font-weight:500;transition:.15s}
.side a svg{width:17px;height:17px;flex:0 0 auto;stroke:currentColor;fill:none;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round;opacity:.8}
.side a:hover{background:#f5f7fc}
.side a.on{background:#eef0ff;color:#4f46e5;font-weight:600}
.side a.on svg{opacity:1}
.list{overflow:auto;background:#fff;display:block}
.msg{display:flex;align-items:center;gap:12px;padding:13px 20px;border-bottom:1px solid #f4f6fa;cursor:pointer;transition:.12s}
.msg:hover{background:#f8faff}
.msg.on{background:#f1f3ff}
.msg .av{flex:0 0 auto;width:38px;height:38px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:15px;font-weight:700;color:#fff;background:linear-gradient(135deg,#818cf8,#a78bfa);text-transform:uppercase}
.msg:nth-child(4n+2) .av{background:linear-gradient(135deg,#34d399,#10b981)}
.msg:nth-child(4n+3) .av{background:linear-gradient(135deg,#fbbf24,#f59e0b)}
.msg:nth-child(4n) .av{background:linear-gradient(135deg,#f472b6,#ec4899)}
.msg .mb{flex:1;min-width:0}
.msg .f{display:flex;align-items:baseline;justify-content:space-between;gap:10px}
.msg .f b{font-size:14px;font-weight:600;color:#1e293b;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.msg .f span{flex:0 0 auto;font-size:12px;color:#9aa5b8}
.msg .s{font-size:13.5px;color:#64748b;margin-top:3px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.msg.unread .f b{font-weight:700;color:#0f172a}
.msg.unread .f b:before{content:"";display:inline-block;width:6px;height:6px;border-radius:50%;background:#ef4444;margin-right:7px;vertical-align:middle}
.msg.unread .s{color:#334155;font-weight:500}

/* 邮件详情 */
.read{overflow:auto;background:#fff;display:none;padding:22px 30px}
.main.show-read .list{display:none}
.main.show-read .read{display:block}
/* 返回按钮：桌面端隐藏（点左侧「收件箱」即可返回），窄屏侧栏收起时才显示 */
.rd-head{display:none;margin:-4px 0 14px}
.rd-back{display:inline-flex;align-items:center;gap:6px;border:1px solid #e6e8ef;background:#fff;color:#4b5563;border-radius:10px;padding:8px 14px;font-size:13.5px;font-weight:600;cursor:pointer;transition:.15s}
.rd-back:hover{background:#f7f8fc;color:#4f46e5;border-color:#dfe3ee}
.read h2{font-size:21px;line-height:1.4;color:#0f172a;font-weight:700;margin-bottom:16px;word-break:break-word}
.read .meta{display:flex;flex-wrap:wrap;gap:6px 26px;font-size:13px;color:#475569;border-bottom:1px solid #f0f2f7;padding-bottom:16px;margin-bottom:20px;line-height:1.6}
.read .meta b{color:#9aa5b8;font-weight:600;margin-right:5px}
.read .body{font-size:14.5px;line-height:1.8;color:#1f2937}
.read iframe{width:100%;min-height:420px;border:0;border-radius:4px}
.empty{color:#9aa5b8;font-size:14px;padding:48px 24px;text-align:center}
.att{margin-top:22px;border-top:1px solid #f0f2f7;padding-top:16px}
.att>b{display:block;font-size:12.5px;color:#9aa5b8;font-weight:600;margin-bottom:10px}
.att a{display:inline-flex;align-items:center;gap:6px;margin:0 8px 8px 0;padding:8px 14px;background:#f5f7fc;border:1px solid #eaeef6;border-radius:10px;color:#334155;font-size:13px;font-weight:500;transition:.15s;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.att a:hover{background:#eef0ff;border-color:#dfe3ff;color:#4f46e5}
.modal{position:fixed;inset:0;background:rgba(15,23,42,.5);backdrop-filter:blur(3px);display:none;align-items:center;justify-content:center;padding:20px;z-index:50}
.modal.on{display:flex;animation:lpFade .16s ease}
@keyframes lpFade{from{opacity:0}to{opacity:1}}
@keyframes lpPop{from{opacity:0;transform:translateY(16px) scale(.98)}to{opacity:1;transform:none}}
.modal .box{background:#fff;border-radius:18px;width:100%;max-width:620px;max-height:92vh;display:flex;flex-direction:column;overflow:hidden;box-shadow:0 32px 70px -24px rgba(15,23,42,.5);animation:lpPop .24s cubic-bezier(.16,1,.3,1)}
.md-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:18px 22px;border-bottom:1px solid #f1f2f7}
.md-title{display:flex;align-items:center;gap:12px;min-width:0}
.md-ic{flex:0 0 auto;width:38px;height:38px;border-radius:11px;background:linear-gradient(135deg,#eef0ff,#f6eeff);color:#4f46e5;display:flex;align-items:center;justify-content:center}
.md-ic svg{width:19px;height:19px;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.md-title b{display:block;font-size:16px;color:#0f172a}
.md-title em{display:block;font-style:normal;font-size:12.5px;color:#94a3b8;margin-top:3px}
.md-x{flex:0 0 auto;width:32px;height:32px;border:0;background:transparent;border-radius:9px;font-size:20px;line-height:1;color:#94a3b8;cursor:pointer;transition:.15s}
.md-x:hover{background:#f3f4f8;color:#475569}
.md-body{padding:18px 22px 4px;overflow:auto}
.md-row{display:flex;align-items:center;gap:12px;margin-bottom:14px}
.md-label{flex:0 0 52px;font-size:13.5px;color:#64748b;font-weight:600}
.md-from{flex:1;min-width:0;font-size:13.5px;color:#334155;background:#f8f9fd;border:1px solid #eef0f5;border-radius:10px;padding:11px 13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.md-chips{flex:1;min-width:0;display:flex;flex-wrap:wrap;align-items:center;gap:7px;border:1px solid #e2e5ec;border-radius:10px;padding:6px 10px;min-height:44px;background:#fff;transition:.15s;cursor:text}
.md-chips.focus{border-color:#818cf8;box-shadow:0 0 0 3px rgba(99,102,241,.14)}
.chip{display:inline-flex;align-items:center;gap:6px;background:#eef0ff;color:#4f46e5;border-radius:8px;padding:4px 6px 4px 10px;font-size:13px;font-weight:600;max-width:100%}
.chip span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:230px}
.chip i{font-style:normal;cursor:pointer;color:#98a0c0;font-size:15px;line-height:1;padding:0 2px}
.chip i:hover{color:#dc2626}
.md-chips input{flex:1;min-width:150px;width:auto;border:0;border-radius:0;padding:4px 2px;font-size:14px;background:transparent}
.md-chips input:focus{box-shadow:none}
.md-input{flex:1;min-width:0}
.md-input:focus{border-color:#818cf8;box-shadow:0 0 0 3px rgba(99,102,241,.14)}
textarea{width:100%;padding:11px 13px;border:1px solid #e2e5ec;border-radius:10px;font-size:14px;outline:none;min-height:150px;resize:vertical;font-family:inherit}
/* 富文本编辑器 */
.mc-toolbar{display:flex;align-items:center;flex-wrap:wrap;gap:2px;padding:6px 8px;background:#f8fafc;border:1px solid #e2e8f0;border-bottom:none;border-radius:10px 10px 0 0}
.mc-tb{display:inline-flex;align-items:center;justify-content:center;min-width:27px;height:28px;padding:0 4px;border:none;background:transparent;border-radius:6px;cursor:pointer;font-size:14px;color:#334155;font-family:inherit}
.mc-tb:hover{background:#e2e8f0}
.mc-sep{width:1px;height:18px;background:#cbd5e1;margin:0 3px}
.mc-select{height:28px;border:1px solid #e2e8f0;border-radius:6px;background:#fff;color:#334155;font-size:12.5px;font-family:inherit;cursor:pointer;max-width:88px}
.mc-color{position:relative;flex-direction:column;gap:1px}
.mc-color input[type=color]{position:absolute;inset:0;opacity:0;cursor:pointer;width:100%;height:100%}
.mc-color-a{font-size:13px;line-height:1}
.mc-color-bar{width:18px;height:3px;border-radius:1px;display:block}
.mc-editor{min-height:230px;max-height:44vh;overflow-y:auto;padding:13px 15px;border:1px solid #e2e8f0;border-radius:0 0 10px 10px;font-size:14.5px;line-height:1.75;color:#1f2937;outline:none;background:#fff;transition:.15s}
.mc-editor:focus{border-color:#818cf8;box-shadow:0 0 0 3px rgba(99,102,241,.14)}
.mc-editor img{max-width:100%}
.mc-editor blockquote{border-left:3px solid #cbd5e1;margin:6px 0;padding-left:10px;color:#64748b}
.mc-atts{display:flex;flex-direction:column;gap:5px;margin-top:10px}
.mc-atts-item{display:flex;align-items:center;gap:8px;font-size:13px;color:#334155;background:#f8fafc;border:1px solid #eef2f7;border-radius:8px;padding:6px 10px}
.mc-atts-item .n{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mc-atts-item .s{color:#94a3b8;flex:0 0 auto}
.mc-atts-item i{font-style:normal;cursor:pointer;color:#98a0c0;font-size:15px;line-height:1;padding:0 2px}
.mc-atts-item i:hover{color:#dc2626}
.md-foot-left{display:flex;align-items:center;gap:10px}
#compose .box{max-width:860px}
.md-foot{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:16px 22px;border-top:1px solid #f1f2f7;background:#fcfcfe}
.md-tip{font-size:12.5px;color:#94a3b8}
.md-foot-right{display:flex;gap:10px}
.md-body .err{margin:0 0 14px}
@media(max-width:640px){
.md-row{flex-direction:column;align-items:stretch;gap:7px}
.md-label{flex:none}
.md-from,.md-chips,.md-input,.mc-editor,.mc-toolbar{flex:none;width:100%}
.md-foot{flex-direction:column;align-items:stretch}
.md-tip{text-align:center}
.md-foot-left,.md-foot-right{justify-content:center;flex-wrap:wrap}
}
.badge{background:#ef4444;color:#fff;border-radius:20px;font-size:11px;padding:1px 7px;margin-left:6px}
.btn.sm{padding:5px 11px;font-size:12.5px;border-radius:8px}
.btn.danger{color:#dc2626;border-color:#fecaca}
.btn.danger:hover{background:#fef2f2;border-color:#fca5a5}
.files-bar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px}
.files-list{display:flex;flex-direction:column;gap:8px}
.frow{display:flex;align-items:center;gap:12px;border:1px solid #eef0f5;border-radius:11px;padding:10px 12px;background:#fff}
.frow .fn{flex:1;min-width:0}
.frow .fn b{display:block;font-size:13.5px;color:#111827;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.frow .fn span{display:block;font-size:12px;color:#9ca3af;margin-top:3px}
.frow .fo{flex:0 0 auto;display:flex;align-items:center;gap:7px}
.fdir{font-size:12px;color:#64748b;background:#f5f7fc;border:1px solid #eef0f5;border-radius:8px;padding:7px 10px;word-break:break-all;margin-bottom:12px}
.fdir code{font-family:ui-monospace,Menlo,Consolas,monospace;color:#4f46e5}
.c-atts{display:flex;flex-wrap:wrap;gap:7px}
@media(max-width:640px){.frow{flex-direction:column;align-items:stretch}.frow .fo{justify-content:flex-end}}
/* ===== 平板（768–1199）：收窄侧栏、压缩留白 ===== */
@media(max-width:1199px){
.main{grid-template-columns:180px 1fr}
.app .top{padding:10px 16px}
.app .top .brand{font-size:15px}
.who em{max-width:140px}
.msg{padding:12px 16px}
.read{padding:20px 22px}
}
/* ===== 手机（<768）：侧栏改为顶部横向切换条，详情整屏 ===== */
@media(max-width:767px){
.app .top{padding:10px 14px;gap:8px}
.app .top .brand{font-size:15px;gap:8px}
.app .top .right{gap:8px}
.who em{display:none}
.app .top .right .btn{padding:8px 12px;font-size:13px}
.main{grid-template-columns:1fr;grid-template-rows:auto 1fr}
.side{display:flex;flex-direction:row;gap:8px;overflow-x:auto;border-right:none;border-bottom:1px solid #e9edf5;padding:10px 12px}
.side a{flex:0 0 auto;margin-bottom:0;padding:8px 14px;border-radius:999px;background:#f4f6fb;font-size:13.5px;white-space:nowrap}
.side a svg{width:15px;height:15px}
.side a.on{background:#4f46e5;color:#fff}
.rd-head{display:block}
.read{padding:16px 16px}
.read h2{font-size:18px}
.read iframe{min-height:62vh}
.msg{padding:12px 14px;gap:10px}
.msg .av{width:34px;height:34px;font-size:14px}
.app-foot{font-size:11.5px;padding:8px 12px}
.modal{padding:10px}
.md-body{padding:16px 16px 4px}
.md-foot{padding:14px 16px}
.md-head{padding:16px 16px}
}
/* 小屏横屏 / 矮屏：编辑区高度自适应 */
@media(max-height:560px){
.mc-editor{min-height:150px;max-height:38vh}
}
.boot{position:fixed;inset:0;z-index:200;display:flex;align-items:center;justify-content:center;background:#eef1f8}
.boot-spin{width:34px;height:34px;border:3px solid #d7dff0;border-top-color:#4f46e5;border-radius:50%;animation:bootSpin .8s linear infinite}
@keyframes bootSpin{to{transform:rotate(360deg)}}
</style>
</head>
<body>

<!-- 启动遮罩：先盖住页面，待校验登录态后再决定显示邮箱还是登录页，避免刷新时闪一下登录页 -->
<div id="bootView" class="boot"><div class="boot-spin"></div></div>

<div id="authView" class="center">
  <div class="auth-card">
    <div class="auth-brand">__LOGO__<b>__NAME__</b></div>
    <div class="tabs">
      <button id="tabLogin" class="on" onclick="LP.tab('login')">登录</button>
      <button id="tabReg" onclick="LP.tab('register')">注册</button>
    </div>
    <div id="paneLogin">
      <label>邮箱账号</label>
      <input id="loginName" placeholder="如 admin 或 admin@域名" autocomplete="username">
      <label>密码</label>
      <input id="loginPass" type="password" placeholder="请输入密码" autocomplete="current-password">
      <button class="btn primary full" onclick="LP.login()">登录</button>
    </div>
    <div id="paneReg" style="display:none">
      <label>邮箱账号</label>
      <input id="regName" placeholder="自定义账号，如 sales">
      <label>密码</label>
      <input id="regPass" type="password" placeholder="请设置密码">
      <label>确认密码</label>
      <input id="regPass2" type="password" placeholder="请再次输入密码">
      <button class="btn primary full" onclick="LP.register()">注册</button>
    </div>
    <div id="authErr" class="err"></div>
    <div id="authOk" class="ok"></div>
    <div class="tip">__FOOTER__</div>
  </div>
</div>

<div id="appView" class="app">
  <div class="top">
    <div class="brand">__LOGO__<span>__NAME__</span></div>
    <div class="right">
      <span class="who"><span class="avatar" id="whoAv">U</span><em id="whoami"></em></span>
      <button class="btn primary" onclick="LP.compose()">写邮件</button>
      <button class="btn ghost" onclick="LP.logout()">退出</button>
    </div>
  </div>
  <div id="main" class="main">
    <div class="side">
      <a id="f-inbox" class="on" onclick="LP.folder('inbox')"><svg viewBox="0 0 24 24"><path d="M3 12h4.5l1.8 3h5.4l1.8-3H21"/><path d="M5.2 5h13.6l2.2 7v6.2a1.3 1.3 0 0 1-1.3 1.3H4.3A1.3 1.3 0 0 1 3 18.2V12z"/></svg><span>收件箱</span></a>
      <a id="f-sent" onclick="LP.folder('sent')"><svg viewBox="0 0 24 24"><path d="M21.5 2.5 10.8 13.2"/><path d="M21.5 2.5 14.8 21.5l-4-8.3-8.3-4z"/></svg><span>已发送</span></a>
      <a onclick="LP.files()"><svg viewBox="0 0 24 24"><path d="M3 7.2A1.7 1.7 0 0 1 4.7 5.5h4l2 2h8.6A1.7 1.7 0 0 1 21 9.2v8.1a1.7 1.7 0 0 1-1.7 1.7H4.7A1.7 1.7 0 0 1 3 17.3z"/></svg><span>我的文件</span></a>
    </div>
    <div id="list" class="list"></div>
    <div id="read" class="read"><div class="empty">选择一封邮件查看内容</div></div>
  </div>
  __APPFOOT__
</div>

<div id="compose" class="modal">
  <div class="box">
    <div class="md-head">
      <div class="md-title">
        <span class="md-ic"><svg viewBox="0 0 24 24"><path d="M3 7h18v10H3z"/><path d="M3 7l9 6 9-6"/></svg></span>
        <div><b>写邮件</b><em id="cFrom">当前登录账号</em></div>
      </div>
      <button class="md-x" onclick="LP.closeCompose()" title="关闭">&times;</button>
    </div>
    <div class="md-body">
      <div class="md-row">
        <span class="md-label">收件人</span>
        <div class="md-chips" id="cChips" onclick="LP.focusTo()">
          <input id="cTo" placeholder="输入邮箱后回车；可用 , ; 空格 分隔多个" autocomplete="off">
        </div>
      </div>
      <div class="md-row">
        <span class="md-label">主　题</span>
        <input id="cSubject" class="md-input" placeholder="邮件主题">
      </div>
      <div class="mc-toolbar">
        <button type="button" class="mc-tb" title="撤销" onclick="LP.exec('undo')">↶</button>
        <button type="button" class="mc-tb" title="重做" onclick="LP.exec('redo')">↷</button>
        <span class="mc-sep"></span>
        <select class="mc-select" title="字体" onchange="LP.fontFamily(this)">
          <option value="">字体</option>
          <option value="Microsoft YaHei">微软雅黑</option>
          <option value="SimSun">宋体</option>
          <option value="SimHei">黑体</option>
          <option value="KaiTi">楷体</option>
          <option value="Arial">Arial</option>
          <option value="Georgia">Georgia</option>
          <option value="Courier New">Courier New</option>
        </select>
        <select class="mc-select" title="字号" onchange="LP.fontSize(this)">
          <option value="">字号</option>
          <option value="2">小</option>
          <option value="3">正常</option>
          <option value="4">中</option>
          <option value="5">大</option>
          <option value="6">特大</option>
        </select>
        <span class="mc-sep"></span>
        <button type="button" class="mc-tb" title="加粗" onclick="LP.exec('bold')"><b>B</b></button>
        <button type="button" class="mc-tb" title="斜体" onclick="LP.exec('italic')"><i>I</i></button>
        <button type="button" class="mc-tb" title="下划线" onclick="LP.exec('underline')"><u>U</u></button>
        <button type="button" class="mc-tb" title="删除线" onclick="LP.exec('strikeThrough')"><s>S</s></button>
        <span class="mc-sep"></span>
        <label class="mc-tb mc-color" title="字体颜色">
          <span class="mc-color-a">A</span>
          <span class="mc-color-bar" id="cForeBar" style="background:#000000"></span>
          <input type="color" value="#000000" oninput="LP.foreColor(this.value)">
        </label>
        <label class="mc-tb mc-color" title="背景颜色">
          <span class="mc-color-a">▨</span>
          <span class="mc-color-bar" id="cBackBar" style="background:#ffff00"></span>
          <input type="color" value="#ffff00" oninput="LP.backColor(this.value)">
        </label>
        <span class="mc-sep"></span>
        <button type="button" class="mc-tb" title="左对齐" onclick="LP.exec('justifyLeft')">左</button>
        <button type="button" class="mc-tb" title="居中" onclick="LP.exec('justifyCenter')">中</button>
        <button type="button" class="mc-tb" title="右对齐" onclick="LP.exec('justifyRight')">右</button>
        <span class="mc-sep"></span>
        <button type="button" class="mc-tb" title="无序列表" onclick="LP.exec('insertUnorderedList')">•≡</button>
        <button type="button" class="mc-tb" title="有序列表" onclick="LP.exec('insertOrderedList')">1≡</button>
        <button type="button" class="mc-tb" title="引用" onclick="LP.exec('formatBlock','blockquote')">❝</button>
        <span class="mc-sep"></span>
        <button type="button" class="mc-tb" title="插入链接" onclick="LP.insertLink()">🔗</button>
        <button type="button" class="mc-tb" title="插入图片(≤5M)" onclick="LP.pickImage()">🖼</button>
        <button type="button" class="mc-tb" title="清除格式" onclick="LP.exec('removeFormat')">Tx</button>
      </div>
      <div id="cBody" class="mc-editor" contenteditable="true"></div>
      <div id="cAtts" class="mc-atts" style="display:none"></div>
      <div id="cErr" class="err"></div>
    </div>
    <div class="md-foot">
      <div class="md-foot-left">
        <button type="button" class="btn ghost" onclick="LP.pickAttach()">附件</button>
        <span class="md-tip">图片 ≤5M 内嵌 · 附件总计 ≤25M</span>
      </div>
      <div class="md-foot-right">
        <button class="btn ghost" onclick="LP.closeCompose()">取消</button>
        <button class="btn primary" id="cSend" onclick="LP.send()">发送</button>
      </div>
    </div>
    <input id="cImgUp" type="file" accept="image/*" style="display:none">
  </div>
</div>

<div id="filesModal" class="modal">
  <div class="box">
    <div class="md-head">
      <div class="md-title">
        <span class="md-ic"><svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg></span>
        <div><b id="filesTitle">我的文件</b><em>上传的附件会保存在下面的目录中</em></div>
      </div>
      <button class="md-x" onclick="LP.closeFiles()" title="关闭">&times;</button>
    </div>
    <div class="md-body">
      <div class="fdir">存放目录：<code id="filesDir">—</code></div>
      <div class="files-bar">
        <label class="btn ghost sm" style="margin:0">上传文件<input id="filesUp" type="file" multiple style="display:none"></label>
        <span class="md-tip">单文件 ≤25MB，可多选；上传后即可在写信时作为附件发送</span>
      </div>
      <div id="filesList" class="files-list"></div>
    </div>
    <div class="md-foot">
      <span class="md-tip">删除后不可恢复，请谨慎操作</span>
      <div class="md-foot-right"><button class="btn ghost" onclick="LP.closeFiles()">关闭</button></div>
    </div>
  </div>
</div>

<script>
var LP = (function(){
  var TOKEN_KEY='lp_mail_token';
  var REGISTER=__REGISTER__;
  var token=localStorage.getItem(TOKEN_KEY)||'';
  var me=null, folder='inbox', msgs=[], curId=0, recips=[];
  var atts=[], files=[], filesMode='manage';
  var EMAIL_RE=/^[^@\s]+@[^@\s]+\.[^@\s]+$/;

  function $(id){return document.getElementById(id);}
  function showErr(el,msg){el.textContent=msg;el.style.display='block';}
  function hide(el){el.style.display='none';}
  // 移除启动遮罩（校验完登录态后调用）
  function bootDone(){var b=$('bootView');if(b&&b.parentNode){b.parentNode.removeChild(b);}}
  function fmt(ts){try{return new Date(ts*1000).toLocaleString();}catch(e){return '';}}
  function esc(s){var d=document.createElement('div');d.textContent=s==null?'':s;return d.innerHTML.replace(/"/g,'&quot;').replace(/'/g,'&#39;');}
  function fmtSize(n){n=n||0;if(n<1024)return n+' B';if(n<1048576)return (n/1024).toFixed(1)+' KB';return (n/1048576).toFixed(2)+' MB';}

  function api(path,opts){
    opts=opts||{};
    var headers=opts.headers||{};
    if(token)headers['Authorization']='Bearer '+token;
    if(opts.body&&!headers['Content-Type'])headers['Content-Type']='application/json';
    return fetch('/api/mail-portal'+path,{method:opts.method||'GET',headers:headers,body:opts.body})
      .then(function(r){
        return r.json().catch(function(){return {};}).then(function(j){
          if(!r.ok){throw new Error((j&&j.msg)||('请求失败 '+r.status));}
          if(j&&j.code&&j.code!==0){throw new Error(j.msg||'请求失败');}
          return j?j.data:null;
        });
      });
  }

  // 上传（multipart，不能手动设置 Content-Type，交给浏览器带 boundary）
  function apiUpload(path,fd){
    var headers={};
    if(token)headers['Authorization']='Bearer '+token;
    return fetch('/api/mail-portal'+path,{method:'POST',headers:headers,body:fd})
      .then(function(r){
        return r.json().catch(function(){return {};}).then(function(j){
          if(!r.ok){throw new Error((j&&j.msg)||('请求失败 '+r.status));}
          if(j&&j.code&&j.code!==0){throw new Error(j.msg||'请求失败');}
          return j?j.data:null;
        });
      });
  }

  function tab(which){
    var isLogin=which==='login';
    $('tabLogin').className=isLogin?'on':'';
    $('tabReg').className=isLogin?'':'on';
    $('paneLogin').style.display=isLogin?'':'none';
    $('paneReg').style.display=isLogin?'none':'';
    hide($('authErr'));hide($('authOk'));
  }

  function enterApp(){
    $('authView').style.display='none';
    $('appView').style.display='grid';
    var addr=(me&&me.address)?me.address:'';
    $('whoami').textContent=addr;
    var av=$('whoAv');if(av)av.textContent=(addr.charAt(0)||'U').toUpperCase();
    bootDone();
    loadMessages();
  }

  function login(){
    hide($('authErr'));hide($('authOk'));
    var name=($('loginName').value||'').trim(), pass=$('loginPass').value||'';
    if(!name||!pass){showErr($('authErr'),'请输入账号和密码');return;}
    api('/login',{method:'POST',body:JSON.stringify({name:name,password:pass})})
      .then(function(d){
        token=d.token;localStorage.setItem(TOKEN_KEY,token);me=d.mailbox;enterApp();
      })
      .catch(function(e){showErr($('authErr'),e.message);});
  }

  function register(){
    hide($('authErr'));hide($('authOk'));
    var name=($('regName').value||'').trim(), pass=$('regPass').value||'', pass2=$('regPass2').value||'';
    if(!name||!pass){showErr($('authErr'),'请输入账号和密码');return;}
    if(pass!==pass2){showErr($('authErr'),'两次输入的密码不一致，请重新输入');return;}
    api('/register',{method:'POST',body:JSON.stringify({name:name,password:pass})})
      .then(function(){
        $('regPass').value='';$('regPass2').value='';
        tab('login');
        $('loginName').value=name;
        showErr($('authOk'),'注册成功，请使用该账号登录');
      })
      .catch(function(e){showErr($('authErr'),e.message);});
  }

  function logout(){
    api('/logout',{method:'POST'}).catch(function(){});
    token='';localStorage.removeItem(TOKEN_KEY);me=null;
    $('appView').style.display='none';$('authView').style.display='flex';
  }

  function loadMe(){
    if(!token){return Promise.reject();}
    return api('/me').then(function(d){me=d;enterApp();});
  }

  function folderSet(f){
    folder=f;
    curId=0;
    $('f-inbox').className=f==='inbox'?'on':'';
    $('f-sent').className=f==='sent'?'on':'';
    $('main').className='main';
    loadMessages();
  }

  function renderMsgs(){
    if(!msgs.length){
      $('list').innerHTML='<div class="empty">'+(folder==='sent'?'暂无已发送邮件':'收件箱是空的')+'</div>';
      return;
    }
    var h='';
    for(var i=0;i<msgs.length;i++){
      var m=msgs[i];
      var who=folder==='sent'?(m.to_addrs||''):(m.from_name||m.from_addr||'');
      var initial=(String(who).trim().charAt(0)||'?').toUpperCase();
      h+='<div class="msg'+(m.seen?'':' unread')+(m.id===curId?' on':'')+'" id="msg-'+m.id+'" onclick="LP.open('+m.id+')">'+
         '<div class="av">'+esc(initial)+'</div>'+
         '<div class="mb"><div class="f"><b>'+esc(who)+'</b><span>'+fmt(m.date)+'</span></div>'+
         '<div class="s">'+esc(m.subject||'(无主题)')+'</div></div></div>';
    }
    $('list').innerHTML=h;
  }

  function loadMessages(){
    $('list').innerHTML='<div class="empty">加载中…</div>';
    api('/messages?folder='+encodeURIComponent(folder))
      .then(function(list){
        msgs=list||[];
        renderMsgs();
      })
      .catch(function(e){
        if(/过期|登录/.test(e.message)){logout();return;}
        msgs=[];
        $('list').innerHTML='<div class="empty">'+esc(e.message)+'</div>';
      });
  }

  // 从邮件详情返回列表（腾讯/新浪邮箱那种交互）
  function back(){
    $('main').className='main';
    renderMsgs();
  }

  function open(id){
    curId=id;
    $('main').className='main show-read';
    $('read').innerHTML='<div class="empty">加载中…</div>';
    api('/messages/'+id).then(function(m){
      var atts='';
      if(m.attachments&&m.attachments.length){
        atts='<div class="att"><b>\u9644\u4ef6\uff08'+m.attachments.length+'\uff09</b>';
        for(var i=0;i<m.attachments.length;i++){
          var a=m.attachments[i];
          atts+='<a href="/api/mail-portal/messages/'+id+'/attachments/'+i+'" target="_blank">\uD83D\uDCCE '+esc(a.filename)+'</a>';
        }
        atts+='</div>';
      }
      var useHtml=!!m.html_body;
      var body='';
      if(useHtml){
        // 正文用 srcdoc 属性赋值（而非拼 HTML 字符串），避免正文里的引号把属性截断
        body='<iframe class="mail-html" sandbox></iframe>';
      }else{
        body='<div class="body" style="white-space:pre-wrap">'+esc(m.text_body||'(无正文内容)')+'</div>';
      }
      var sender=esc(m.from_name||m.from_addr||'\u2014');
      if(m.from_name&&m.from_addr)sender+=' &lt;'+esc(m.from_addr)+'&gt;';
      var meta='<div class="meta">'+
        '<span><b>\u53d1\u4ef6\u4eba</b>'+sender+'</span>'+
        (m.to_addrs?'<span><b>\u6536\u4ef6\u4eba</b>'+esc(m.to_addrs)+'</span>':'')+
        '<span><b>\u65f6\u95f4</b>'+fmt(m.date)+'</span></div>';
      $('read').innerHTML='<div class="rd-head"><button type="button" class="rd-back" onclick="LP.back()">\u2190 返回'+esc(folder==='sent'?'已发送':'收件箱')+'</button></div>'+
        '<h2>'+esc(m.subject||'(无主题)')+'</h2>'+meta+body+atts;
      if(useHtml){
        var fr=$('read').querySelector('iframe.mail-html');
        if(fr)fr.srcdoc=m.html_body;
      }
      // 本地标记已读（返回列表时生效）
      var idx=-1;for(var k=0;k<msgs.length;k++){if(msgs[k].id===id){idx=k;break;}}
      if(idx>=0){msgs[idx].seen=true;var n=$('list').children[idx];if(n)n.className='msg on';}
    }).catch(function(e){
      $('read').innerHTML='<div class="empty">'+esc(e.message)+'</div>';
    });
  }

  function renderChips(){
    var box=$('cChips'), input=$('cTo');
    if(!box||!input){return;}
    var olds=box.querySelectorAll('.chip');
    for(var i=0;i<olds.length;i++){olds[i].parentNode.removeChild(olds[i]);}
    for(var j=0;j<recips.length;j++){
      (function(addr,idx){
        var el=document.createElement('span');
        el.className='chip';
        var tx=document.createElement('span');
        tx.textContent=addr;
        var del=document.createElement('i');
        del.textContent='\u00d7';
        del.title='移除';
        del.onclick=function(e){if(e&&e.stopPropagation){e.stopPropagation();}recips.splice(idx,1);renderChips();};
        el.appendChild(tx);
        el.appendChild(del);
        box.insertBefore(el,input);
      })(recips[j],j);
    }
  }

  function addRecips(raw){
    var parts=String(raw||'').split(/[,，;；\s\n\r]+/);
    var bad=0, added=0;
    for(var i=0;i<parts.length;i++){
      var p=parts[i].trim();
      if(!p){continue;}
      if(!EMAIL_RE.test(p)){bad++;continue;}
      if(recips.indexOf(p)<0){recips.push(p);added++;}
    }
    if(added){renderChips();}
    return {added:added,bad:bad};
  }

  function onToKey(e){
    var v=$('cTo').value||'';
    if(!v){return;}
    if(e&&e.key==='Enter'||/[,，;；\s\n\r]/.test(v)){
      var r=addRecips(v);
      $('cTo').value='';
      if(r.bad){showErr($('cErr'),'邮箱格式不正确，已忽略');}else{hide($('cErr'));}
    }
  }

  function onToPaste(e){
    var cb=e.clipboardData||window.clipboardData;
    var t=cb?cb.getData('text'):'';
    if(!t){return;}
    e.preventDefault();
    var r=addRecips(t);
    if(r.bad){showErr($('cErr'),'有 '+r.bad+' 个邮箱格式不正确，已忽略');}else{hide($('cErr'));}
  }

  function focusTo(){var el=$('cTo');if(el){try{el.focus();}catch(e){}}}

  // ===== 我的文件（附件库）=====

  function renderAtts(){
    var box=$('cAtts');if(!box)return;
    if(!atts.length){box.innerHTML='';box.style.display='none';return;}
    var h='';
    for(var i=0;i<atts.length;i++){
      var f=null;
      for(var k=0;k<files.length;k++){if(files[k].name===atts[i]){f=files[k];break;}}
      h+='<div class="mc-atts-item"><span class="n">'+esc(atts[i])+'</span>'+
         (f?'<span class="s">'+fmtSize(f.size)+'</span>':'')+
         '<i onclick="LP.rmAttach('+i+')" title="移除">\u00d7</i></div>';
    }
    box.innerHTML=h;
    box.style.display='flex';
  }
  function rmAttach(i){atts.splice(i,1);renderAtts();}

  function addAttach(i){
    var f=files[i];if(!f)return;
    if(atts.indexOf(f.name)<0)atts.push(f.name);
    renderAtts();
    renderFiles();
  }

  function openFiles(mode){
    filesMode=mode||'manage';
    $('filesTitle').textContent=filesMode==='pick'?'选择附件':'我的文件';
    $('filesModal').className='modal on';
    loadFiles();
  }
  function files(){openFiles('manage');}
  function pickAttach(){
    if(!atts){atts=[];}
    openFiles('pick');
  }
  function closeFiles(){$('filesModal').className='modal';}

  function renderFiles(){
    var list=$('filesList');if(!list)return;
    if(!files.length){list.innerHTML='<div class="empty">还没有文件，点击上方「上传文件」添加</div>';return;}
    var h='';
    for(var i=0;i<files.length;i++){
      var f=files[i];
      var picked=atts.indexOf(f.name)>=0;
      h+='<div class="frow">'+
         '<div class="fn"><b>'+esc(f.name)+'</b><span>'+fmtSize(f.size)+' · '+fmt(f.mtime)+'</span></div>'+
         '<div class="fo">'+
         (filesMode==='pick'?('<button type="button" class="btn ghost sm" onclick="LP.addAttach('+i+')">'+(picked?'已添加':'添加')+'</button>'):'')+
         '<button type="button" class="btn ghost sm" onclick="LP.download('+i+')">下载</button>'+
         '<button type="button" class="btn ghost sm danger" onclick="LP.delFile('+i+')">删除</button>'+
         '</div></div>';
    }
    list.innerHTML=h;
  }

  function loadFiles(){
    var list=$('filesList');if(!list)return;
    list.innerHTML='<div class="empty">加载中…</div>';
    api('/files').then(function(d){
      var dir=$('filesDir');if(dir)dir.textContent=(d&&d.dir)||'—';
      files=(d&&d.files)||[];
      renderFiles();
    }).catch(function(e){
      var dir=$('filesDir');if(dir)dir.textContent='—';
      files=[];
      list.innerHTML='<div class="empty">'+esc(e.message)+'</div>';
    });
  }

  function delFile(i){
    var f=files[i];if(!f)return;
    if(!confirm('确定删除文件「'+f.name+'」？删除后不可恢复。'))return;
    api('/files/'+encodeURIComponent(f.name),{method:'DELETE'}).then(function(){
      var k=atts.indexOf(f.name);if(k>=0)atts.splice(k,1);
      renderAtts();
      loadFiles();
    }).catch(function(e){alert(e.message);});
  }

  function download(i){
    var f=files[i];if(!f)return;
    var headers={};
    if(token)headers['Authorization']='Bearer '+token;
    fetch('/api/mail-portal/files/'+encodeURIComponent(f.name),{headers:headers})
      .then(function(r){if(!r.ok)throw new Error('下载失败');return r.blob();})
      .then(function(b){
        var a=document.createElement('a');
        a.href=URL.createObjectURL(b);
        a.download=f.name;
        document.body.appendChild(a);a.click();
        setTimeout(function(){URL.revokeObjectURL(a.href);if(a.parentNode)a.parentNode.removeChild(a);},1500);
      })
      .catch(function(e){alert(e.message);});
  }

  // 依次上传多个文件，全部完成后再刷新列表
  function uploadSeq(fileArr,onDone){
    var list=Array.prototype.slice.call(fileArr||[]);
    if(!list.length){if(onDone)onDone();return;}
    var i=0,failed=0;
    function next(){
      if(i>=list.length){
        if(failed)alert('有 '+failed+' 个文件上传失败');
        if(onDone)onDone();
        return;
      }
      var f=list[i++];
      if(f.size>25*1024*1024){failed++;next();return;}
      var fd=new FormData();fd.append('file',f);
      apiUpload('/files',fd).catch(function(){failed++;}).then(next);
    }
    next();
  }

  function onFilesUp(e){
    // 注意：清空 input.value 会同时清空 FileList，必须先复制成数组
    var fs=Array.prototype.slice.call(e.target.files||[]);
    e.target.value='';
    var before=files.map(function(x){return x.name;});
    uploadSeq(fs,function(){
      loadFiles();
      // 从写信的「附件」入口进来时，上传完自动加入附件
      if(filesMode!=='pick')return;
      api('/files').then(function(d){
        var all=(d&&d.files)||[];
        for(var i=0;i<all.length;i++){
          if(before.indexOf(all[i].name)<0&&atts.indexOf(all[i].name)<0){atts.push(all[i].name);}
        }
        renderAtts();
      }).catch(function(){});
    });
  }

  // ===== 富文本编辑器 =====

  function exec(cmd,val){
    var el=$('cBody');
    if(el)el.focus();
    try{
      document.execCommand('styleWithCSS',false,true);
      document.execCommand(cmd,false,val);
    }catch(e){}
  }
  function fontFamily(sel){if(sel.value){exec('fontName',sel.value);}sel.value='';}
  function fontSize(sel){if(sel.value){exec('fontSize',sel.value);}sel.value='';}
  function foreColor(v){var b=$('cForeBar');if(b)b.style.background=v;exec('foreColor',v);}
  function backColor(v){var b=$('cBackBar');if(b)b.style.background=v;exec('hiliteColor',v);}
  function insertLink(){
    var u=prompt('请输入链接地址','https://');
    if(u){exec('createLink',u);}
  }
  function pickImage(){var el=$('cImgUp');if(el)el.click();}
  function onPickImage(e){
    var file=(e.target.files||[])[0];
    e.target.value='';
    if(!file)return;
    if(file.type.indexOf('image/')!==0){alert('请选择图片文件');return;}
    if(file.size>5*1024*1024){alert('图片不能超过 5MB');return;}
    var reader=new FileReader();
    reader.onload=function(){
      var el=$('cBody');
      if(el)el.focus();
      try{document.execCommand('insertHTML',false,'<img src="'+reader.result+'" style="max-width:100%">');}catch(err){}
    };
    reader.readAsDataURL(file);
  }

  // 收集正文：HTML + 纯文本；正文里的 data URL 图片转成内嵌附件（cid 引用）
  function collectBody(){
    var el=$('cBody');
    var html=el?el.innerHTML:'';
    var text=el?(el.innerText||'').replace(/\u00a0/g,' ').trim():'';
    var inline=[];
    html=html.replace(/<img\b[^>]*\bsrc="(data:image\/[^"]+)"[^>]*>/gi,function(m,dataUrl){
      var semi=dataUrl.indexOf(';'), comma=dataUrl.indexOf(',');
      if(semi<0||comma<0)return m;
      var mime=dataUrl.slice(5,semi);
      var b64=dataUrl.slice(comma+1);
      var cid='img_'+Date.now()+'_'+Math.random().toString(36).slice(2,8);
      inline.push({filename:cid+'.'+(mime.split('/')[1]||'png'),type:mime,cid:cid,data:b64});
      return m.replace(dataUrl,'cid:'+cid);
    });
    return {html:html,text:text,inline:inline};
  }

  function compose(){
    recips=[];atts=[];
    $('cTo').value='';$('cSubject').value='';
    var bd=$('cBody');if(bd)bd.innerHTML='';
    renderAtts();
    hide($('cErr'));
    $('cFrom').textContent=(me&&me.address)?me.address:'当前登录账号';
    renderChips();
    $('compose').className='modal on';
    setTimeout(focusTo,80);
  }
  function closeCompose(){$('compose').className='modal';}

  function send(){
    hide($('cErr'));
    var rest=$('cTo').value||'';
    if(rest.replace(/\s/g,'')){addRecips(rest);$('cTo').value='';}
    if(!recips.length){showErr($('cErr'),'请填写收件人');focusTo();return;}
    var body=collectBody();
    if(!body.text&&!atts.length){showErr($('cErr'),'请输入正文或添加附件');return;}
    var payload={to:recips,subject:($('cSubject').value||'').trim(),text:body.text,html:body.html,attachments:atts.slice(),inline_images:body.inline};
    var btn=$('cSend');
    if(btn){btn.disabled=true;btn.textContent='发送中…';}
    api('/send',{method:'POST',body:JSON.stringify(payload)})
      .then(function(){
        closeCompose();
        recips=[];atts=[];$('cTo').value='';$('cSubject').value='';
        var bd=$('cBody');if(bd)bd.innerHTML='';
        renderChips();renderAtts();
        alert('发送成功');
      })
      .catch(function(e){showErr($('cErr'),e.message);})
      .then(function(){if(btn){btn.disabled=false;btn.textContent='发送';}});
  }

  var toInput=$('cTo');
  if(toInput){
    toInput.addEventListener('keyup',onToKey);
    toInput.addEventListener('paste',onToPaste);
    toInput.addEventListener('focus',function(){var c=$('cChips');if(c){c.className='md-chips focus';}});
    toInput.addEventListener('blur',function(){var c=$('cChips');if(c){c.className='md-chips';}});
  }
  $('compose').addEventListener('click',function(e){if(e.target===this){closeCompose();}});
  var fu=$('filesUp');if(fu)fu.addEventListener('change',onFilesUp);
  var iu=$('cImgUp');if(iu)iu.addEventListener('change',onPickImage);
  $('filesModal').addEventListener('click',function(e){if(e.target===this){closeFiles();}});
  document.addEventListener('keydown',function(e){
    if(e.key==='Escape'){
      if($('filesModal').className.indexOf('on')>=0){closeFiles();return;}
      if($('compose').className.indexOf('on')>=0){closeCompose();}
    }
  });

  if(!REGISTER){var tr=$('tabReg');if(tr)tr.style.display='none';}
  else if(location.hash==='#register'){tab('register');}
  if(token){
    // 有登录态：先显示启动遮罩，校验通过直接进邮箱，失败再回登录页，避免闪一下登录页
    loadMe().catch(function(){logout();}).then(bootDone);
  }else{
    bootDone();
    if(location.hash==='#register'&&REGISTER){tab('register');}
  }

  return {tab:tab,login:login,register:register,logout:logout,folder:folderSet,open:open,back:back,compose:compose,closeCompose:closeCompose,send:send,focusTo:focusTo,files:files,closeFiles:closeFiles,pickAttach:pickAttach,addAttach:addAttach,rmAttach:rmAttach,delFile:delFile,download:download,exec:exec,fontFamily:fontFamily,fontSize:fontSize,foreColor:foreColor,backColor:backColor,insertLink:insertLink,pickImage:pickImage};
})();
</script>
</body>
</html>`
