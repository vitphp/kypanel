//go:build !linux

package service

import (
	"errors"
	"io"
)

// ptySession 非 Linux 平台的占位实现（保证交叉编译/本地编译通过）
type ptySession struct{}

func defaultShell() string { return "cmd.exe" }

func startPty(cols, rows int, shell, cwd string) (*ptySession, error) {
	return nil, errors.New("Web 终端功能仅支持 Linux 平台")
}

func (p *ptySession) Read(b []byte) (int, error)  { return 0, io.EOF }
func (p *ptySession) Write(b []byte) (int, error) { return 0, errors.New("unsupported") }
func (p *ptySession) Resize(cols, rows int) error { return errors.New("unsupported") }
func (p *ptySession) Close() error                { return nil }
