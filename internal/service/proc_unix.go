//go:build !windows

package service

import (
	"os/exec"
	"syscall"
)

// setupCmdProcAttr 让命令在独立进程组运行，取消时可整组杀死
func setupCmdProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killCmdTree 杀掉命令所在的整个进程组（含所有子进程）
func killCmdTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
