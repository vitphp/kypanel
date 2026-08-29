package service

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"kypanel/internal/model"
)

// nginx 默认 access 日志格式：
//
//	$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
// 示例：
//	1.2.3.4 - - [21/Aug/2026:13:24:55 +0800] "GET /index.php HTTP/1.1" 200 578 "-" "Mozilla/5.0 ..."
var siteStatLogRegex = regexp.MustCompile(
	`^(\S+)\s+` +
		`\S+\s+\S+\s+` +
		`\[([^\]]+)\]\s+` +
		`"(\S+)\s+(\S+)\s+\S+"\s+` +
		`(\d+)\s+(\d+|-)\s+` +
		`"([^"]*)"\s+` +
		`"([^"]*)"`,
)

// siteStatImporters 单站点日志导入游标（断点续传）
type siteStatImporter struct {
	mu       sync.Mutex
	site     model.Site
	logPath  string
	offset   int64 // 上一次读到文件的位置
	stop     chan struct{}
	running  bool
	lastErr  string
}

var (
	siteStatMu  sync.RWMutex
	siteStatMap = map[uint]*siteStatImporter{} // site_id → importer
)

// maxStatVisitRetention 访问日志最长保留天数（自定义时间范围 90 天以内都能查到）
const maxStatVisitRetention = 92 * 24 * time.Hour

// StartSiteStatImports 启动所有站点的访问日志 importer + 后台清理任务
// 安全做法：DB 为 nil 时直接跳过，不影响其它功能
func StartSiteStatImports() {
	go siteStatRetentionLoop()
	if model.DB == nil {
		return
	}
	var sites []model.Site
	if err := model.DB.Find(&sites).Error; err == nil {
		for _, s := range sites {
			StartSiteStatImport(s.ID)
		}
	}
}

// StartSiteStatImport 启动指定站点的访问日志实时导入；多次调用幂等
func StartSiteStatImport(siteID uint) {
	if model.DB == nil {
		return
	}
	siteStatMu.Lock()
	imp, ok := siteStatMap[siteID]
	if ok && imp != nil && imp.running {
		siteStatMu.Unlock()
		return
	}
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		siteStatMu.Unlock()
		return
	}
	imp = &siteStatImporter{
		site:    site,
		logPath: siteAccessLogPath(site.Name),
		stop:    make(chan struct{}),
	}
	siteStatMap[siteID] = imp
	siteStatMu.Unlock()

	imp.running = true
	go imp.run()
}

// StopSiteStatImport 停止指定站点的导入器（删除站点时调用）
func StopSiteStatImport(siteID uint) {
	siteStatMu.Lock()
	imp := siteStatMap[siteID]
	delete(siteStatMap, siteID)
	siteStatMu.Unlock()
	if imp != nil {
		close(imp.stop)
	}
}

// GetSiteStatImporterStatus 返回指定站点的导入器状态（用于调试接口）
func GetSiteStatImporterStatus(siteID uint) (running bool, logPath string, lastErr string) {
	siteStatMu.RLock()
	defer siteStatMu.RUnlock()
	if imp, ok := siteStatMap[siteID]; ok && imp != nil {
		return imp.running, imp.logPath, imp.lastErr
	}
	return false, "", ""
}

func (imp *siteStatImporter) run() {
	defer func() { imp.running = false }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-imp.stop:
			return
		case <-ticker.C:
			if err := imp.tickOnce(); err != nil {
				imp.mu.Lock()
				imp.lastErr = err.Error()
				imp.mu.Unlock()
			}
		}
	}
}

