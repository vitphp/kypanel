package service

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/net"

	"kypanel/internal/model"
)

// DashboardSummary 概览页聚合数据
type DashboardSummary struct {
	NetInRate  float64 `json:"net_in_rate"`  // 入流量速率 KB/s
	NetOutRate float64 `json:"net_out_rate"` // 出流量速率 KB/s
	NetInTotal  uint64 `json:"net_in_total"`  // 累计入流量字节
	NetOutTotal uint64 `json:"net_out_total"` // 累计出流量字节
	DiskReadRate  float64 `json:"disk_read_rate"`  // 磁盘读速率 KB/s
	DiskWriteRate float64 `json:"disk_write_rate"` // 磁盘写速率 KB/s
	AlertEnabled  bool    `json:"alert_enabled"`   // 告警是否启用
	AlertCount24h int64   `json:"alert_count_24h"` // 最近 24h 告警数
	TopProcs   []ProcessInfo `json:"top_procs"`   // CPU 占用 TOP 进程
	Services   []DashboardService `json:"services"`  // 关键服务状态
}

// DashboardService 概览页服务状态
type DashboardService struct {
	Key        string `json:"key"`         // 应用商店 key
	Name       string `json:"name"`        // 展示名
	Service    string `json:"service"`     // systemd 服务名
	Running    bool   `json:"running"`     // 是否运行
	Active     string `json:"active"`      // active / inactive / not-found
	HasSettings bool  `json:"has_settings"` // 是否有设置入口（跳转对应管理页）
	LastAction int64  `json:"last_action"` // 最近一次启停/重启操作时间（unix 秒，0=从未操作）
}

// serviceActionTimes 记录各应用服务最后一次操作时间（内存态，用于概览排序）
var serviceActionTimes = make(map[string]time.Time)

// serviceHasSettings 判断应用是否有「设置」入口（跳转到面板对应管理页）
func serviceHasSettings(key string) bool {
	switch key {
	case "nginx", "apache":
		return true // 网站管理
	case "mysql", "mariadb", "postgresql", "redis", "mongodb", "sqlserver", "phpmyadmin":
		return true // 数据库管理
	case "docker":
		return true // 容器管理
	case "ftp":
		return true // FTP 管理
	}
	return false
}

// isEnvCategory 判断 appMeta.Category 是否属于"环境类"（概览展示的类别）：
// server / database / cache / runtime / tool。远程拉取的"应用类"会被过滤掉。
func isEnvCategory(cat string) bool {
	switch cat {
	case "server", "database", "cache", "runtime", "tool":
		return true
	}
	return false
}

// netRateState 网络流量采样状态
var (
	netRateLastRx   uint64
	netRateLastTx   uint64
	netRateLastTime time.Time
)

// diskIORateState 磁盘 IO 采样状态
var (
	diskIOLastRead   uint64
	diskIOLastWrite  uint64
	diskIOLastTime   time.Time
)

// GetDashboardSummary 采集概览页聚合数据
func GetDashboardSummary() *DashboardSummary {
	summary := &DashboardSummary{}

	// 网络实时速率（两次采样差值）+ 累计流量
	summary.NetInRate, summary.NetOutRate = sampleNetRate()
	summary.NetInTotal, summary.NetOutTotal = netTotalBytes()

	// 磁盘 IO 速率（两次采样差值）
	summary.DiskReadRate, summary.DiskWriteRate = sampleDiskIO()

	// 告警状态
	summary.AlertEnabled = model.GetAlertConfig().Enabled
	model.DB.Model(&model.AlertLog{}).Where("created_at > ?", time.Now().Add(-24*time.Hour)).Count(&summary.AlertCount24h)

	// 进程 TOP（CPU 排序，取前 10）
	if procs, err := ListProcesses("cpu", "", 10); err == nil {
		summary.TopProcs = procs
	}

	// 服务状态：只显示"已安装"的环境类应用（按最近操作时间排序）。
	// 概览只展示"环境类"服务（web 服务器 / 数据库 / 缓存 / 运行时 / 容器 / ftp），
	// 远程拉取的"应用类"（如 phpmyadmin、vsftpd）不在此显示。
	// 「是否已安装」统一从 EnvStatus() 读取（其底层探测并缓存到 {DataDir}/env_status.json），
	// 不直接依赖 app_records 表，避免旧版本迁移丢失记录导致服务状态空白。
	envMap := EnvStatus()
	recByKey := make(map[string]*model.AppRecord)
	var recs []model.AppRecord
	model.DB.Where("status = ?", model.AppInstalled).Find(&recs)
	for i := range recs {
		recByKey[recs[i].Key] = &recs[i]
	}
	for _, meta := range appMetas {
		// 只保留环境类（server/database/cache/runtime/tool），过滤掉应用类
		if !isEnvCategory(meta.Category) {
			continue
		}
		// 已安装判断：读 EnvStatus 缓存（env_status.json）
		env, envOK := envMap[meta.Key]
		if !envOK || !env.Installed {
			continue
		}
		serviceName := resolveServiceName(meta)
		if serviceName == "" {
			continue // 无服务可管（sqlite / nodejs / python3 / golang / java 等）
		}
		// 若 app_records 里有该应用的服务名记录（面板内安装时可能改名），优先用它
		if rec, ok := recByKey[meta.Key]; ok && rec.ServiceName != "" {
			serviceName = rec.ServiceName
		}
		item := DashboardService{
			Key:         meta.Key,
			Name:        meta.Name,
			Service:     serviceName,
			HasSettings: serviceHasSettings(meta.Key),
			LastAction:  0,
		}
		if t, ok := serviceActionTimes[meta.Key]; ok {
			item.LastAction = t.Unix()
		}
		res, err := ExecCommand("systemctl is-active "+serviceName+" 2>/dev/null", 3*time.Second)
		if err != nil {
			item.Active = "not-found"
			item.Running = false
		} else {
			item.Active = strings.TrimSpace(res.Stdout)
			item.Running = item.Active == "active"
		}
		summary.Services = append(summary.Services, item)
	}

	// 按最近操作时间倒序（最近操作过的排前面），未操作过的保持商店顺序在后
	sort.SliceStable(summary.Services, func(i, j int) bool {
		return summary.Services[i].LastAction > summary.Services[j].LastAction
	})

	return summary
}

