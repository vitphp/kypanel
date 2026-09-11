//go:build linux

package service

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 自研系统资源采集（替代 gopsutil）：面板仅支持 Linux，直接读 /proc 文件，
// 避免 gopsutil 拖入的 wmi/go-ole/perfstat/plan9stats 等平台采集库。

// readProcLines 读取 /proc 文件全部行
func readProcLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// cpuPercent 计算整机 CPU 使用率（%）。interval 为两次采样间隔。
// 通过读取 /proc/stat 的 cpu 聚合行，两次差值计算。
func cpuPercent(interval time.Duration) float64 {
	read := func() (idle, total uint64, ok bool) {
		lines, err := readProcLines("/proc/stat")
		if err != nil {
			return 0, 0, false
		}
		for _, l := range lines {
			if !strings.HasPrefix(l, "cpu ") {
				continue
			}
			f := strings.Fields(l)
			if len(f) < 5 {
				return 0, 0, false
			}
			var vals [8]uint64
			for i := 0; i < 8 && i+1 < len(f); i++ {
				vals[i], _ = strconv.ParseUint(f[i+1], 10, 64)
			}
			idle = vals[3] + vals[4] // idle + iowait
			total = vals[0] + vals[1] + vals[2] + vals[3] + vals[4] + vals[5] + vals[6] + vals[7]
			return idle, total, true
		}
		return 0, 0, false
	}

	idle1, total1, ok1 := read()
	if !ok1 {
		return 0
	}
	time.Sleep(interval)
	idle2, total2, ok2 := read()
	if !ok2 {
		return 0
	}
	dt := total2 - total1
	if dt == 0 {
		return 0
	}
	return float64(dt-(idle2-idle1)) / float64(dt) * 100
}

// memInfo 解析 /proc/meminfo，返回字段名 -> KB 值
func memInfo() map[string]uint64 {
	m := map[string]uint64{}
	lines, err := readProcLines("/proc/meminfo")
	if err != nil {
		return m
	}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		name := strings.TrimSuffix(f[0], ":")
		v, _ := strconv.ParseUint(f[1], 10, 64)
		m[name] = v // 单位 KB
	}
	return m
}

// loadAvg 读取 /proc/loadavg 的 1/5/15 分钟负载
func loadAvg() (l1, l5, l15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(f[0], 64)
	l5, _ = strconv.ParseFloat(f[1], 64)
	l15, _ = strconv.ParseFloat(f[2], 64)
	return
}

// diskUsage 通过 statfs 获取指定挂载点容量（字节）
func diskUsage(path string) (total, used, free uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, false
	}
	total = st.Blocks * uint64(st.Bsize)
	free = st.Bavail * uint64(st.Bsize)
	used = total - st.Bfree*uint64(st.Bsize)
	return total, used, free, true
}

// diskPartitions 读取 /proc/mounts 返回真实挂载点（去重）
func diskPartitions() []mountEntry {
	lines, err := readProcLines("/proc/mounts")
	if err != nil {
		return nil
	}
	var out []mountEntry
	seen := map[string]bool{}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		dev, mp, fs := f[0], f[1], f[2]
		// 跳过伪文件系统与重复挂载点
		if skipFs(fs) || seen[mp] {
			continue
		}
		// 只保留块设备或明确为真实磁盘的挂载
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		seen[mp] = true
		out = append(out, mountEntry{Mount: mp, FsType: fs})
	}
	return out
}

// netDev 解析 /proc/net/dev 汇总所有网卡（排除 lo）的累计收发字节
func netDevTotals() (rx, tx uint64) {
	lines, err := readProcLines("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	for _, l := range lines {
		idx := strings.Index(l, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(l[:idx])
		if name == "lo" {
			continue
		}
		f := strings.Fields(l[idx+1:])
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

// diskIOTotals 读取 /proc/diskstats 汇总所有块设备的累计读写字节
func diskIOTotals() (read, write uint64) {
	lines, err := readProcLines("/proc/diskstats")
	if err != nil {
		return 0, 0
	}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 14 {
			continue
		}
		// 字段 3=sectors read, 7=sectors written（扇区 512B）
		sr, _ := strconv.ParseUint(f[5], 10, 64)
		sw, _ := strconv.ParseUint(f[9], 10, 64)
		read += sr * 512
		write += sw * 512
	}
	return read, write
}

// osRelease 读取 /etc/os-release 返回指定 key 值
func osRelease(key string) string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, key+"=") {
			continue
		}
		v := strings.TrimPrefix(l, key+"=")
		v = strings.Trim(v, `"`)
		return v
	}
	return ""
}

// cpuModelName 读取 /proc/cpuinfo 的第一个 model name
func cpuModelName() string {
	lines, err := readProcLines("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "model name") {
			if idx := strings.Index(l, ":"); idx >= 0 {
				return strings.TrimSpace(l[idx+1:])
			}
		}
	}
	return ""
}

// cpuCoreCount 统计 /proc/cpuinfo 的 processor 条目数
func cpuCoreCount() int {
	lines, err := readProcLines("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "processor") {
			n++
		}
	}
	return n
}

// uptimeSeconds 读取 /proc/uptime 返回开机秒数
func uptimeSeconds() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(data))
	if len(f) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return uint64(v)
}
