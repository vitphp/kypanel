//go:build linux

package service

import (
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
)

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
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &ptySession{File: f, cmd: cmd}, nil
}

// Resize 调整伪终端尺寸
func (p *ptySession) Resize(cols, rows int) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
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
