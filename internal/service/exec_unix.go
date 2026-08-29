//go:build !windows

package service

import (
	"os/exec"
	"syscall"
)

// setProcAttr 让命令跑在独立进程组里，超时才能整组清理
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup 杀死整个进程组（含派生的子进程），防止孤儿进程
func killProcessGroup(pid int) {
	// 负数 = 进程组 id（= 组长 pid）
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
