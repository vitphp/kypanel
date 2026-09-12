package service

// ==================== 迁出到对端面板：非 PHP 项目（Java / Node / Python / Go） ====================
//
// 对端面板（宝塔）的 Java / Node / Python / Go 项目不是「网站」：网站接口（site?action=AddSite）
// 只会建一条 PHP/静态站点记录——没有 project_config、没有守护进程、也不会把域名反代到应用端口，
// 结果就是「Java/Node 站点迁过去全变成 PHP 网站」。项目必须走项目接口创建：
//
//	新版：POST /mod/<java|nodejs|python|go>/project/<方法>/stype（body: data=JSON）
//	旧版：POST /project?action=model&mod_name=<模块>&def_name=<方法>（body: data=JSON）
//
// 两代接口参数一致，btProjectCall 会逐个尝试直到命中。
//
// 关键前提：项目目录、jar 包、可执行文件必须先在服务器上就位——对端面板创建项目时会校验它们存在，
// 因此迁出流程对进程型站点改为「先上传解压文件 → 再创建项目」。

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kypanel/internal/model"
)

// btProjectRemark 迁移到对端面板的项目备注
const btProjectRemark = "由 kypanel 网站搬家迁入"

// 对端面板项目模块名（/mod/<模块>/project/... 与 /project?action=model&mod_name=<模块>）
const (
	btModJava   = "java"
	btModNode   = "nodejs"
	btModPython = "python"
	btModGo     = "go"
)

// btProjectModule 站点类型 → 项目模块名
func btProjectModule(siteType string) string {
	switch siteType {
	case model.SiteTypeJava:
		return btModJava
	case model.SiteTypeNode:
		return btModNode
	case model.SiteTypePython:
		return btModPython
	case model.SiteTypeGo:
		return btModGo
	}
	return ""
}

// btRemoveProject 删除对端面板上的进程型项目（项目删除接口会一并清理守护进程；
// 仅删站点记录会留下仍在运行的服务与 project_config，导致重建时报「项目名称已存在」）。
// 调用方（覆盖迁移）在失败时会回退到站点删除接口，因此这里尽力而为即可。
func btRemoveProject(bt *BTClient, siteType, projectName string) error {
	mod := btProjectModule(siteType)
	if mod == "" {
		return fmt.Errorf("不支持的站点类型 %s", siteType)
	}
	fields := map[string]any{
		"def_name":     "remove_project",
		"project_name": projectName,
	}
	switch siteType {
	case model.SiteTypeNode:
		fields["project_type"] = "nodejs"
	case model.SiteTypePython:
		fields["pjname"] = projectName
	}
	urls := btProjectURLs(mod, "", "remove_project", "")
	urls = append(urls, btProjectURLs(mod, "", "delete", "")...)
	_, err := bt.btProjectCall(urls, fields)
	return err
}

// btProjectName 生成对端面板可接受的项目名。
// 对端面板要求项目名匹配 ^\w+$（字母/数字/下划线），Java 项目还限制 1~20 字符；
// 而 kypanel 站点名多为域名（含 "." "-"），这里统一替换并截断。
func btProjectName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		s = "app"
	}
	if len(s) > 20 {
		s = s[:20]
	}
	return s
}

// btDomainBindings 把域名列表转成对端面板项目接口要求的 "域名:端口" 形式。
// 端口为站点监听端口（HTTP 默认 80）：对端面板按它生成 vhost，并自动反代到项目端口。
func btDomainBindings(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if strings.Contains(d, ":") {
			out = append(out, d)
			continue
		}
		out = append(out, d+":80")
	}
	return out
}

// btEnvVarsHint 源站配置了环境变量时的提示。
// 对端面板各项目接口对环境变量的入参格式（env_list / env_file）版本差异大，
// 传错格式会写坏启动环境，因此除 Java（格式已实测确认）外不自动搬运，只提示用户手动补齐。
func btEnvVarsHint(t *ImportTask, ms *MigrateSite) {
	if strings.TrimSpace(ms.EnvVars) == "" {
		return
	}
	t.logf("提示：网站 %s 在源站配置了环境变量，对端面板接口不支持直接导入，请到对端项目设置里手动补充", ms.Name)
}

// btJavaEnvList 把「KEY=VALUE 每行一个」的环境变量文本转成对端 Java 项目接口要求的
// [{"k":"KEY","v":"VALUE"}] 结构（模块内部按 i["k"] / i["v"] 写入 .env 文件）。
func btJavaEnvList(envVars string) []map[string]string {
	var out []map[string]string
	for _, line := range strings.Split(envVars, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out = append(out, map[string]string{"k": k, "v": strings.TrimSpace(v)})
	}
	return out
}

