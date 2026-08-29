//go:build windows

package service

import (
	"os/exec"
)

// setProcAttr Windows 上无进程组语义，空实现保证本地编译
func setProcAttr(cmd *exec.Cmd) {}

// killProcessGroup Windows 上无进程组语义，空实现
func killProcessGroup(pid int) {}
