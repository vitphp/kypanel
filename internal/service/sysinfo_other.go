//go:build !linux

package service

import "time"

// 非 Linux 平台：面板不支持，采集函数返回零值占位。

func cpuPercent(interval time.Duration) float64                 { return 0 }
func memInfo() map[string]uint64                                 { return nil }
func loadAvg() (l1, l5, l15 float64)                             { return 0, 0, 0 }
func diskUsage(path string) (total, used, free uint64, ok bool)  { return 0, 0, 0, false }
func diskPartitions() []mountEntry                               { return nil }
func netDevTotals() (rx, tx uint64)                              { return 0, 0 }
func diskIOTotals() (read, write uint64)                         { return 0, 0 }
func osRelease(key string) string                                { return "" }
func cpuModelName() string                                       { return "" }
func cpuCoreCount() int                                          { return 0 }
func uptimeSeconds() uint64                                      { return 0 }