// createBtProject 在对端面板创建进程型项目（Java / Node / Python / Go）。
// path 为已确认过（非系统关键目录）的项目目录，调用前站点文件应已解压到该目录。
func createBtProject(t *ImportTask, bt *BTClient, ms *MigrateSite, path string, domains []string, port int) error {
	switch ms.Type {
	case model.SiteTypeJava:
		return createBtJavaProject(t, bt, ms, path, domains, port)
	case model.SiteTypeNode:
		return createBtNodeProject(t, bt, ms, path, domains, port)
	case model.SiteTypeGo:
		return createBtGoProject(t, bt, ms, path, domains, port)
	case model.SiteTypePython:
		return createBtPythonProject(t, bt, ms, path, domains, port)
	default:
		return fmt.Errorf("不支持在对端面板创建 %s 项目", ms.Type)
	}
}

// createBtJavaProject 创建 Java（SpringBoot）项目。
//
// 对端面板字段要求（实测 11.5，缺 project_ps/proxy_path 会直接报「参数格式错误」）：
//
//	project_name 1~20 字符、project_jar 必须是已存在的 jar 文件、project_jdk 必须是可用 JDK、
//	project_cmd 需带绝对 java 路径、domains 为 "域名:端口" 数组。
//	对端不校验 port（按进程监听端口自动识别），这里不传，避免把反代端口写错。
func createBtJavaProject(t *ImportTask, bt *BTClient, ms *MigrateSite, path string, domains []string, port int) error {
	jar := filepath.Base(strings.TrimSpace(ms.JarFile))
	if jar == "" || jar == "." || !bt.RemoteFileExists(filepath.Join(path, jar)) {
		if jars := bt.ListJars(path); len(jars) > 0 {
			jar = jars[0]
		} else {
			return errors.New("项目目录中未找到 jar 包，无法在对端面板创建 Java 项目")
		}
	}
	jdk := bt.PickJavaHome(ms.RuntimeVersion)
	if jdk == "" {
		if jdks := bt.JavaJDKs(); len(jdks) > 0 {
			// 对端有 JDK 但版本不够：源站 JDK 21 编译的 jar（class 65.0）在 JDK 8 上会报
			// UnsupportedClassVersionError，硬建出来也是跑不起来的，这里直接说清要装哪个版本。
			return fmt.Errorf("源站项目需要 JDK %d，对端面板只装了 %s，请在对端面板「网站 → Java 项目」中安装 JDK %d 后重试",
				btJavaMajor(ms.RuntimeVersion), btJavaVersionsText(jdks), btJavaMajor(ms.RuntimeVersion))
		}
		return errors.New("对端面板未安装 JDK，请先在对端面板「网站 → Java 项目」中安装 JDK 后重试")
	}
	jarPath := filepath.Join(path, jar)
	cmd := btJavaCmd(jdk, ms, jarPath)

	fields := map[string]any{
		"def_name":     "create_project",
		"project_type": 0, // 0 = SpringBoot（统一入口 create_project 按 project_type 分发）
		"project_name": btProjectName(ms.Name),
		"project_jar":  jarPath,
		"project_jdk":  jdk,
		"run_user":     "www",
		"project_cmd":  cmd,
		"project_ps":   btProjectRemark, // 必填
		"proxy_path":   "/",             // 必填
	}
	if len(domains) > 0 {
		fields["domains"] = btDomainBindings(domains)
		// 关键：要让对端自动生成「域名 → 应用端口」的反向代理，必须同时传 proxy_dir 和 port。
		// 对端 create_project 里只认 proxy_dir（我们之前传的 proxy_path 只是站点元数据，不触发反代），
		// 反代目标端口取的是 get.port，缺了它域名就只指向静态目录、访问不到应用端口。
		fields["proxy_dir"] = "/"
		if port > 0 {
			fields["port"] = port
		}
	}
	// 环境变量：对端 Java 项目的 env_list 格式为 [{"k":..,"v":..}]（模块按 k/v 写 .env 文件），
	// 本机存的是「KEY=VALUE 每行一个」，这里转换后直接带上，避免迁移后要人工补环境变量。
	if env := btJavaEnvList(ms.EnvVars); len(env) > 0 {
		fields["env_list"] = env
	}
	urls := btProjectURLs(btModJava, "", "create_project", "")
	urls = append(urls, btProjectURLs(btModJava, "", "create_spring_boot_project", "")...)
	if _, err := bt.btProjectCall(urls, fields); err != nil {
		return fmt.Errorf("Java 项目创建失败: %w", err)
	}
	t.logf("对端面板 Java 项目 %s 创建完成（jar %s，JDK %s，启动命令 %s）",
		fields["project_name"], jar, jdk, cmd)
	btEnvVarsHint(t, ms)
	return nil
}

