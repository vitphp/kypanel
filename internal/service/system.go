package service

import (
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// DiskPart 磁盘分区信息
type DiskPart struct {
	Mount   string  `json:"mount"`
	FsType  string  `json:"fs_type"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

// NetIf 网卡信息
type NetIf struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Mac     string `json:"mac"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// SystemInfo 系统信息聚合
type SystemInfo struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`           // 发行版名，如 Ubuntu 22.04
	Platform    string  `json:"platform"`     // 发行版家族
	Kernel      string  `json:"kernel"`       // 内核版本
	Arch        string  `json:"arch"`         // 架构
	Uptime      uint64  `json:"uptime"`       // 开机时长（秒）
	CpuModel    string  `json:"cpu_model"`
	CpuCores    int     `json:"cpu_cores"`
	CpuPercent  float64 `json:"cpu_percent"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	MemTotal    uint64  `json:"mem_total"`
	MemUsed     uint64  `json:"mem_used"`
	MemFree     uint64  `json:"mem_free"`
	MemPercent  float64 `json:"mem_percent"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
	Disks       []DiskPart `json:"disks"`
	Nets        []NetIf    `json:"nets"`
	GoVersion   string  `json:"go_version"`
	PanelVer    string  `json:"panel_version"`
}

// GetSystemInfo 采集系统信息
func GetSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{
		GoVersion: runtime.Version(),
		PanelVer:  "0.1.0",
	}

	// 主机信息
	if hi, err := host.Info(); err == nil {
		info.Hostname = hi.Hostname
		info.OS = hi.Platform + " " + hi.PlatformVersion
		info.Platform = hi.Platform
		info.Kernel = hi.KernelVersion
		info.Arch = hi.KernelArch
		info.Uptime = hi.Uptime
	}

	// CPU
	if ci, err := cpu.Info(); err == nil && len(ci) > 0 {
		info.CpuModel = ci[0].ModelName
	}
	info.CpuCores, _ = cpu.Counts(true)
	if p, err := cpu.Percent(500*time.Millisecond, false); err == nil && len(p) > 0 {
		info.CpuPercent = p[0]
	}

	// 负载（仅 Unix 有）
	if la, err := load.Avg(); err == nil {
		info.Load1 = la.Load1
		info.Load5 = la.Load5
		info.Load15 = la.Load15
	}

	// 内存
	if mi, err := mem.VirtualMemory(); err == nil {
		info.MemTotal = mi.Total
		info.MemUsed = mi.Used
		info.MemFree = mi.Free
		info.MemPercent = mi.UsedPercent
	}
	if si, err := mem.SwapMemory(); err == nil {
		info.SwapTotal = si.Total
		info.SwapUsed = si.Used
	}

	// 磁盘分区
	if parts, err := disk.Partitions(false); err == nil {
		for _, p := range parts {
			// 跳过伪文件系统
			if skipFs(p.Fstype) {
				continue
			}
			du, err := disk.Usage(p.Mountpoint)
			if err != nil || du.Total == 0 {
				continue
			}
			info.Disks = append(info.Disks, DiskPart{
				Mount:   p.Mountpoint,
				FsType:  p.Fstype,
				Total:   du.Total,
				Used:    du.Used,
				Free:    du.Free,
				Percent: du.UsedPercent,
			})
		}
	}

	// 网卡
	if nics, err := net.Interfaces(); err == nil {
		for _, n := range nics {
			if n.HardwareAddr == "" || n.HardwareAddr == "00:00:00:00:00:00" {
				continue
			}
			ip := ""
			for _, a := range n.Addrs {
				if a.Addr != "" {
					ip = a.Addr
					break
				}
			}
			ni := NetIf{Name: n.Name, IP: ip, Mac: n.HardwareAddr}
			if io, err := net.IOCounters(false); err == nil {
				for _, c := range io {
					if c.Name == n.Name {
						ni.RxBytes = c.BytesRecv
						ni.TxBytes = c.BytesSent
					}
				}
			}
			info.Nets = append(info.Nets, ni)
		}
	}

	return info, nil
}

func skipFs(fs string) bool {
	switch fs {
	case "proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "overlay",
		"cgroup", "cgroup2", "pstore", "securityfs", "debugfs", "mqueue",
		"hugetlbfs", "configfs", "fusectl", "binfmt_misc", "rpc_pipefs",
		"autofs", "squashfs":
		return true
	}
	return false
}
