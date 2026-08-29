package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// expandSiteTargets 把请求里的站点名/根目录（逗号分隔）展开成数组。
// nameCsv="*" 表示全部站点，rootCsv 必须相应为空；返回 (names, roots, nil)。
func expandSiteTargets(nameCsv, rootCsv string) ([]string, []string, error) {
	nameCsv = strings.TrimSpace(nameCsv)
	rootCsv = strings.TrimSpace(rootCsv)
	if nameCsv == "" {
		return nil, nil, errors.New("请选择有效站点")
	}
	if nameCsv == "*" {
		// 全部站点：从 DB 读出所有站点
		sites := ListSites()
		names := make([]string, 0, len(sites))
		roots := make([]string, 0, len(sites))
		for _, s := range sites {
			if s.Name == "" || s.Root == "" {
				continue
			}
			names = append(names, s.Name)
			roots = append(roots, filepath.Clean(s.Root))
		}
		if len(names) == 0 {
			return nil, nil, errors.New("当前没有任何站点可备份")
		}
		return names, roots, nil
	}
	names := strings.Split(nameCsv, ",")
	var roots []string
	if rootCsv != "" {
		roots = strings.Split(rootCsv, ",")
	} else {
		// 没传 root 列表时尝试从 ListSites 反查
		all := ListSites()
		idx := map[string]string{}
		for _, s := range all {
			idx[s.Name] = s.Root
		}
		for _, n := range names {
			roots = append(roots, filepath.Clean(idx[n]))
		}
	}
	if len(roots) != len(names) {
		return nil, nil, errors.New("站点数量与根目录数量不匹配")
	}
	return names, roots, nil
}