// btJavaCmd 生成对端 Java 项目的启动命令（对端要求 java 用绝对路径，且要跟源站跑法一致）。
//
//   - 源站填了自定义启动命令：沿用原命令，只把开头的 java 换成对端 JDK 的绝对路径；
//   - 源站用默认规则（本机 javaDefaultRun 生成）：java [JVM参数] -jar <jar> --server.port=<端口>。
//     端口参数必须带上，否则应用退回到自己的默认端口，对端反代的目标端口就跟源站不一致。
func btJavaCmd(jdk string, ms *MigrateSite, jarPath string) string {
	java := jdk + "/bin/java"
	if sc := strings.TrimSpace(ms.StartCommand); sc != "" {
		fields := strings.Fields(sc)
		if len(fields) > 0 && (fields[0] == "java" || strings.HasSuffix(fields[0], "/java")) {
			fields[0] = java
			return strings.Join(fields, " ")
		}
		return sc
	}
	parts := []string{java}
	if args := strings.TrimSpace(ms.JvmArgs); args != "" {
		parts = append(parts, args)
	}
	parts = append(parts, "-jar", jarPath)
	if ms.ProxyPort > 0 {
		parts = append(parts, fmt.Sprintf("--server.port=%d", ms.ProxyPort))
	}
	return strings.Join(parts, " ")
}

// createBtNodeProject 创建 Node 项目。
//
// 对端面板的 Node 项目分「nodejs（package.json 脚本）」「pm2」「general（直接跑入口文件）」三类，
// 这里按源站启动命令自动归类：npm/pnpm/yarn run xxx → nodejs；node xxx.js → general。
//
// 注意（实测宝塔 11.5）：Node 项目创建接口是基于 WebSocket 的（comMod.create 内部
// `self.get._ws.send(...)` + ws_err_exit），且 WebSocket 入口要求浏览器登录会话，
// 面板 API 密钥无法调用 —— 这里仍然发一次请求（新版本可能放开），失败时给出明确指引。
func createBtNodeProject(t *ImportTask, bt *BTClient, ms *MigrateSite, path string, domains []string, port int) error {
	ver := bt.PickNodejsVersion(ms.RuntimeVersion)
	if ver == "" {
		return errors.New("对端面板未安装 Node 版本，请先在对端面板「网站 → Node 项目」中安装 Node 版本后重试")
	}
	script, pkgManager, entry := btNodeStartSpec(ms.StartCommand)
	fields := map[string]any{
		"def_name":       "create",
		"project_name":   btProjectName(ms.Name),
		"project_cwd":    path,
		"project_type":   "nodejs",
		"run_user":       "www",
		"nodejs_version": ver,
		"pkg_manager":    pkgManager,
		"is_power_on":    true,
		"project_ps":     btProjectRemark,
	}
	if entry != "" {
		// 直接以 node 入口文件运行：对端面板归类为 general 项目
		fields["project_type"] = "general"
		fields["project_file"] = entry
	} else {
		fields["project_script"] = script
	}
	if port > 0 {
		fields["port"] = port
	}
	if len(domains) > 0 {
		fields["bind_extranet"] = 1
		fields["domains"] = btDomainBindings(domains)
	} else {
		fields["bind_extranet"] = 0
	}
	// 对端面板新版接口的 env 就是「KEY=VALUE 每行一个」文本，与本机存储格式一致
	if env := strings.TrimSpace(ms.EnvVars); env != "" {
		fields["env"] = env
	}
	urls := btProjectURLs(btModNode, "com", "create", "")
	if _, err := bt.btProjectCall(urls, fields); err != nil {
		t.logf("Node 项目自动创建失败（对端面板 Node 项目创建为 WebSocket 专有接口）: %v", err)
		return fmt.Errorf("对端面板（宝塔）的 Node 项目只能在其面板界面创建（接口为 WebSocket 专有，API 密钥无法调用）。"+
			"请先到对端面板「网站 → Node 项目」新建项目：名称 %s、目录 %s、端口 %d、Node 版本 %s，然后重试迁移以补齐文件",
			btProjectName(ms.Name), path, port, ver)
	}
	t.logf("对端面板 Node 项目 %s 创建完成（Node %s，端口 %d）", fields["project_name"], ver, port)
	return nil
}

