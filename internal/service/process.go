package service

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ProcessInfo 进程信息
type ProcessInfo struct {
	PID     int    `json:"pid"`
	User    string `json:"user"`
	CPU     string `json:"cpu"`
	Mem     string `json:"mem"`
	RSSKB   int64  `json:"rss_kb"`   // 实际占用内存（KB），便于按内存绝对值排序
	Command string `json:"command"`
}

// 允许的排序字段
var validProcessSort = map[string]string{
	"cpu":     "-%cpu",
	"mem":     "-%mem",
	"rss":     "-rss",
	"pid":     "pid",
	"user":    "user",
	"command": "command",
}

// ListProcesses 列出进程，按 sort 字段降序（除 pid/默认 cpu）
// sort: cpu | mem | rss | pid | user | command (默认 cpu)
func ListProcesses(sortKey string, keyword string, limit int) ([]ProcessInfo, error) {
	if sortKey == "" {
		sortKey = "cpu"
	}
	psSort, ok := validProcessSort[sortKey]
	if !ok {
		psSort = "-%cpu"
	}

	cmd := "ps -eo pid,user,%cpu,%mem,rss,command --sort=" + psSort
	cmd += " | head -n 2000" // 先取较大池子，前端再过滤/截取

	res, err := ExecCommand(cmd, defaultExecTimeout)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}

	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	var procs []ProcessInfo
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		rss, _ := strconv.ParseInt(fields[4], 10, 64)
		procs = append(procs, ProcessInfo{
			PID:     pid,
			User:    fields[1],
			CPU:     fields[2],
			Mem:     fields[3],
			RSSKB:   rss,
			Command: strings.Join(fields[5:], " "),
		})
	}

	// 关键字过滤（命令/PID/用户）
	if keyword != "" {
		kw := strings.ToLower(keyword)
		filtered := procs[:0]
		for _, p := range procs {
			if strings.Contains(strings.ToLower(p.Command), kw) ||
				strings.Contains(strings.ToLower(p.User), kw) ||
				strings.Contains(strconv.Itoa(p.PID), kw) {
				filtered = append(filtered, p)
			}
		}
		procs = filtered
	}

	// 二次排序：ps 本身的 --sort 对 rss 列在我们这台机器上可能不为负生效，统一兜底
	switch sortKey {
	case "mem":
		sort.SliceStable(procs, func(i, j int) bool {
			return parseFloat(procs[i].Mem) > parseFloat(procs[j].Mem)
		})
	case "rss":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].RSSKB > procs[j].RSSKB })
	case "pid":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
	case "user":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].User < procs[j].User })
	case "command":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].Command < procs[j].Command })
	case "cpu", "":
		sort.SliceStable(procs, func(i, j int) bool {
			return parseFloat(procs[i].CPU) > parseFloat(procs[j].CPU)
		})
	}

	if limit > 0 && len(procs) > limit {
		procs = procs[:limit]
	}
	return procs, nil
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// KillProcess 结束进程
func KillProcess(pid int) error {
	if pid <= 0 {
		return errors.New("无效 PID")
	}
	res, err := ExecCommand("kill -9 "+strconv.Itoa(pid), defaultExecTimeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New("结束进程失败: " + res.Stderr)
	}
	return nil
}
