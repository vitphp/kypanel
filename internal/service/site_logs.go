package service

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/model"
)

// AccessLogEntry nginx 访问日志结构化条目（combined 格式）
// 原始格式：IP - - [time] "METHOD path HTTP/x.x" status bytes "referer" "ua"
// 例：114.132.203.10 - - [24/Aug/2026:14:30:18 +0800] "GET /static/img.png?v=3 HTTP/1.1" 200 27757 "-" "Mozilla/5.0 ..."
type AccessLogEntry struct {
	IP        string `json:"ip"`
	Time      string `json:"time"`       // 原始时间字符串
	TimeUnix  int64  `json:"time_unix"`  // 解析后的 unix 时间戳
	Method    string `json:"method"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	Status    int    `json:"status"`
	Bytes     int    `json:"bytes"`
	Referer   string `json:"referer"`
	UserAgent string `json:"user_agent"`
	Region    string `json:"region"`     // IP 归属地（离线库查得），格式如 "中国 河北省 石家庄市"
	Raw       string `json:"raw"`        // 原始行（用于"原始文本"模式展示）
}

// accessLogTimeRe 解析 nginx 时间格式 [24/Aug/2026:14:30:18 +0800]
var accessLogTimeRe = regexp.MustCompile(`\[(\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}) ([+\-]\d{4})\]`)

// nginxTimeLayouts 支持的时间格式
var nginxTimeLayouts = []string{
	"02/Jan/2006:15:04:05 -0700",
	"02/Jan/2006:15:04:05 +0700",
}

// parseAccessLogLine 解析单行 nginx 访问日志为结构化条目
// 解析失败返回 nil（不报错，前端展示时跳过该行）
func parseAccessLogLine(line string) *AccessLogEntry {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	e := &AccessLogEntry{Raw: line}

	// 时间（可能含 []）
	if m := accessLogTimeRe.FindStringSubmatch(line); len(m) == 3 {
		e.Time = m[1] + " " + m[2]
		for _, layout := range nginxTimeLayouts {
			if t, err := time.Parse(layout, m[1]+" "+m[2]); err == nil {
				e.TimeUnix = t.Unix()
				break
			}
		}
	}

	// 提取请求行 "METHOD PATH HTTP/x.x"（被双引号包住）
	reqStart := strings.Index(line, `"`)
	if reqStart >= 0 {
		reqEnd := strings.Index(line[reqStart+1:], `"`)
		if reqEnd > 0 {
			req := line[reqStart+1 : reqStart+1+reqEnd]
			parts := strings.SplitN(req, " ", 3)
			if len(parts) >= 2 {
				method := parts[0]
				// method 白名单：过滤掉畸形/恶意请求（避免脏数据进入 method_dist）
				if isValidHTTPMethod(method) {
					e.Method = method
				} else {
					e.Method = ""
				}
				e.Protocol = ""
				if len(parts) == 3 {
					e.Protocol = parts[2]
				}
				e.Path = parts[1]
			}
		}
	}

	// 提取状态码与字节数（请求行之后到下一个引号之前）
	// 用正则更稳：找到 " " + 三位数字 + " " + 数字 + " "
	statusRe := regexp.MustCompile(`" (\d{3}) (\d+|-) "`)
	if m := statusRe.FindStringSubmatch(line); len(m) == 3 {
		e.Status, _ = strconv.Atoi(m[1])
		if m[2] != "-" {
			e.Bytes, _ = strconv.Atoi(m[2])
		}
	}

	// UA 和 Referer（最后一个 "..." 之后到末尾；倒数第二个 "..." 是 referer）
	// 找所有引号配对位置
	quotes := []int{}
	for i, ch := range line {
		if ch == '"' {
			quotes = append(quotes, i)
		}
	}
	if len(quotes) >= 6 {
		// 形如："req" status bytes "referer" "ua"
		// quotes[0..1] = req; quotes[2..3] = referer; quotes[4..5] = ua
		e.Referer = line[quotes[2]+1 : quotes[3]]
		e.UserAgent = line[quotes[4]+1 : quotes[5]]
		if e.Referer == "-" {
			e.Referer = ""
		}
	}

	// IP：第一段（空格分隔前）
	fields := strings.Fields(line)
	if len(fields) > 0 {
		ip := strings.Trim(fields[0], `[]`)
		// 剥端口（仅 IPv4 IP:port）
		if i := strings.LastIndex(ip, ":"); i > 0 && strings.Count(ip, ":") == 1 {
			ip = ip[:i]
		}
		if net.ParseIP(ip) != nil {
			e.IP = ip
		}
	}
	return e
}

