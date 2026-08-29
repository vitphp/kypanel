//go:build !linux

package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// filePermInfo 非 Linux 平台不解析属主，仅返回八进制权限
func filePermInfo(info os.FileInfo) (perm, user, group string) {
	return fmt.Sprintf("%o", info.Mode().Perm()), "", ""
}

// ListSystemUsers 非 Linux 平台返回空列表
type SystemUser struct {
	Name      string `json:"name"`
	Uid       uint32 `json:"uid"`
	Gid       uint32 `json:"gid"`
	GroupName string `json:"group_name"`
	Home      string `json:"home"`
	Shell     string `json:"shell"`
}

func ListSystemUsers() ([]SystemUser, error) {
	return []SystemUser{}, nil
}

// ListSystemGroups 非 Linux 平台返回空列表
func ListSystemGroups() ([]string, error) {
	return []string{}, nil
}

// SetPerm 非 Linux 平台仅支持修改权限
func SetPerm(path, mode, owner, group string, recursive bool) error {
	if strings.TrimSpace(owner) != "" || strings.TrimSpace(group) != "" {
		return errors.New("当前平台不支持修改文件属主")
	}
	return Chmod(path, mode)
}

// RecommendOwner 非 Linux 平台返回空
func RecommendOwner(path string) (owner, group, site string) {
	return "", "", ""
}

// ChownToWebUser 非 Linux 平台空实现
func ChownToWebUser(path string, recursive bool) error {
	return nil
}
