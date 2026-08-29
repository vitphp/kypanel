//go:build windows

package service

import "os/exec"

// setupCmdProcAttr Windows 下无需设置进程组
func setupCmdProcAttr(cmd *exec.Cmd) {}

// killCmdTree Windows 下仅杀死直接子进程
func killCmdTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