// tickOnce 读取从上次 offset 到现在的所有新增行；行不在 last 90 天内的不入库但仍推进 offset
func (imp *siteStatImporter) tickOnce() error {
	imp.mu.Lock()
	off := imp.offset
	imp.mu.Unlock()

	f, err := os.Open(imp.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件还没产生：保持 offset=0，下次再来
			return nil
		}
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	// 日志 rotate 后文件被截断：把 offset 拨回到 0 从头开始读
	if fi.Size() < off {
		off = 0
	}
	if _, err = f.Seek(off, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	var rows []model.SiteStatVisit
	var nowOffset = off
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			nowOffset += int64(len(line))
			if v := parseSiteStatLine(line, imp.site); v != nil {
				rows = append(rows, *v)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	if len(rows) > 0 {
		// 批量插入；gorm 在 sqlite 上每批 200 行已足够
		if err := model.DB.CreateInBatches(rows, 200).Error; err != nil {
			return err
		}
	}
	imp.mu.Lock()
	imp.offset = nowOffset
	imp.lastErr = ""
	imp.mu.Unlock()
	return nil
}

// parseSiteStatLine 解析单行 nginx access 日志；返回 nil 表示跳过
func parseSiteStatLine(line string, site model.Site) *model.SiteStatVisit {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}
	m := siteStatLogRegex.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	ip := m[1]
	ts, err := parseNginxTime(m[2])
	if err != nil {
		return nil
	}
	method := m[3]
	path := m[4]
	status, _ := strconv.Atoi(m[5])
	bytesSent := int64(0)
	if m[6] != "-" {
		bytesSent, _ = strconv.ParseInt(m[6], 10, 64)
	}
	referer := m[7]
	ua := m[8]

	province, city, isp := "", "", ""
	if IpRegionEnabled() {
		if r, ok := SearchIp(ip); ok {
			province = r.Province
			city = r.City
			isp = r.ISP
		}
	}

	// IP 段归一化：IPv4 直接用；IPv6 取前 4 段前缀（前 64 bit）
	ipIndex := ip
	if parsed := net.ParseIP(ip); parsed != nil {
		if v4 := parsed.To4(); v4 != nil {
			ipIndex = v4.String()
		} else {
			ipIndex = ip + "/64"
		}
	}

	return &model.SiteStatVisit{
		SiteID:    site.ID,
		SiteName:  site.Name,
		IP:        ipIndex,
		Province:  province,
		City:      city,
		ISP:       isp,
		BytesSent: bytesSent,
		Status:    status,
		Method:    method,
		Path:      path,
		UA:        ua,
		UAHash:    hashUA(ua),
		Referer:   referer,
		VisitedAt: ts,
	}
}

func parseNginxTime(s string) (time.Time, error) {
	// 21/Aug/2026:13:24:55 +0800
	return time.Parse("02/Jan/2006:15:04:05 -0700", s)
}

func hashUA(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return int64(h.Sum64()) // SQLite 不支持 uint64 高位，转为 int64
}

// siteStatRetentionLoop 每小时跑一次：清理超过保留期的访问记录
func siteStatRetentionLoop() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		if model.DB == nil {
			continue
		}
		cutoff := time.Now().Add(-maxStatVisitRetention)
		_ = model.DB.Where("visited_at < ?", cutoff).Delete(&model.SiteStatVisit{}).Error
	}
}

// --- 统计查询 ---

// SiteStatSummary 汇总最近 N 天指标
type SiteStatSummary struct {
	Traffic    int64 `json:"traffic"`     // 流量字节
	Requests   int64 `json:"requests"`    // 总请求数
	IPs        int64 `json:"ips"`         // 独立 IP 数
	UV         int64 `json:"uv"`          // 独立访客 (UA 去重)
	PV         int64 `json:"pv"`          // 页面浏览量（行数）
	Status2xx  int64 `json:"status_2xx"`
	Status3xx  int64 `json:"status_3xx"`
	Status4xx  int64 `json:"status_4xx"`
	Status5xx  int64 `json:"status_5xx"`
	From       string `json:"from"`
	To         string `json:"to"`
}

// SiteStatSeries 一段时间内按天分组的数据
type SiteStatSeries struct {
	Days    []string `json:"days"`     // YYYY-MM-DD
	Traffic []int64  `json:"traffic"`  // 每日流量字节
	Requests []int64 `json:"requests"` // 每日请求数
	IPs      []int64 `json:"ips"`      // 每日独立 IP（粗略，基于当天的访问行）
	PV       []int64 `json:"pv"`       // 每日 PV
}

// SiteStatRegion 地域分布（按省份聚合）
type SiteStatRegion struct {
	Province string `json:"province"`
	City     string `json:"city"`
	Requests int64  `json:"requests"`
	IPs      int64  `json:"ips"`
	Traffic  int64  `json:"traffic"`
}

