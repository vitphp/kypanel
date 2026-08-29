package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// trashDir 回收站根目录：<DataDir>/trash
func trashDir() string {
	return filepath.Join(config.Get().DataDir, "trash")
}

// DeleteToTrash 删除文件/目录并移入回收站（用户方案：集中存放）
// 安全规则：回收站自身不可删除
func DeleteToTrash(path string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	if clean == string(os.PathSeparator) {
		return errors.New("禁止删除根目录")
	}
	trashRoot := trashDir()
	if clean == trashRoot || strings.HasPrefix(clean, trashRoot+string(os.PathSeparator)) {
		return errors.New("回收站目录不能删除")
	}

	// 目标名冲突处理：若原路径已不存在则报错
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("文件或目录不存在")
		}
		return err
	}

	// 回收站内唯一目录
	uid := uuid.NewString()
	dstDir := filepath.Join(trashRoot, uid)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return errors.New("创建回收站目录失败: " + err.Error())
	}
	// 回收站里保留层级：trash/<uuid>/<原文件名>
	dst := filepath.Join(dstDir, filepath.Base(clean))
	if err := os.Rename(clean, dst); err != nil {
		// 跨设备回退为复制+删除
		if err := copyRecursive(clean, dst); err != nil {
			return errors.New("移入回收站失败: " + err.Error())
		}
		if err := os.RemoveAll(clean); err != nil {
			return errors.New("移入回收站后清理原文件失败: " + err.Error())
		}
	}

	// 记录数据库（不阻断：即使 DB 写入失败文件也已移走）
	typ := "file"
	if info.IsDir() {
		typ = "dir"
	}
	item := model.TrashItem{
		TrashDir:   uid,
		Type:       typ,
		Name:       filepath.Base(clean),
		OriginPath: clean,
		TrashPath:  dst,
		Size:       info.Size(),
		DeletedAt:  time.Now(),
	}
	if err := model.DB.Create(&item).Error; err != nil {
		return errors.New("回收站记录写入失败: " + err.Error())
	}
	return nil
}

// ListTrash 回收站列表（按删除时间倒序）
func ListTrash() ([]model.TrashItem, error) {
	var items []model.TrashItem
	if err := model.DB.Order("deleted_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	// 同步状态：磁盘上已不存在的条目标记为失效（跳过）
	result := make([]model.TrashItem, 0, len(items))
	for _, it := range items {
		if _, err := os.Stat(it.TrashPath); err == nil {
			result = append(result, it)
		}
	}
	return result, nil
}

// RestoreTrash 从回收站还原到原位置
func RestoreTrash(id uint) error {
	var item model.TrashItem
	if err := model.DB.First(&item, id).Error; err != nil {
		return errors.New("回收站记录不存在")
	}
	if _, err := os.Stat(item.TrashPath); err != nil {
		return errors.New("回收站内文件已丢失，无法还原")
	}
	// 原位置已存在同名文件 → 拒绝
	if _, err := os.Stat(item.OriginPath); err == nil {
		return errors.New("原位置已存在同名文件，请先处理或改名后再还原")
	}
	// 确保原目录存在
	if err := os.MkdirAll(filepath.Dir(item.OriginPath), 0o755); err != nil {
		return errors.New("还原目录创建失败: " + err.Error())
	}
	if err := os.Rename(item.TrashPath, item.OriginPath); err != nil {
		// 跨设备回退为复制+删除
		if err := copyRecursive(item.TrashPath, item.OriginPath); err != nil {
			return errors.New("还原失败: " + err.Error())
		}
		if err := os.RemoveAll(item.TrashPath); err != nil {
			return errors.New("还原后清理回收站文件失败: " + err.Error())
		}
	}
	// 清理空目录 + DB 记录
	_ = os.Remove(filepath.Dir(item.TrashPath))
	_ = model.DB.Delete(&item).Error
	return nil
}

// PurgeTrash 彻底删除回收站条目
func PurgeTrash(id uint) error {
	var item model.TrashItem
	if err := model.DB.First(&item, id).Error; err != nil {
		return errors.New("回收站记录不存在")
	}
	if err := os.RemoveAll(item.TrashPath); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(item.TrashPath))
	_ = model.DB.Delete(&item).Error
	return nil
}

// EmptyTrash 清空回收站
func EmptyTrash() error {
	if err := model.DB.Where("1=1").Delete(&model.TrashItem{}).Error; err != nil {
		return err
	}
	root := trashDir()
	if err := os.RemoveAll(root); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(root, 0o755)
}

// copyRecursive 递归复制文件/目录（保留权限与符号链接）
func copyRecursive(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyRecursive(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		_ = os.Remove(dst) // 复制失败清理半成品
		return cpErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
}

// CopyPath 复制文件/目录到目标目录（目标必须为目录）
func CopyPath(srcPath, dstDir string) error {
	src, err := SanitizePath(srcPath)
	if err != nil {
		return err
	}
	dst, err := SanitizePath(dstDir)
	if err != nil {
		return err
	}
	di, err := os.Stat(dst)
	if err != nil || !di.IsDir() {
		return errors.New("目标必须是已存在的目录")
	}
	// 防止把目录复制进自身内部
	if strings.HasPrefix(dst, src+string(os.PathSeparator)) || dst == src {
		return errors.New("不能复制到自身内部")
	}
	target := filepath.Join(dst, filepath.Base(src))
	if _, err := os.Lstat(target); err == nil {
		return errors.New("目标位置已存在同名文件: " + filepath.Base(src))
	}
	return copyRecursive(src, target)
}

// MovePath 移动文件/目录到目标目录（跨设备自动回退复制+删除）
func MovePath(srcPath, dstDir string) error {
	src, err := SanitizePath(srcPath)
	if err != nil {
		return err
	}
	dst, err := SanitizePath(dstDir)
	if err != nil {
		return err
	}
	di, err := os.Stat(dst)
	if err != nil || !di.IsDir() {
		return errors.New("目标必须是已存在的目录")
	}
	if strings.HasPrefix(dst, src+string(os.PathSeparator)) || dst == src {
		return errors.New("不能移动到自身内部")
	}
	target := filepath.Join(dst, filepath.Base(src))
	if _, err := os.Lstat(target); err == nil {
		return errors.New("目标位置已存在同名文件: " + filepath.Base(src))
	}
	if err := os.Rename(src, target); err == nil {
		return nil
	}
	// EXDEV 等跨设备错误 → 复制后删除
	if err := copyRecursive(src, target); err != nil {
		return errors.New("移动失败（复制阶段）: " + err.Error())
	}
	if err := os.RemoveAll(src); err != nil {
		return errors.New("移动失败（清理原文件）: " + err.Error())
	}
	return nil
}

// FormatTrashSize 供前端展示用的人类可读大小（避免 import 冲突）
func FormatTrashSize(n int64) string {
	return FormatBytes(n)
}
