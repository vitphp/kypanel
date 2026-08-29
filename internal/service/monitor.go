package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// MonitorPoint 一个监控数据点
type MonitorPoint struct {
	Time    int64   `json:"time"`    // 时间戳（秒）
	Cpu     float64 `json:"cpu"`     // CPU 使用率 %
	Mem     float64 `json:"mem"`     // 内存使用率 %
	Disk    float64 `json:"disk"`    // 根分区使用率 %
	NetIn   float64 `json:"net_in"`  // 入流量 KB/s
	NetOut  float64 `json:"net_out"` // 出流量 KB/s
	Load1   float64 `json:"load1"`   // 1 分钟负载
	Load5   float64 `json:"load5"`   // 5 分钟负载
	Load15  float64 `json:"load15"`  // 15 分钟负载
}

const (
	monitorInterval = 5 * time.Second
	monitorCapacity = 120 // 内存中保留约 10 分钟
)

var (
	monitorMu      sync.RWMutex
	monitorPts     = make([]MonitorPoint, 0, monitorCapacity)
	lastNetRx      uint64
	lastNetTx      uint64
	lastNetTime    time.Time
	monitorReady   bool
	monitorStopCh  = make(chan struct{})
	monitorRunning bool
)

// MonitorConfig 监控设置
type MonitorConfig struct {
	Enabled   bool `json:"enabled"`
	SaveDays  int  `json:"save_days"`
}

// GetMonitorConfig 读取监控配置，默认开启、保存 30 天
func GetMonitorConfig() MonitorConfig {
	enabled := model.GetSetting("monitor_enabled")
	days := model.GetSetting("monitor_save_days")
	cfg := MonitorConfig{Enabled: true, SaveDays: 30}
	if enabled != "" {
		cfg.Enabled = enabled == "1" || enabled == "true"
	}
	if days != "" {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			cfg.SaveDays = n
		}
	}
	return cfg
}

func setMonitorConfig(cfg MonitorConfig) error {
	env := "1"
	if !cfg.Enabled {
		env = "0"
	}
	if err := model.SetSetting("monitor_enabled", env); err != nil {
		return err
	}
	if cfg.SaveDays < 1 {
		cfg.SaveDays = 1
	}
	if err := model.SetSetting("monitor_save_days", strconv.Itoa(cfg.SaveDays)); err != nil {
		return err
	}
	return nil
}

// monitorDataPath 监控数据文件路径
func monitorDataPath() string {
	return filepath.Join(config.Get().DataDir, "monitor.json")
}

// LoadMonitorHistory 启动时从历史文件加载数据
func LoadMonitorHistory() error {
	path := monitorDataPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var pts []MonitorPoint
	if err := json.Unmarshal(data, &pts); err != nil {
		return err
	}
	// 只保留在保存天数内的数据
	cfg := GetMonitorConfig()
	cutoff := time.Now().AddDate(0, 0, -cfg.SaveDays).Unix()
	monitorMu.Lock()
	defer monitorMu.Unlock()
	for _, p := range pts {
		if p.Time >= cutoff {
			monitorPts = append(monitorPts, p)
		}
	}
	if len(monitorPts) > monitorCapacity {
		monitorPts = monitorPts[len(monitorPts)-monitorCapacity:]
	}
	return nil
}

