package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 门户「我的文件」：每个邮箱账号在门户站点根目录下拥有独立目录
// <门户站点根目录>/files/<账号>，用于存放发信附件，可在 webmail 中上传 / 下载 / 删除。
// 目录位于站点根目录内，便于通过文件管理器或 FTP 统一备份管理。

// MailPortalMaxFileSize 单个附件上限 25MB
const MailPortalMaxFileSize = 25 * 1024 * 1024

// MailPortalFile 文件条目
type MailPortalFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// MailPortalFilesView 文件列表视图（dir 为服务端存放目录，展示给用户）
type MailPortalFilesView struct {
	Dir   string           `json:"dir"`
	Files []MailPortalFile `json:"files"`
}

// sanitizeMailPortalFileName 归一化文件名：去掉路径、上跳与控制字符，防止目录穿越。
func sanitizeMailPortalFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "", errors.New("文件名无效")
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name, nil
}

// sanitizeMailboxDirName 账号名转安全目录名（仅保留字母数字与 . _ -）
func sanitizeMailboxDirName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		out = "default"
	}
	return out
}

// MailPortalFilesDir 返回某邮箱账号在门户站点目录下的文件存放目录（会确保目录存在）。
func MailPortalFilesDir(box *model.Mailbox) (string, error) {
	if box == nil {
		return "", errors.New("账号不存在")
	}
	var dom model.MailDomain
	if err := model.DB.First(&dom, box.DomainID).Error; err != nil {
		return "", errors.New("邮箱域名不存在")
	}
	if dom.PortalSiteID == 0 {
		return "", errors.New("该域名尚未开启门户网站，请先在后台开启")
	}
	s, err := getSiteOrErr(dom.PortalSiteID)
	if err != nil {
		return "", errors.New("门户站点不存在，请重新保存门户配置")
	}
	dir := filepath.Join(s.Root, "files", sanitizeMailboxDirName(box.Name))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ListMailPortalFiles 列出账号文件目录内容（按修改时间倒序）。
func ListMailPortalFiles(box *model.Mailbox) (*MailPortalFilesView, error) {
	dir, err := MailPortalFilesDir(box)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]MailPortalFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, MailPortalFile{Name: e.Name(), Size: info.Size(), Mtime: info.ModTime().Unix()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Mtime > files[j].Mtime })
	return &MailPortalFilesView{Dir: dir, Files: files}, nil
}

// SaveMailPortalFile 保存上传文件到账号目录（同名自动改名，不覆盖）。
func SaveMailPortalFile(box *model.Mailbox, filename string, data []byte) (*MailPortalFile, error) {
	if len(data) == 0 {
		return nil, errors.New("文件内容为空")
	}
	if len(data) > MailPortalMaxFileSize {
		return nil, fmt.Errorf("单个文件不能超过 %dMB", MailPortalMaxFileSize/1024/1024)
	}
	name, err := sanitizeMailPortalFileName(filename)
	if err != nil {
		return nil, err
	}
	dir, err := MailPortalFilesDir(box)
	if err != nil {
		return nil, err
	}
	name = uniqueMailPortalFileName(dir, name)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return nil, err
	}
	return &MailPortalFile{Name: name, Size: int64(len(data)), Mtime: time.Now().Unix()}, nil
}

// uniqueMailPortalFileName 同名时追加 (1)、(2) 后缀。
func uniqueMailPortalFileName(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		cand := fmt.Sprintf("%s(%d)%s", base, i, ext)
		if _, err := os.Stat(filepath.Join(dir, cand)); err != nil {
			return cand
		}
	}
	return fmt.Sprintf("%s(%d)%s", base, time.Now().UnixNano(), ext)
}

// DeleteMailPortalFile 删除账号目录下的某文件。
func DeleteMailPortalFile(box *model.Mailbox, name string) error {
	name, err := sanitizeMailPortalFileName(name)
	if err != nil {
		return err
	}
	dir, err := MailPortalFilesDir(box)
	if err != nil {
		return err
	}
	full := filepath.Join(dir, name)
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		return errors.New("文件不存在")
	}
	return os.Remove(full)
}

// GetMailPortalFile 读取账号目录下的某文件（供下载或作为附件发送）。
func GetMailPortalFile(box *model.Mailbox, name string) (filename, contentType string, data []byte, err error) {
	name, err = sanitizeMailPortalFileName(name)
	if err != nil {
		return "", "", nil, err
	}
	dir, err := MailPortalFilesDir(box)
	if err != nil {
		return "", "", nil, err
	}
	full := filepath.Join(dir, name)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", "", nil, errors.New("文件不存在")
	}
	data, err = os.ReadFile(full)
	if err != nil {
		return "", "", nil, errors.New("文件读取失败")
	}
	return name, guessMailContentType(name), data, nil
}

// LoadMailPortalAttachments 把文件库中的若干文件组装成邮件附件（发送时调用）。
func LoadMailPortalAttachments(box *model.Mailbox, names []string) ([]MailAttachment, error) {
	out := make([]MailAttachment, 0, len(names))
	var total int64
	for _, n := range names {
		fname, ctype, data, err := GetMailPortalFile(box, n)
		if err != nil {
			return nil, fmt.Errorf("附件 %s 不存在", n)
		}
		total += int64(len(data))
		if total > MailPortalMaxFileSize {
			return nil, fmt.Errorf("附件总大小不能超过 %dMB", MailPortalMaxFileSize/1024/1024)
		}
		out = append(out, MailAttachment{Filename: fname, ContentType: ctype, Data: data})
	}
	return out, nil
}

// guessMailContentType 按扩展名推断 Content-Type（不依赖系统 mime 库，跨平台一致）。
func guessMailContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".log", ".md", ".csv", ".ini", ".conf":
		return "text/plain; charset=UTF-8"
	case ".html", ".htm":
		return "text/html; charset=UTF-8"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".zip":
		return "application/zip"
	case ".rar":
		return "application/vnd.rar"
	case ".7z":
		return "application/x-7z-compressed"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".tar":
		return "application/x-tar"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	}
	return "application/octet-stream"
}
