//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// ioctl 请求码（Linux amd64 / arm64 一致）
const (
	ioctlTIOCGPTN   = 0x80045430 // 取从端编号
	ioctlTIOCSPTLCK = 0x40045431 // 解锁从端
	ioctlTIOCSWINSZ = 0x5414     // 设置窗口行列数
)

// winsize 终端窗口尺寸
type winsize struct {
	Rows uint16
	Cols uint16
	X    uint16
	Y    uint16
}

// ptySession 封装伪终端与 shell 子进程
type ptySession struct {
	*os.File
	cmd  *exec.Cmd
	once sync.Once
}

// defaultShell 返回登录用户默认 shell，不可用时回退 /bin/bash
func defaultShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/bash"
	}
	return shell
}

// setWinsize 设置终端行列数
func setWinsize(f *os.File, cols, rows int) error {
	ws := &winsize{Cols: uint16(cols), Rows: uint16(rows)}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioctlTIOCSWINSZ, uintptr(unsafe.Pointer(ws)))
	if errno != 0 {
		return errno
	}
	return nil
}

// openPty 打开一对伪终端（/dev/ptmx 主端 + /dev/pts/N 从端）
func openPty() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlTIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		return nil, nil, errno
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlTIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		master.Close()
		return nil, nil, errno
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

// startPty 以指定尺寸启动 shell 伪终端；cwd 为空时使用用户主目录
func startPty(cols, rows int, shell, cwd string) (*ptySession, error) {
	// 让 bash 强制进入交互模式并设置 PS1（即便 /etc/bash.bashrc 缺失，
	// 例如 alpine / docker 基础镜像，也能显示带 pwd 的提示符）。
	var cmd *exec.Cmd
	switch {
	case strings.HasSuffix(shell, "bash"):
		cmd = exec.Command(shell, "-i")
	case strings.HasSuffix(shell, "zsh"):
		cmd = exec.Command(shell, "-i")
	case strings.HasSuffix(shell, "sh"):
		cmd = exec.Command(shell)
	default:
		cmd = exec.Command(shell)
	}
	// PS1 中包含 \w（当前路径）和 \u/\h（用户名 / 主机名），
	// 即便没有 rc 文件 prompt 也能清晰展示当前所在目录。
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=C.UTF-8",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		`PS1=\u@\h:\w\$ `,
	)
	if cwd != "" {
		if fi, err := os.Stat(cwd); err == nil && fi.IsDir() {
			cmd.Dir = cwd
		} else if home, herr := os.UserHomeDir(); herr == nil {
			cmd.Dir = home
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}

	master, slave, err := openPty()
	if err != nil {
		return nil, err
	}
	if err := setWinsize(slave, cols, rows); err != nil {
		master.Close()
		slave.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid + Setctty：子进程成为新会话首进程，并把从端设为它的控制终端；
	// Ctty 保持 0 —— 上面已把从端接到子进程的 fd 0，Go 会对其执行 TIOCSCTTY。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		return nil, err
	}
	_ = slave.Close() // 从端已交给子进程，父进程不再持有
	return &ptySession{File: master, cmd: cmd}, nil
}

// Resize 调整伪终端尺寸
func (p *ptySession) Resize(cols, rows int) error {
	return setWinsize(p.File, cols, rows)
}

// Close 终止子进程并关闭伪终端（幂等）
func (p *ptySession) Close() error {
	var err error
	p.once.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
		if p.File != nil {
			err = p.File.Close()
		}
	})
	return err
}