// SaveMonitorHistory 持久化监控数据
func SaveMonitorHistory() error {
	monitorMu.RLock()
	pts := make([]MonitorPoint, len(monitorPts))
	copy(pts, monitorPts)
	monitorMu.RUnlock()

	path := monitorDataPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(pts)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// StartMonitor 启动后台监控采集
func StartMonitor() {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	if monitorRunning {
		return
	}
	monitorRunning = true
	monitorStopCh = make(chan struct{})
	go monitorLoop()
}

// StopMonitor 停止后台监控采集
func StopMonitor() {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	if !monitorRunning {
		return
	}
	close(monitorStopCh)
	monitorRunning = false
}

func monitorLoop() {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	// 立即采集一次
	if GetMonitorConfig().Enabled {
		collectOnce()
	}

	for {
		select {
		case <-monitorStopCh:
			return
		case <-ticker.C:
			if GetMonitorConfig().Enabled {
				collectOnce()
			}
		}
	}
}

func collectOnce() {
	p := MonitorPoint{Time: time.Now().Unix()}

	if c, err := cpu.Percent(0, false); err == nil && len(c) > 0 {
		p.Cpu = round1(c[0])
	}
	if m, err := mem.VirtualMemory(); err == nil {
		p.Mem = round1(m.UsedPercent)
	}
	if d, err := disk.Usage("/"); err == nil {
		p.Disk = round1(d.UsedPercent)
	}
	if la, err := load.Avg(); err == nil {
		p.Load1 = round2(la.Load1)
		p.Load5 = round2(la.Load5)
		p.Load15 = round2(la.Load15)
	}

	// 网络速率：用两次 IOCounters 差值估算
	now := time.Now()
	if counters, err := net.IOCounters(false); err == nil && len(counters) > 0 {
		var rx, tx uint64
		for _, c := range counters {
			rx += c.BytesRecv
			tx += c.BytesSent
		}
		if monitorReady && !lastNetTime.IsZero() {
			dt := now.Sub(lastNetTime).Seconds()
			if dt > 0 {
				p.NetIn = round2(float64(rx-lastNetRx) / 1024 / dt)  // KB/s
				p.NetOut = round2(float64(tx-lastNetTx) / 1024 / dt) // KB/s
			}
		}
		lastNetRx = rx
		lastNetTx = tx
		lastNetTime = now
		monitorReady = true
	}

	monitorMu.Lock()
	monitorPts = append(monitorPts, p)
	if len(monitorPts) > monitorCapacity {
		monitorPts = monitorPts[len(monitorPts)-monitorCapacity:]
	}
	monitorMu.Unlock()

	// 每 1 分钟持久化一次，并清理过期
	if int(p.Time)%60 < 5 {
		_ = cleanAndSaveMonitorHistory()
	}
}

// cleanAndSaveMonitorHistory 清理过期数据并持久化
func cleanAndSaveMonitorHistory() error {
	cfg := GetMonitorConfig()
	cutoff := time.Now().AddDate(0, 0, -cfg.SaveDays).Unix()

	monitorMu.Lock()
	n := 0
	for _, p := range monitorPts {
		if p.Time >= cutoff {
			monitorPts[n] = p
			n++
		}
	}
	monitorPts = monitorPts[:n]
	monitorMu.Unlock()

	return SaveMonitorHistory()
}

// UpdateMonitorConfig 更新监控配置
func UpdateMonitorConfig(cfg MonitorConfig) error {
	old := GetMonitorConfig()
	if err := setMonitorConfig(cfg); err != nil {
		return err
	}
	// 关闭监控时立即保存一次；变更保存天数时立即清理
	if !cfg.Enabled || cfg.SaveDays != old.SaveDays {
		_ = cleanAndSaveMonitorHistory()
	}
	return nil
}

// GetMonitorHistory 返回历史监控点（按时间范围）
func GetMonitorHistory(start, end int64) []MonitorPoint {
	monitorMu.RLock()
	defer monitorMu.RUnlock()
	out := make([]MonitorPoint, 0, len(monitorPts))
	for _, p := range monitorPts {
		if (start == 0 || p.Time >= start) && (end == 0 || p.Time <= end) {
			out = append(out, p)
		}
	}
	return out
}

// GetMonitorHistoryByRange 根据字符串范围返回历史数据，便于 HTTP 接口调用
func GetMonitorHistoryByRange(rangeType string, customStart, customEnd int64) []MonitorPoint {
	now := time.Now()
	var start, end int64
	switch rangeType {
	case "today":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		end = now.Unix()
	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		start = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location()).Unix()
		end = start + 86400
	case "week":
		start = now.AddDate(0, 0, -7).Unix()
		end = now.Unix()
	case "custom":
		start, end = customStart, customEnd
	default:
		// 默认内存中的最近数据
		monitorMu.RLock()
		defer monitorMu.RUnlock()
		out := make([]MonitorPoint, len(monitorPts))
		copy(out, monitorPts)
		return out
	}
	return GetMonitorHistory(start, end)
}

// GetMonitorCurrent 返回最近一个监控点（未就绪时立即采集一次）
func GetMonitorCurrent() MonitorPoint {
	monitorMu.RLock()
	if len(monitorPts) > 0 {
		p := monitorPts[len(monitorPts)-1]
		monitorMu.RUnlock()
		return p
	}
	monitorMu.RUnlock()
	if GetMonitorConfig().Enabled {
		collectOnce()
	}
	monitorMu.RLock()
	defer monitorMu.RUnlock()
	if len(monitorPts) > 0 {
		return monitorPts[len(monitorPts)-1]
	}
	return MonitorPoint{}
}

// ClearMonitorHistory 清空监控历史
func ClearMonitorHistory() error {
	monitorMu.Lock()
	monitorPts = monitorPts[:0]
	monitorMu.Unlock()
	return SaveMonitorHistory()
}

// MonitorLogSize 返回监控日志文件大小（字节）
func MonitorLogSize() int64 {
	info, err := os.Stat(monitorDataPath())
	if err != nil {
		return 0
	}
	return info.Size()
}

// MonitorStats 返回监控历史聚合统计
func MonitorStats() map[string]float64 {
	monitorMu.RLock()
	pts := make([]MonitorPoint, len(monitorPts))
	copy(pts, monitorPts)
	monitorMu.RUnlock()
	if len(pts) == 0 {
		return map[string]float64{}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time < pts[j].Time })
	avg := func(fn func(MonitorPoint) float64) float64 {
		var sum float64
		for _, p := range pts {
			sum += fn(p)
		}
		return round2(sum / float64(len(pts)))
	}
	return map[string]float64{
		"avg_cpu":    avg(func(p MonitorPoint) float64 { return p.Cpu }),
		"avg_mem":    avg(func(p MonitorPoint) float64 { return p.Mem }),
		"avg_disk":   avg(func(p MonitorPoint) float64 { return p.Disk }),
		"avg_load1":  avg(func(p MonitorPoint) float64 { return p.Load1 }),
		"avg_load5":  avg(func(p MonitorPoint) float64 { return p.Load5 }),
		"avg_load15": avg(func(p MonitorPoint) float64 { return p.Load15 }),
	}
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