// createBtGoProject 创建 Go 项目（走 /project/go/create_project/stype，实测 11.5 可用）：
// 对端面板只负责「运行已编译好的可执行文件」，因此要先确定可执行文件名。
func createBtGoProject(t *ImportTask, bt *BTClient, ms *MigrateSite, path string, domains []string, port int) error {
	exeName, args := btGoExeSpec(ms.StartCommand)
	exePath := ""
	if exeName != "" {
		exePath = filepath.Join(path, exeName)
	}
	// 启动命令指向源码（如 /www/wwwroot/apihot/main.go）或文件不存在时，退化为在项目目录里找可执行文件
	if exePath == "" || !bt.RemoteFileExists(exePath) {
		if cand := bt.FindGoExecutable(path, ms.Name); cand != "" {
			exePath = filepath.Join(path, cand)
			args = ""
		}
	}
	if exePath == "" {
		return fmt.Errorf("项目目录 %s 中没有可执行文件（源站启动命令为 %q）：对端面板的 Go 项目只能运行已编译好的二进制，"+
			"请先在本机编译出可执行文件后重试", path, strings.TrimSpace(ms.StartCommand))
	}
	if !bt.RemoteFileExists(exePath) {
		return fmt.Errorf("对端项目目录中未找到可执行文件 %s，请确认文件已上传并具有执行权限", exePath)
	}
	cmd := exePath
	if args != "" {
		cmd += " " + args
	}
	fields := map[string]any{
		"def_name":      "create_project",
		"project_name":  btProjectName(ms.Name),
		"project_exe":   exePath, // 注意：对端的 sites.path 存的是这个可执行文件路径
		"project_cmd":   cmd,
		"port":          port,
		"run_user":      "www",
		"is_power_on":   1,
		"project_ps":    btProjectRemark,
		"bind_extranet": 0,
		"domains":       []string{},
		"env_file":      "",
		"env_list":      []any{},
	}
	if len(domains) > 0 {
		fields["bind_extranet"] = 1
		fields["domains"] = btDomainBindings(domains)
	}
	if _, err := bt.btProjectCall(btProjectURLs(btModGo, "", "create_project", ""), fields); err != nil {
		return fmt.Errorf("Go 项目创建失败: %w", err)
	}
	t.logf("对端面板 Go 项目 %s 创建完成（可执行文件 %s，端口 %d）", fields["project_name"], filepath.Base(exePath), port)
	btEnvVarsHint(t, ms)
	return nil
}

// createBtPythonProject 创建 Python 项目（走 /project/python/CreateProject/<运行方式>，实测 11.5 可用）。
// 对端面板要求指定已注册的 Python 虚拟环境解释器（python_bin），且项目创建接口不支持绑定域名，
// 因此域名需要迁移完成后到对端面板项目设置里手动绑定（这里显式提示）。
func createBtPythonProject(t *ImportTask, bt *BTClient, ms *MigrateSite, path string, domains []string, port int) error {
	pyBin := bt.PythonBinPath()
	if pyBin == "" {
		return errors.New("对端面板尚未创建 Python 虚拟环境，请先在对端面板「网站 → Python 项目」中创建一个 Python 环境后重试")
	}
	cmd := btPythonCommand(pyBin, ms.StartCommand)
	if cmd == "" {
		return errors.New("Python 项目缺少启动命令，无法在对端面板创建项目")
	}
	framework := strings.TrimSpace(ms.Framework)
	if framework == "" {
		framework = "python"
	}
	fields := map[string]any{
		"def_name":     "CreateProject",
		"pjname":       btProjectName(ms.Name),
		"project_name": btProjectName(ms.Name),
		"path":         path,
		"stype":        "command", // 运行方式：url 段与 data 里都要有（老接口从 url 取）
		"project_cmd":  cmd,
		"python_bin":   pyBin,
		"user":         "www",
		"framework":    framework,
		"auto_run":     true,
		"is_power_on":  true,
		"project_ps":   btProjectRemark,
	}
	if port > 0 {
		fields["port"] = port
	}
	if _, err := bt.btProjectCall(btProjectURLs(btModPython, "", "CreateProject", "command"), fields); err != nil {
		return fmt.Errorf("Python 项目创建失败: %w", err)
	}
	t.logf("对端面板 Python 项目 %s 创建完成（解释器 %s，端口 %d）", fields["pjname"], pyBin, port)
	if len(domains) > 0 {
		t.logf("提示：对端面板 Python 项目接口不支持绑定域名，请迁移完成后到项目设置里手动绑定 %s", strings.Join(domains, "、"))
	}
	return nil
}