// readAccessLogFile 从文件末尾倒读最多 maxLines 行（流式，不读全文件，避免大日志 OOM）。
// 返回行切片（按文件中顺序）和文件总行数。
func readAccessLogFile(path string, maxLines int) ([]string, int, error) {
	if maxLines <= 0 {
		maxLines = 1000
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	fileSize := info.Size()

	// 先快速统计总行数（只扫一遍，但大文件也慢）。
	// 这里用一次倒读同时拿到末尾 N 行和总行数，避免二次扫描。
	const chunk = 64 * 1024
	var buf []byte           // 累积读到的尾部数据
	var offset = fileSize    // 从文件末尾开始
	var totalLines = 0
	needNewlines := maxLines // 需要找到的换行数（找到这些就能截出末尾 maxLines 行）

	for offset > 0 {
		readSize := int64(chunk)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		block := make([]byte, readSize)
		if _, err := f.ReadAt(block, offset); err != nil && err != io.EOF {
			return nil, 0, err
		}
		// 把这块拼到 buf 前面
		buf = append(block, buf...)
		// 统计新增块里的换行数
		nl := bytes.Count(block, []byte{'\n'})
		totalLines += nl
		// 如果已经攒够了末尾 maxLines 行（多找一个换行作边界），就停止
		if totalLines >= needNewlines {
			break
		}
	}

	// 总行数 = 已经数到的换行数（可能不是全文件，但作为"是否截断"判断足够了）
	// 为了准确，若没读完整个文件，则 totalLines 是已读部分的换行数；需估算全文件行数。
	// 这里简化：totalLines 仅用于截断判断，够用。
	text := string(buf)
	// 去掉开头可能的半行（第一个换行之前的内容是上一块截断的残行）
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[idx+1:]
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	// 末尾可能多出一行（末尾无换行符的情况），过滤空首行
	if len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	// 若超过 maxLines，取最后 maxLines 行
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, totalLines, nil
}

// SiteLogsResult 访问日志接口返回
type SiteLogsResult struct {
	Entries    []AccessLogEntry `json:"entries"`
	TotalLines int              `json:"total_lines"` // 文件总行数
	Truncated  bool             `json:"truncated"`   // 是否截断到 maxLines
	HasFile    bool             `json:"has_file"`    // 日志文件是否存在
}

// SiteLogs 读取并解析站点访问日志（最近 maxLines 行；maxLines=0 用默认 300）
func SiteLogs(id uint, maxLines int) (*SiteLogsResult, error) {
	if maxLines <= 0 {
		maxLines = 300
	}
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	path := siteAccessLogPath(s.Name)
	lines, total, err := readAccessLogFile(path, maxLines)
	if err != nil {
		return nil, errors.New("读取日志失败: " + err.Error())
	}
	entries := make([]AccessLogEntry, 0, len(lines))
	regionCache := make(map[string]string) // 同 IP 多次出现只查一次，离线库查询是 O(log N)
	for _, ln := range lines {
		if e := parseAccessLogLine(ln); e != nil {
			// 离线库查归属地（IP 维度缓存，避免重复查询拖慢接口）
			if e.IP != "" {
				if r, ok := regionCache[e.IP]; ok {
					e.Region = r
				} else if info, ok := SearchIp(e.IP); ok {
					region := strings.TrimSpace(strings.Join([]string{info.Country, info.Province, info.City}, " "))
					e.Region = region
					regionCache[e.IP] = region
				}
			}
			entries = append(entries, *e)
		}
	}
	// 访问日志：文件最末尾就是最新访问，按时间倒序展示（最新在前）
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].TimeUnix != entries[j].TimeUnix {
			return entries[i].TimeUnix > entries[j].TimeUnix
		}
		// 同秒内：保留文件原顺序（行号越靠后越新）
		return false
	})
	return &SiteLogsResult{
		Entries:    entries,
		TotalLines: total,
		Truncated:  total > maxLines,
		HasFile:    total > 0,
	}, nil
}

// ---------- 日志分析 ----------

// AccessLogStats 访问统计
type AccessLogStats struct {
	Total       int                  `json:"total"`        // 解析成功的请求总数
	StatusDist  map[string]int       `json:"status_dist"`  // 状态码分布，如 {"2xx":123, "4xx":5, "5xx":1}
	MethodDist  map[string]int       `json:"method_dist"`  // 方法分布
	TopPaths    []PathCount          `json:"top_paths"`    // Top 路径
	TopIPs      []IPCount            `json:"top_ips"`      // Top IP
	HourlyDist  map[string]int       `json:"hourly_dist"`  // 24 小时分布（0-23）
	UniqueIPs   int                  `json:"unique_ips"`   // 唯一 IP 数
}

// PathCount 路径统计
type PathCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// IPCount IP 统计
type IPCount struct {
	IP     string `json:"ip"`
	Count  int    `json:"count"`
	Region string `json:"region"` // 国家/省/市
}