// expandDatabaseTargets 把请求里的数据库名（逗号分隔）展开成数组。
// nameCsv="*" 表示全部数据库。
func expandDatabaseTargets(nameCsv string) ([]string, error) {
	nameCsv = strings.TrimSpace(nameCsv)
	if nameCsv == "" {
		return nil, errors.New("请选择数据库")
	}
	if nameCsv == "*" {
		// 全部数据库
		dbs, err := ListDatabases()
		if err != nil {
			return nil, fmt.Errorf("列出所有数据库失败: %w", err)
		}
		var names []string
		for _, d := range dbs {
			if d.Name != "" {
				names = append(names, d.Name)
			}
		}
		if len(names) == 0 {
			return nil, errors.New("当前没有任何数据库可备份")
		}
		return names, nil
	}
	names := strings.Split(nameCsv, ",")
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

// joinShellBlocks 把多个独立 shell 命令块用 `&&` 串起来（任一失败则后续不执行）
func joinShellBlocks(blocks []string) string {
	return strings.Join(blocks, " && ")
}

// appendRemoteUploadHook 若 target_type=remote，在命令末尾追加"上传最新备份到远程存储"的钩子。
// 当前实现：仅追加一行 echo 提示 + 把任务 ID/存储名记录到 cron Command 字符串里，便于后续
// 由 wrapper 脚本（cron-wrapper.sh）识别并调用 /cron/backup-upload 端点。
// 实际远程传输逻辑（基于 task_id 拉取 cron 配置、调用 S3/OSS SDK 上传）将在下一次迭代补齐。
func appendRemoteUploadHook(baseCmd, targetType, targetName string, remoteKeep, localKeep int) string {
	if targetType != "remote" || strings.TrimSpace(targetName) == "" {
		return baseCmd
	}
	if remoteKeep <= 0 {
		remoteKeep = localKeep
		if remoteKeep <= 0 {
			remoteKeep = 7
		}
	}
	// 用一个哨兵注释 + JSON 行让 wrapper 识别上传目标（后续迭代实现实际 HTTP 调用）
	hook := fmt.Sprintf(
		"&& echo '__KYPANEL_REMOTE_UPLOAD__ storage=%s remote_keep=%d'",
		shellQuoteSafe(targetName), remoteKeep)
	return baseCmd + " " + hook
}

// shellQuoteSafe 用单引号包裹字符串（替换内部的单引号为 '\''
func shellQuoteSafe(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\\''`) + "'"
}

// CronTemplate 任务模板元数据（前端用）
type CronTemplate struct {
	Key         string   `json:"key"`          // 模板 key，前端标识
	Label       string   `json:"label"`        // 显示名
	Group       string   `json:"group"`        // 分组：common / backup / maintenance / network
	Icon        string   `json:"icon"`         // Element Plus 图标名
	Description string   `json:"description"`  // 模板说明
	Needs       []string `json:"needs"`        // 需要的附加字段：site / database / dir / url / script / days / format
	SuggestSpec string   `json:"suggest_spec"` // 建议周期，如 0 3 * * *
}

// CronTemplateReq 根据模板生成 cron 命令的请求
type CronTemplateReq struct {
	Template string `json:"template"`          // 模板 key
	SiteName string `json:"site_name"`         // 备份网站：站点名（多个用英文逗号分隔；"*" 代表全部）
	SiteRoot string `json:"site_root"`         // 备份网站：站点根目录（多个用英文逗号分隔，与 SiteName 一一对应）
	Database string `json:"database"`          // 备份数据库：库名（多个用英文逗号分隔；"*" 代表全部）
	Dir      string `json:"dir"`               // 备份目录：路径
	URL      string `json:"url"`               // 访问 URL：地址
	Script   string `json:"script"`            // Shell 脚本：原始命令
	Method   string `json:"method"`            // 访问 URL 方法：GET / POST
	Days     int    `json:"days"`              // 保留天数 / 日志清理天数
	Keep     int    `json:"keep"`              // 增量备份保留份数
	Format   string `json:"format"`            // 压缩格式 zip/tar.gz
	Extra    string `json:"extra"`             // 其它附加参数（自由文本）
	// 备份目标（仅 backup_site / backup_db / backup_db_incremental 使用）
	TargetType string `json:"target_type"`    // local / remote
	TargetName string `json:"target_name"`    // remote=存储名称
	RemoteKeep int    `json:"remote_keep"`    // 远程保留份数；= 0 表示用本地 Keep
}

// CronTemplates 内置任务模板
var CronTemplates = []CronTemplate{
	{
		Key: "shell", Group: "common", Icon: "Document",
		Label: "Shell 脚本", Description: "直接执行一段 Shell 命令（高级用户）",
		Needs: []string{"script"}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "python", Group: "common", Icon: "MagicStick",
		Label: "Python 脚本", Description: "执行一段 Python 脚本",
		Needs: []string{"script"}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "backup_site", Group: "backup", Icon: "FolderOpened",
		Label: "备份网站", Description: "打包压缩指定站点根目录到 /www/backup/site，保留最近 N 份自动清理",
		Needs: []string{"site", "keep"}, SuggestSpec: "0 2 * * *",
	},
	{
		Key: "backup_db", Group: "backup", Icon: "Coin",
		Label: "备份数据库", Description: "mysqldump 备份指定数据库到 /www/backup/database，保留最近 N 份自动清理",
		Needs: []string{"database", "keep"}, SuggestSpec: "0 1 * * *",
	},
	{
		Key: "backup_db_incremental", Group: "backup", Icon: "Files",
		Label: "数据库增量备份", Description: "每天备份一份到 /www/backup/database_incremental/<db>，保留最近 N 份自动清理",
		Needs: []string{"database", "keep"}, SuggestSpec: "0 1 * * *",
	},
	{
		Key: "backup_dir", Group: "backup", Icon: "Box",
		Label: "备份目录", Description: "打包压缩任意目录到 /www/backup/dir，保留最近 N 份自动清理",
		Needs: []string{"dir", "format", "keep"}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "cut_log", Group: "maintenance", Icon: "Document",
		Label: "网站日志切割", Description: "切割 Nginx 站点访问日志，并 reload",
		Needs: []string{"site"}, SuggestSpec: "0 0 * * *",
	},
	{
		Key: "clear_log", Group: "maintenance", Icon: "Delete",
		Label: "定时清理日志", Description: "删除指定目录下 N 天前的日志文件",
		Needs: []string{"dir", "days"}, SuggestSpec: "0 4 * * 0",
	},
	{
		Key: "free_mem", Group: "maintenance", Icon: "Cpu",
		Label: "释放内存", Description: "释放 Linux 缓存内存（不影响业务）",
		Needs: []string{}, SuggestSpec: "0 4 * * *",
	},
	{
		Key: "sync_time", Group: "maintenance", Icon: "Clock",
		Label: "同步时间", Description: "与 NTP 服务器同步时间",
		Needs: []string{}, SuggestSpec: "0 1 * * *",
	},
	{
		Key: "scan_trojan", Group: "maintenance", Icon: "WarningFilled",
		Label: "木马查杀", Description: "扫描最近修改的 PHP/脚本文件中的可疑函数",
		Needs: []string{"dir"}, SuggestSpec: "0 5 * * *",
	},
	{
		Key: "http_get", Group: "network", Icon: "Link",
		Label: "访问 URL - GET", Description: "定时通过 GET 访问一个 URL",
		Needs: []string{"url"}, SuggestSpec: "*/10 * * * *",
	},
	{
		Key: "http_post", Group: "network", Icon: "Position",
		Label: "访问 URL - POST", Description: "定时通过 POST 访问一个 URL",
		Needs: []string{"url"}, SuggestSpec: "*/10 * * * *",
	},
	{
		Key: "nginx_reload", Group: "maintenance", Icon: "Refresh",
		Label: "重新载入 Nginx", Description: "reload Nginx 配置",
		Needs: []string{}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "nginx_restart", Group: "maintenance", Icon: "Refresh",
		Label: "重启 Nginx", Description: "重启 Nginx 服务",
		Needs: []string{}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "apache_reload", Group: "maintenance", Icon: "Refresh",
		Label: "重新载入 Apache", Description: "reload Apache 配置",
		Needs: []string{}, SuggestSpec: "0 3 * * *",
	},
	{
		Key: "apache_restart", Group: "maintenance", Icon: "Refresh",
		Label: "重启 Apache", Description: "重启 Apache 服务",
		Needs: []string{}, SuggestSpec: "0 3 * * *",
	},
}

// GetCronTemplates 返回所有模板
func GetCronTemplates() []CronTemplate {
	return CronTemplates
}

var dirOKRe = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)

// GenerateCronFromTemplate 根据模板生成可执行的 shell 命令
func GenerateCronFromTemplate(req CronTemplateReq) (string, string, error) {
	dateStamp := "$(date +%Y%m%d_%H%M%S)"
	switch req.Template {
	case "shell":
		if strings.TrimSpace(req.Script) == "" {
			return "", "", errors.New("请填写 Shell 命令")
		}
		return "shell", req.Script, nil

	case "python":
		if strings.TrimSpace(req.Script) == "" {
			return "", "", errors.New("请填写 Python 脚本内容")
		}
		script := strings.TrimRight(req.Script, "\n") + "\n"
		return "python", fmt.Sprintf("python3 -c %s", shellQuote(script)), nil

	case "backup_site":
		keep := req.Keep
		if keep <= 0 {
			keep = 7
		}
		backupDir := "/www/backup/site"
		// 解析多个站点（逗号分隔；"*" 表示全部）
		siteNames, siteRoots, err := expandSiteTargets(req.SiteName, req.SiteRoot)
		if err != nil {
			return "", "", err
		}
		// 多个站点 → 每个站点单独一个 tar 包，并各自清理旧的
		var blocks []string
		for i, name := range siteNames {
			root := siteRoots[i]
			if !siteNameRe.MatchString(name) || !strings.HasPrefix(root, "/") {
				continue
			}
			block := fmt.Sprintf(
				"mkdir -p %s && tar -czf %s/%s_%s.tar.gz -C %s . && "+
					"cd %s && ls -1tr %s_%s_*.tar.gz 2>/dev/null | head -n -%d | xargs -r rm --",
				backupDir, backupDir, name, dateStamp, root,
				backupDir, name, dateStamp, keep)
			blocks = append(blocks, block)
		}
		if len(blocks) == 0 {
			return "", "", errors.New("请选择有效站点")
		}
		// 远程存储时附加上传命令（由 wrapper 解释执行）
		cmd := joinShellBlocks(blocks)
		cmd = appendRemoteUploadHook(cmd, req.TargetType, req.TargetName, req.RemoteKeep, req.Keep)
		return "shell", cmd, nil

	case "backup_db":
		keep := req.Keep
		if keep <= 0 {
			keep = 7
		}
		backupDir := "/www/backup/database"
		dbNames, err := expandDatabaseTargets(strings.TrimSpace(req.Database))
		if err != nil {
			return "", "", err
		}
		var blocks []string
		for _, db := range dbNames {
			if !identRe.MatchString(db) {
				continue
			}
			block := fmt.Sprintf(
				"mkdir -p %s && "+mysqlBaseArgs()+" mysqldump --default-character-set=utf8mb4 %s | gzip > %s/%s_%s.sql.gz && "+
					"cd %s && ls -1tr %s_%s_*.sql.gz 2>/dev/null | head -n -%d | xargs -r rm --",
				backupDir, db, backupDir, db, dateStamp,
				backupDir, db, dateStamp, keep)
			blocks = append(blocks, block)
		}
		if len(blocks) == 0 {
			return "", "", errors.New("请选择有效数据库")
		}
		cmd := joinShellBlocks(blocks)
		cmd = appendRemoteUploadHook(cmd, req.TargetType, req.TargetName, req.RemoteKeep, req.Keep)
		return "shell", cmd, nil

	case "backup_db_incremental":
		db := strings.TrimSpace(req.Database)
		if !identRe.MatchString(db) {
			return "", "", errors.New("数据库名无效")
		}
		keep := req.Keep
		if keep <= 0 {
			keep = 7
		}
		backupDir := "/www/backup/database_incremental/" + db
		cmd := fmt.Sprintf(
			"mkdir -p %s && "+mysqlBaseArgs()+" mysqldump --default-character-set=utf8mb4 %s | gzip > %s/%s_%s.sql.gz && "+
				"cd %s && ls -1tr %s_%s_*.sql.gz 2>/dev/null | head -n -%d | xargs -r rm --",
			backupDir, db, backupDir, db, dateStamp,
			backupDir, db, dateStamp, keep)
		return "shell", cmd, nil

	case "backup_dir":
		dir := filepath.Clean(req.Dir)
		if dir == "" || dir == "/" || dir == "." || !strings.HasPrefix(dir, "/") {
			return "", "", errors.New("目录路径无效")
		}
		if !dirOKRe.MatchString(dir) {
			return "", "", errors.New("目录路径包含非法字符")
		}
		format := req.Format
		if format != "zip" {
			format = "tar.gz"
		}
		keep := req.Keep
		if keep <= 0 {
			keep = 7
		}
		baseName := strings.ReplaceAll(strings.TrimPrefix(dir, "/"), "/", "_")
		backupDir := "/www/backup/dir"
		// 按扩展名匹配，避免误删（*.tar.gz / *.zip 分别处理）
		var cmd string
		if format == "zip" {
			cmd = fmt.Sprintf(
				"mkdir -p %s && zip -qr %s/%s_%s.zip %s && "+
					"cd %s && ls -1tr %s_%s_*.zip 2>/dev/null | head -n -%d | xargs -r rm --",
				backupDir, backupDir, baseName, dateStamp, dir,
				backupDir, baseName, dateStamp, keep)
		} else {
			cmd = fmt.Sprintf(
				"mkdir -p %s && tar -czf %s/%s_%s.tar.gz %s && "+
					"cd %s && ls -1tr %s_%s_*.tar.gz 2>/dev/null | head -n -%d | xargs -r rm --",
				backupDir, backupDir, baseName, dateStamp, dir,
				backupDir, baseName, dateStamp, keep)
		}
		return "shell", cmd, nil

	case "cut_log":
		if !siteNameRe.MatchString(req.SiteName) {
			return "", "", errors.New("请选择有效站点")
		}
		logFile := siteAccessLogPath(req.SiteName)
		backup := fmt.Sprintf("%s.%s", logFile, dateStamp)
		var reloadCmd string
		if WebServerType() == webApache {
			reloadCmd = "apachectl graceful 2>/dev/null || apache2ctl graceful 2>/dev/null || systemctl reload apache2 2>/dev/null || true"
		} else {
			reloadCmd = "/etc/init.d/nginx reload 2>/dev/null || nginx -s reload 2>/dev/null || systemctl reload nginx 2>/dev/null || true"
		}
		cmd := fmt.Sprintf("[ -f %s ] && mv %s %s && touch %s && %s || true",
			logFile, logFile, backup, logFile, reloadCmd)
		return "shell", cmd, nil

	case "clear_log":
		dir := filepath.Clean(req.Dir)
		if !strings.HasPrefix(dir, "/") {
			return "", "", errors.New("日志目录无效")
		}
		days := req.Days
		if days <= 0 {
			days = 30
		}
		cmd := fmt.Sprintf("find %s -type f -name '*.log' -mtime +%d -delete", dir, days)
		return "shell", cmd, nil

	case "free_mem":
		return "shell", "sync && echo 1 > /proc/sys/vm/drop_caches && echo 2 > /proc/sys/vm/drop_caches && echo 3 > /proc/sys/vm/drop_caches", nil

	case "sync_time":
		// 优先 chronyc，失败则 ntpdate
		return "shell", "chronyc makestep 2>/dev/null; /usr/sbin/ntpdate -u cn.pool.ntp.org 2>/dev/null || ntpdate -u cn.pool.ntp.org 2>/dev/null || true", nil

	case "scan_trojan":
		dir := filepath.Clean(req.Dir)
		if !strings.HasPrefix(dir, "/") {
			return "", "", errors.New("扫描目录无效")
		}
		// 扫描最近 7 天修改的脚本/网页文件，包含可疑函数
		cmd := fmt.Sprintf(
			"find %s -type f \\( -name '*.php' -o -name '*.jsp' -o -name '*.asp' \\) -mtime -7 -print0 | xargs -0 grep -l 'eval(\\|base64_decode(\\|passthru(\\|system(\\|shell_exec(' 2>/dev/null | tee /tmp/suspicious_files_$(date +%%Y%%m%%d).log",
			dir)
		return "shell", cmd, nil

	case "http_get":
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return "", "", errors.New("URL 必须以 http:// 或 https:// 开头")
		}
		return "shell", fmt.Sprintf("curl -fsS -m 30 %s >/dev/null 2>&1", shellQuote(req.URL)), nil

	case "http_post":
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return "", "", errors.New("URL 必须以 http:// 或 https:// 开头")
		}
		return "shell", fmt.Sprintf("curl -fsS -m 30 -X POST %s >/dev/null 2>&1", shellQuote(req.URL)), nil

	case "nginx_reload":
		return "shell", "/etc/init.d/nginx reload 2>/dev/null || nginx -s reload 2>/dev/null || systemctl reload nginx 2>/dev/null", nil

	case "nginx_restart":
		return "shell", "/etc/init.d/nginx restart 2>/dev/null || systemctl restart nginx 2>/dev/null", nil

	case "apache_reload":
		return "shell", "apachectl graceful 2>/dev/null || apache2ctl graceful 2>/dev/null || systemctl reload apache2 2>/dev/null || systemctl reload httpd 2>/dev/null", nil

	case "apache_restart":
		return "shell", "apachectl restart 2>/dev/null || apache2ctl restart 2>/dev/null || systemctl restart apache2 2>/dev/null || systemctl restart httpd 2>/dev/null", nil

	default:
		return "", "", errors.New("未知任务模板: " + req.Template)
	}
}