// sampleNetRate 采样网络总流量速率（KB/s）
func sampleNetRate() (inRate, outRate float64) {
	now := time.Now()
	var totalRx, totalTx uint64
	if counters, err := net.IOCounters(false); err == nil && len(counters) > 0 {
		totalRx = counters[0].BytesRecv
		totalTx = counters[0].BytesSent
	}

	if !netRateLastTime.IsZero() {
		elapsed := now.Sub(netRateLastTime).Seconds()
		if elapsed > 0 && totalRx >= netRateLastRx && totalTx >= netRateLastTx {
			inRate = float64(totalRx-netRateLastRx) / elapsed / 1024
			outRate = float64(totalTx-netRateLastTx) / elapsed / 1024
		}
	}

	netRateLastRx = totalRx
	netRateLastTx = totalTx
	netRateLastTime = now
	return
}

// netTotalBytes 返回网卡累计入/出字节数
func netTotalBytes() (rx, tx uint64) {
	if counters, err := net.IOCounters(false); err == nil && len(counters) > 0 {
		return counters[0].BytesRecv, counters[0].BytesSent
	}
	return 0, 0
}

// sampleDiskIO 采样磁盘总读写速率（KB/s）
func sampleDiskIO() (readRate, writeRate float64) {
	now := time.Now()
	var totalRead, totalWrite uint64
	if counters, err := disk.IOCounters(); err == nil {
		for _, c := range counters {
			totalRead += c.ReadBytes
			totalWrite += c.WriteBytes
		}
	}

	if !diskIOLastTime.IsZero() {
		elapsed := now.Sub(diskIOLastTime).Seconds()
		if elapsed > 0 && totalRead >= diskIOLastRead && totalWrite >= diskIOLastWrite {
			readRate = float64(totalRead-diskIOLastRead) / elapsed / 1024
			writeRate = float64(totalWrite-diskIOLastWrite) / elapsed / 1024
		}
	}

	diskIOLastRead = totalRead
	diskIOLastWrite = totalWrite
	diskIOLastTime = now
	return
}

// defaultDashboardLayout 概览页默认卡片顺序
var defaultDashboardLayout = []string{
	"uptime", "cpu", "mem", "disk", "total", "net", "diskio", "alert",
}

// GetDashboardLayout 读取概览页卡片布局
func GetDashboardLayout() []string {
	raw := model.GetSetting("dashboard_layout")
	if raw == "" {
		return defaultDashboardLayout
	}
	var layout []string
	if err := json.Unmarshal([]byte(raw), &layout); err != nil || len(layout) != len(defaultDashboardLayout) {
		return defaultDashboardLayout
	}
	set := make(map[string]bool)
	for _, k := range layout {
		set[k] = true
	}
	for _, k := range defaultDashboardLayout {
		if !set[k] {
			return defaultDashboardLayout
		}
	}
	return layout
}

// SaveDashboardLayout 保存概览页卡片布局
func SaveDashboardLayout(layout []string) error {
	if len(layout) != len(defaultDashboardLayout) {
		return errors.New("布局数据不完整")
	}
	set := make(map[string]bool)
	for _, k := range layout {
		set[k] = true
	}
	for _, k := range defaultDashboardLayout {
		if !set[k] {
			return errors.New("布局数据非法")
		}
	}
	raw, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	return model.SetSetting("dashboard_layout", string(raw))
}