// btNodeStartSpec 从启动命令解析 Node 项目参数：
//	"npm run start" / "pnpm start" / "yarn dev" → (脚本名, 包管理器, "")
//	"node app.js" → ("", "npm", "app.js")   —— 直接跑入口文件，对端面板归类为 general 项目
func btNodeStartSpec(startCommand string) (script, pkgManager, entry string) {
	pkgManager = "npm"
	cmd := strings.TrimSpace(startCommand)
	if cmd == "" {
		return "start", pkgManager, ""
	}
	if m := regexp.MustCompile(`^(npm|pnpm|yarn)\s+(?:run\s+)?([^\s]+)`).FindStringSubmatch(cmd); m != nil {
		return m[2], m[1], ""
	}
	if m := regexp.MustCompile(`^node\s+(?:\./)?([^\s]+)`).FindStringSubmatch(cmd); m != nil {
		return "", pkgManager, m[1]
	}
	// 其它形态（如自定义脚本）：交给对端面板按 package.json 的 start 脚本处理
	return "start", pkgManager, ""
}

// btGoExeSpec 从启动命令解析 Go 项目的可执行文件名与启动参数：
//	"./apihot" / "./apihot --config x.yml" → ("apihot", "--config x.yml")
func btGoExeSpec(startCommand string) (exeName, args string) {
	fields := strings.Fields(strings.TrimSpace(startCommand))
	if len(fields) == 0 {
		return "", ""
	}
	first := fields[0]
	if !strings.HasPrefix(first, "./") && !strings.HasPrefix(first, "/") {
		return "", ""
	}
	exeName = filepath.Base(first)
	args = strings.Join(fields[1:], " ")
	return exeName, args
}

// btPythonCommand 把源站启动命令里的 python 可执行文件替换成对端面板指定的虚拟环境解释器：
//	"python app.py" → "<pyBin> app.py"；不含 python 前缀时直接前置解释器。
func btPythonCommand(pyBin, startCommand string) string {
	cmd := strings.TrimSpace(startCommand)
	if cmd == "" {
		return ""
	}
	if m := regexp.MustCompile(`^(?:python3?|uwsgi|gunicorn)\b`).FindStringIndex(cmd); m != nil && m[0] == 0 {
		return pyBin + cmd[m[1]:]
	}
	return pyBin + " " + cmd
}

// btSplitDomains 拆分逗号分隔的域名串（去空、去重，保持原顺序）
func btSplitDomains(domains string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range strings.Split(domains, ",") {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// btPrimaryDomain 取主域名（去掉 :端口 后缀；CreateProxy 等接口要求传纯域名）
func btPrimaryDomain(domains []string, fallback string) string {
	for _, d := range domains {
		if d == "" {
			continue
		}
		if i := strings.LastIndex(d, ":"); i > 0 && !strings.Contains(d[i:], "]") {
			return d[:i]
		}
		return d
	}
	return fallback
}

// btUploadSiteFiles 上传站点文件包到对端面板并解压到目标目录。
// 无文件包时只创建目录（进程型项目也要求项目目录存在，所以不能直接跳过）。
func btUploadSiteFiles(t *ImportTask, bt *BTClient, ms MigrateSite, path, pkgFile string) error {
	bt.CreateRemoteDir(path)
	if !fileExists(pkgFile) {
		t.logf("网站 %s 无文件包，跳过文件上传", ms.Name)
		return nil
	}
	remote := "/www/backup/migrate-" + ms.Name + ".tar.gz"
	t.logf("上传网站 %s 文件包...", ms.Name)
	lastProg := time.Now()
	if err := bt.Upload(pkgFile, remote, func(done, total int64) {
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
		return errors.New("上传文件失败：" + err.Error())
	}
	t.logf("解压网站 %s 文件到站点目录...", ms.Name)
	if err := bt.Unzip(remote, path); err != nil {
		t.logf("解压网站 %s 文件失败: %v", ms.Name, err)
		return errors.New("解压文件失败：" + err.Error())
	}
	btDeleteRemoteFile(bt, remote)
	t.logf("网站 %s 文件恢复完成", ms.Name)
	return nil
}