// SiteStatResult 完整统计查询结果
type SiteStatResult struct {
	SiteID    uint             `json:"site_id"`
	SiteName  string           `json:"site_name"`
	Presets   []string         `json:"presets"` // 支持的时间段标签：yesterday, before_yesterday, 7d, 30d, custom
	Range     SiteStatSummary  `json:"range"`   // 区间汇总
	Series    SiteStatSeries   `json:"series"`  // 时间序列
	Regions   []SiteStatRegion `json:"regions"` // 地域分布
	TopPaths  []SiteStatPath   `json:"top_paths"`
	TopIPs    []SiteStatIP     `json:"top_ips"`
	IPRegionEnabled bool       `json:"ip_region_enabled"`
}

type SiteStatPath struct {
	Path     string `json:"path"`
	Requests int64  `json:"requests"`
	Traffic  int64  `json:"traffic"`
}

type SiteStatIP struct {
	IP       string `json:"ip"`
	Province string `json:"province"`
	City     string `json:"city"`
	ISP      string `json:"isp"`
	Requests int64  `json:"requests"`
	VisitedAt string `json:"visited_at,omitempty"`
}

// GetSiteStat 按站点 ID + 时间范围查询统计
// rangeKind: yesterday / before_yesterday / 7d / 30d / custom
// start / end (仅 custom 有效，RFC3339 或 "2006-01-02")
func GetSiteStat(siteID uint, rangeKind, start, end string) (*SiteStatResult, error) {
	if model.DB == nil {
		return &SiteStatResult{SiteID: siteID, IPRegionEnabled: IpRegionEnabled()}, nil
	}
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return nil, fmt.Errorf("站点不存在")
	}
	res := &SiteStatResult{
		SiteID:   site.ID,
		SiteName: site.Name,
		IPRegionEnabled: IpRegionEnabled(),
		Presets:  []string{"yesterday", "before_yesterday", "7d", "30d", "custom"},
	}

	from, to := computeStatRange(rangeKind, start, end)
	res.Range.From = from.Format("2006-01-02")
	res.Range.To = to.Format("2006-01-02")

	// 区间汇总
	var sumAgg struct {
		Traffic   int64
		Requests  int64
		IPs       int64
		UV        int64
		PV        int64
		Status2xx int64
		Status3xx int64
		Status4xx int64
		Status5xx int64
	}
	row := model.DB.Model(&model.SiteStatVisit{}).
		Select(`
			COALESCE(SUM(bytes_sent),0) AS traffic,
			COUNT(*) AS requests,
			COUNT(DISTINCT ip) AS ips,
			COUNT(DISTINCT ua_hash) AS uv,
			COUNT(*) AS pv,
			COALESCE(SUM(CASE WHEN status>=200 AND status<300 THEN 1 ELSE 0 END),0) AS status_2xx,
			COALESCE(SUM(CASE WHEN status>=300 AND status<400 THEN 1 ELSE 0 END),0) AS status_3xx,
			COALESCE(SUM(CASE WHEN status>=400 AND status<500 THEN 1 ELSE 0 END),0) AS status_4xx,
			COALESCE(SUM(CASE WHEN status>=500 AND status<600 THEN 1 ELSE 0 END),0) AS status_5xx
		`).
		Where("site_id = ? AND visited_at >= ? AND visited_at < ?", siteID, from, to).
		Row()
	if row != nil {
		_ = row.Scan(&sumAgg.Traffic, &sumAgg.Requests, &sumAgg.IPs, &sumAgg.UV, &sumAgg.PV,
			&sumAgg.Status2xx, &sumAgg.Status3xx, &sumAgg.Status4xx, &sumAgg.Status5xx)
	}
	res.Range = SiteStatSummary{
		Traffic:   sumAgg.Traffic,
		Requests:  sumAgg.Requests,
		IPs:       sumAgg.IPs,
		UV:        sumAgg.UV,
		PV:        sumAgg.PV,
		Status2xx: sumAgg.Status2xx,
		Status3xx: sumAgg.Status3xx,
		Status4xx: sumAgg.Status4xx,
		Status5xx: sumAgg.Status5xx,
		From:      res.Range.From,
		To:        res.Range.To,
	}

	// 时间序列：按天分桶。最多覆盖所选时间段的全部天数。
	days := daysBetween(from, to)
	res.Series.Days = days
	res.Series.Traffic = make([]int64, len(days))
	res.Series.Requests = make([]int64, len(days))
	res.Series.IPs = make([]int64, len(days))
	res.Series.PV = make([]int64, len(days))

	if len(days) > 0 && len(days) <= 92 {
		type Daily struct {
			Day      string
			Traffic  int64
			Requests int64
			IPs      int64
			PV       int64
		}
		var dailies []Daily
		dayFrom := from
		dayTo := to
		_ = model.DB.Model(&model.SiteStatVisit{}).
			Select(`
				strftime('%Y-%m-%d', visited_at, 'localtime') AS day,
				COALESCE(SUM(bytes_sent),0) AS traffic,
				COUNT(*) AS requests,
				COUNT(DISTINCT ip) AS ips,
				COUNT(*) AS pv
			`).
			Where("site_id = ? AND visited_at >= ? AND visited_at < ?", siteID, dayFrom, dayTo).
			Group("day").
			Scan(&dailies)
		idx := map[string]int{}
		for i, d := range days {
			idx[d] = i
		}
		for _, d := range dailies {
			i, ok := idx[d.Day]
			if !ok {
				continue
			}
			res.Series.Traffic[i] = d.Traffic
			res.Series.Requests[i] = d.Requests
			res.Series.IPs[i] = d.IPs
			res.Series.PV[i] = d.PV
		}
	}

	// 地域分布（仅当日/小范围时；最多取 50 个）
	if IpRegionEnabled() {
		var regions []SiteStatRegion
		model.DB.Model(&model.SiteStatVisit{}).
			Select(`COALESCE(NULLIF(province,''),'未知') AS province,
			        COALESCE(NULLIF(city,''),'未知') AS city,
			        COUNT(*) AS requests,
			        COUNT(DISTINCT ip) AS ips,
			        COALESCE(SUM(bytes_sent),0) AS traffic`).
			Where("site_id = ? AND visited_at >= ? AND visited_at < ?", siteID, from, to).
			Group("province, city").
			Order("requests DESC").
			Limit(50).
			Scan(&regions)
		res.Regions = regions
	}

	// Top path / IP
	var topPaths []SiteStatPath
	model.DB.Model(&model.SiteStatVisit{}).
		Select(`path, COUNT(*) AS requests, COALESCE(SUM(bytes_sent),0) AS traffic`).
		Where("site_id = ? AND visited_at >= ? AND visited_at < ?", siteID, from, to).
		Group("path").
		Order("requests DESC").
		Limit(20).
		Scan(&topPaths)
	res.TopPaths = topPaths

	var topIPs []SiteStatIP
	rowIPs := model.DB.Model(&model.SiteStatVisit{}).
		Select(`ip, MIN(province) AS province, MIN(city) AS city, MIN(isp) AS isp,
		        COUNT(*) AS requests, MAX(visited_at) AS visited_at`).
		Where("site_id = ? AND visited_at >= ? AND visited_at < ?", siteID, from, to).
		Group("ip").
		Order("requests DESC").
		Limit(50)
	if rowIPs != nil {
		_ = rowIPs.Scan(&topIPs)
	}
	res.TopIPs = topIPs

	return res, nil
}

