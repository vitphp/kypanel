package service

import (
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

// mountEntry 挂载点条目（sysinfo_linux 通过 /proc/mounts 解析）
type mountEntry struct {
	Mount  string
	FsType string
}

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
	Hostname   string     `json:"hostname"`
	OS         string     `json:"os"`       // 发行版名，如 Ubuntu 22.04
	Platform   string     `json:"platform"` // 发行版家族
	Kernel     string     `json:"kernel"`   // 内核版本
	Arch       string     `json:"arch"`     // 架构
	Uptime     uint64     `json:"uptime"`   // 开机时长（秒）
	CpuModel   string     `json:"cpu_model"`
	CpuCores   int        `json:"cpu_cores"`
	CpuPercent float64    `json:"cpu_percent"`
	Load1      float64    `json:"load1"`
	Load5      float64    `json:"load5"`
	Load15     float64    `json:"load15"`
	MemTotal   uint64     `json:"mem_total"`
	MemUsed    uint64     `json:"mem_used"`
	MemFree    uint64     `json:"mem_free"`
	MemPercent float64    `json:"mem_percent"`
	SwapTotal  uint64     `json:"swap_total"`
	SwapUsed   uint64     `json:"swap_used"`
	Disks      []DiskPart `json:"disks"`
	Nets       []NetIf    `json:"nets"`
	GoVersion  string     `json:"go_version"`
	PanelVer   string     `json:"panel_version"`
}

// GetSystemInfo 采集系统信息
func GetSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{
		GoVersion: runtime.Version(),
		PanelVer:  "0.1.0",
	}

	// 主机信息
	if hn, err := os.Hostname(); err == nil {
		info.Hostname = hn
	}
	info.Platform = osRelease("ID")
	if info.Platform != "" {
		info.OS = info.Platform + " " + osRelease("VERSION_ID")
	}
	info.Kernel = kernelVersion()
	info.Arch = runtime.GOARCH
	info.Uptime = uptimeSeconds()

	// CPU
	info.CpuModel = cpuModelName()
	info.CpuCores = cpuCoreCount()
	info.CpuPercent = round1(cpuPercent(500 * time.Millisecond))

	// 负载
	info.Load1, info.Load5, info.Load15 = loadAvg()

	// 内存（/proc/meminfo，单位 KB -> 字节）
	mi := memInfo()
	info.MemTotal = mi["MemTotal"] * 1024
	info.MemFree = mi["MemAvailable"] * 1024
	if info.MemFree == 0 {
		info.MemFree = mi["MemFree"] * 1024
	}
	info.MemUsed = info.MemTotal - info.MemFree
	if info.MemTotal > 0 {
		info.MemPercent = round1(float64(info.MemUsed) / float64(info.MemTotal) * 100)
	}
	info.SwapTotal = mi["SwapTotal"] * 1024
	info.SwapUsed = (mi["SwapTotal"] - mi["SwapFree"]) * 1024

	// 磁盘分区
	for _, p := range diskPartitions() {
		total, used, free, ok := diskUsage(p.Mount)
		if !ok || total == 0 {
			continue
		}
		info.Disks = append(info.Disks, DiskPart{
			Mount:   p.Mount,
			FsType:  p.FsType,
			Total:   total,
			Used:    used,
			Free:    free,
			Percent: round1(float64(used) / float64(total) * 100),
		})
	}

	// 网卡（Linux 上通过 /sys/class/net 遍历，IP/MAC 读文件）
	info.Nets = listNetInterfaces()

	return info, nil
}

// kernelVersion 读取 /proc/sys/kernel/osrelease 获取内核版本
func kernelVersion() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// listNetInterfaces 遍历 /sys/class/net 读取网卡名/IP/MAC 及累计流量
func listNetInterfaces() []NetIf {
	rx, tx := netDevTotals()
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var out []NetIf
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		mac := readSysFile("/sys/class/net/" + name + "/address")
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		ip := readInterfaceIP(name)
		out = append(out, NetIf{
			Name:    name,
			IP:      ip,
			Mac:     mac,
			RxBytes: rx,
			TxBytes: tx,
		})
	}
	return out
}

// readSysFile 读取 /sys 下单个文件内容（去换行）
func readSysFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readInterfaceIP 通过 net.Interfaces 读取网卡首个 IP（复用标准库）
func readInterfaceIP(name string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Name != name {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return ""
		}
		for _, a := range addrs {
			ip := a.String()
			if ip != "" && !strings.Contains(ip, ":") { // 取 IPv4
				if idx := strings.Index(ip, "/"); idx >= 0 {
					ip = ip[:idx]
				}
				return ip
			}
		}
	}
	return ""
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