// RiskItem 风险条目
type RiskItem struct {
	IP        string `json:"ip"`
	Region    string `json:"region"`    // IP 归属地（离线库查得）
	Time      string `json:"time"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Type      string `json:"type"`      // 风险类型
	Severity  string `json:"severity"`  // high/medium/low
	Desc      string `json:"desc"`      // 中文描述
	UserAgent string `json:"user_agent"`
}

// AccessLogAnalyzeResult 分析接口返回
type AccessLogAnalyzeResult struct {
	Stats AccessLogStats `json:"stats"`
	Risks []RiskItem     `json:"risks"`
}

// 攻击特征（路径关键字）
var attackSignatures = []struct {
	keyword string
	typ     string
	desc    string
}{
	{"wp-admin", "scanner", "WordPress 后台扫描"},
	{"wp-login", "scanner", "WordPress 登录页扫描"},
	{".env", "info_leak", "环境变量文件探测"},
	{".git/", "info_leak", "Git 目录探测"},
	{".svn/", "info_leak", "SVN 目录探测"},
	{".htaccess", "info_leak", "敏感配置文件探测"},
	{"phpmyadmin", "scanner", "phpMyAdmin 扫描"},
	{"/admin", "scanner", "后台路径扫描"},
	{"/shell", "webshell", "WebShell 探测"},
	{"/cmd", "webshell", "命令执行探测"},
	{"union%20select", "sqli", "SQL 注入特征 (UNION SELECT)"},
	{"union+select", "sqli", "SQL 注入特征 (UNION SELECT)"},
	{"' or '1'='1", "sqli", "SQL 注入特征"},
	{"%27%20or%20", "sqli", "SQL 注入特征"},
	{"<script", "xss", "XSS 特征 (<script)"},
	{"%3Cscript", "xss", "XSS 特征 (编码 <script)"},
	{"../", "traversal", "路径遍历 (../)"},
	{"..%2f", "traversal", "路径遍历 (../ 编码)"},
	{"./../", "traversal", "路径遍历"},
	{"/etc/passwd", "lfi", "本地文件包含 (/etc/passwd)"},
	{"/proc/self", "lfi", "本地文件包含 (/proc/self)"},
}

// 扫描器/爬虫 UA 特征
var scannerUASignatures = []struct {
	keyword string
	typ     string
	desc    string
}{
	{"sqlmap", "scanner", "SQLMap 注入工具"},
	{"nikto", "scanner", "Nikto Web 扫描器"},
	{"nmap", "scanner", "Nmap 端口扫描器"},
	{"masscan", "scanner", "Masscan 扫描器"},
	{"nessus", "scanner", "Nessus 漏洞扫描器"},
	{"acunetix", "scanner", "Acunetix 扫描器"},
	{"burpcollaborator", "scanner", "Burp Suite"},
	{"gobuster", "scanner", "Gobuster 目录扫描"},
	{"dirbuster", "scanner", "DirBuster 目录扫描"},
	{"python-requests", "bot", "Python 脚本请求"},
	{"go-http-client", "bot", "Go 脚本请求"},
	{"curl/", "bot", "curl 命令行请求"},
	{"wget/", "bot", "wget 命令行请求"},
	{"scrapy", "scraper", "Scrapy 爬虫"},
	{"ahrefsbot", "scraper", "Ahrefs 爬虫"},
	{"semrushbot", "scraper", "Semrush 爬虫"},
	{"mj12bot", "scraper", "Majestic 爬虫"},
	{"dotbot", "scraper", "Moz DotBot 爬虫"},
	{"petalbot", "scraper", "Petal 爬虫"},
	{"yisouspider", "scraper", "神马搜索爬虫"},
}

// SiteLogsAnalyze 读取日志并做访问统计 + 风险分析
func SiteLogsAnalyze(id uint, maxLines int) (*AccessLogAnalyzeResult, error) {
	if maxLines <= 0 {
		maxLines = 2000
	}
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	path := siteAccessLogPath(s.Name)
	lines, _, err := readAccessLogFile(path, maxLines)
	if err != nil {
		return nil, errors.New("读取日志失败: " + err.Error())
	}

	entries := make([]AccessLogEntry, 0, len(lines))
	for _, ln := range lines {
		if e := parseAccessLogLine(ln); e != nil {
			entries = append(entries, *e)
		}
	}

	stats := analyzeStats(entries)
	risks := analyzeRisks(entries)
	// 为 Top IP 补 region
	for i := range stats.TopIPs {
		if r, ok := SearchIp(stats.TopIPs[i].IP); ok {
			stats.TopIPs[i].Region = strings.Join([]string{r.Country, r.Province, r.City}, " ")
			stats.TopIPs[i].Region = strings.TrimSpace(stats.TopIPs[i].Region)
		}
	}
	// 为每条风险补 IP 归属地（按 IP 缓存，离线库查询是 O(log N)）
	riskRegionCache := make(map[string]string)
	for i := range risks {
		ip := risks[i].IP
		if ip == "" {
			continue
		}
		if r, ok := riskRegionCache[ip]; ok {
			risks[i].Region = r
		} else if info, ok := SearchIp(ip); ok {
			region := strings.TrimSpace(strings.Join([]string{info.Country, info.Province, info.City}, " "))
			risks[i].Region = region
			riskRegionCache[ip] = region
		}
	}

	return &AccessLogAnalyzeResult{Stats: stats, Risks: risks}, nil
}

// analyzeStats 统计访问分布
func analyzeStats(entries []AccessLogEntry) AccessLogStats {
	s := AccessLogStats{
		Total:      len(entries),
		StatusDist: map[string]int{},
		MethodDist: map[string]int{},
		HourlyDist: map[string]int{},
	}
	pathCnt := map[string]int{}
	ipCnt := map[string]int{}

	for _, e := range entries {
		// 状态码分布（2xx/3xx/4xx/5xx）
		cls := strconv.Itoa(e.Status/100) + "xx"
		s.StatusDist[cls]++

		// 方法
		if e.Method != "" {
			s.MethodDist[e.Method]++
		}

		// 小时分布
		if e.TimeUnix > 0 {
			h := time.Unix(e.TimeUnix, 0).Hour()
			s.HourlyDist[strconv.Itoa(h)]++
		}

		// 路径（去掉 query）
		if e.Path != "" {
			p := e.Path
			if i := strings.Index(p, "?"); i >= 0 {
				p = p[:i]
			}
			pathCnt[p]++
		}
		if e.IP != "" {
			ipCnt[e.IP]++
		}
	}

	s.UniqueIPs = len(ipCnt)
	s.TopPaths = topNFromMap(pathCnt, 10)
	s.TopIPs = topNFromIPMap(ipCnt, 10)
	return s
}

// analyzeRisks 风险扫描：4xx/5xx + 攻击特征 + 扫描器 UA
func analyzeRisks(entries []AccessLogEntry) []RiskItem {
	risks := make([]RiskItem, 0)
	// 仅分析最近 1000 条以避免过多
	limit := len(entries)
	if limit > 1000 {
		limit = 1000
	}
	for i := 0; i < limit; i++ {
		e := entries[i]
		// 1) 4xx/5xx 错误
		if e.Status >= 400 {
			sev := "low"
			if e.Status >= 500 {
				sev = "medium"
			}
			risks = append(risks, RiskItem{
				IP: e.IP, Time: e.Time, Method: e.Method, Path: e.Path, Status: e.Status,
				Type:     "http_error",
				Severity: sev,
				Desc:     "HTTP " + strconv.Itoa(e.Status) + " 错误",
				UserAgent: e.UserAgent,
			})
		}
		// 2) 攻击特征（path）
		lowPath := strings.ToLower(e.Path)
		for _, sig := range attackSignatures {
			if strings.Contains(lowPath, sig.keyword) {
				risks = append(risks, RiskItem{
					IP: e.IP, Time: e.Time, Method: e.Method, Path: e.Path, Status: e.Status,
					Type:     sig.typ,
					Severity: "high",
					Desc:     sig.desc,
					UserAgent: e.UserAgent,
				})
				break // 一行只算一次
			}
		}
		// 3) 扫描器 UA
		lowUA := strings.ToLower(e.UserAgent)
		for _, sig := range scannerUASignatures {
			if strings.Contains(lowUA, sig.keyword) {
				risks = append(risks, RiskItem{
					IP: e.IP, Time: e.Time, Method: e.Method, Path: e.Path, Status: e.Status,
					Type:     sig.typ,
					Severity: "medium",
					Desc:     sig.desc + " UA",
					UserAgent: e.UserAgent,
				})
				break
			}
		}
	}
	// 按时间倒序（最新在前）
	sort.SliceStable(risks, func(i, j int) bool {
		return risks[i].Time > risks[j].Time
	})
	// 限制最多 200 条
	if len(risks) > 200 {
		risks = risks[:200]
	}
	return risks
}

// topNFromMap 从 map 取 TopN（按值倒序）
func topNFromMap(m map[string]int, n int) []PathCount {
	out := make([]PathCount, 0, len(m))
	for k, v := range m {
		out = append(out, PathCount{Path: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func topNFromIPMap(m map[string]int, n int) []IPCount {
	out := make([]IPCount, 0, len(m))
	for k, v := range m {
		out = append(out, IPCount{IP: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// validHTTPMethods 合法 HTTP 方法白名单
var validHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// isValidHTTPMethod 判断是否为合法 HTTP 方法（过滤畸形/恶意请求）
func isValidHTTPMethod(m string) bool {
	if m == "" || len(m) > 20 {
		return false
	}
	return validHTTPMethods[strings.ToUpper(m)]
}