func computeStatRange(kind, start, end string) (time.Time, time.Time) {
	now := time.Now()
	switch kind {
	case "today":
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return today, today.AddDate(0, 0, 1)
	case "yesterday":
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return today.AddDate(0, 0, -1), today
	case "before_yesterday":
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return today.AddDate(0, 0, -2), today.AddDate(0, 0, -1)
	case "7d":
		return now.AddDate(0, 0, -7), now
	case "30d":
		return now.AddDate(0, 0, -30), now
	case "custom":
		s, e1 := parseStatDate(start), parseStatDate(end)
		if !s.IsZero() && !e1.IsZero() {
			// end 加一天把末端包含进去
			return s, e1.AddDate(0, 0, 1)
		}
		// 兜底
		return now.AddDate(0, 0, -7), now
	}
	// 兜底 today
	return now.AddDate(0, 0, -7), now
}

// ParseStatDate is exported wrapper around the unexported one used internally
func ParseStatDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

func parseStatDate(s string) time.Time { return ParseStatDate(s) }

// daysBetween 返回 [from, to) 范围按本地时区的天序列 (YYYY-MM-DD)
func daysBetween(from, to time.Time) []string {
	loc := time.Local
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	e := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc)
	if !e.After(f) {
		return nil
	}
	var out []string
	for d := f; d.Before(e); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}
