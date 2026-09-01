package service

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FileItem 文件/目录条目
type FileItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`  // 八进制权限，如 755
	Owner   string `json:"owner"` // 属主:属组，如 www:www（保留兼容）
	User    string `json:"user"`  // 属主，如 www
	Group   string `json:"group"` // 属组，如 www
	ModTime string `json:"mod_time"`
	Dir     string `json:"dir,omitempty"` // 父目录（搜索结果用）
}

// SanitizePath 规范化路径并阻止越权（防止 ../ 逃逸）
func SanitizePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("路径不能为空")
	}
	clean := filepath.Clean(path)
	// 禁止相对路径逃逸到当前目录之外
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("非法路径")
	}
	if !filepath.IsAbs(clean) {
		return "", errors.New("必须使用绝对路径")
	}
	return clean, nil
}

// ListDir 列出目录内容
func ListDir(path string) ([]FileItem, error) {
	clean, err := SanitizePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, err
	}
	items := make([]FileItem, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		perm, user, group := filePermInfo(info)
		owner := user
		if group != "" {
			owner = user + ":" + group
		}
		items = append(items, FileItem{
			Name:    e.Name(),
			Path:    filepath.Join(clean, e.Name()),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			Mode:    perm,
			Owner:   owner,
			User:    user,
			Group:   group,
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	// 目录在前，名称排序
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// ReadFile 读取文本文件
// MaxReadFileSize 限制在线编辑文件最大 3MB，避免传输大文件造成卡顿
const MaxReadFileSize = 3 * 1024 * 1024

func ReadFile(path string) (string, error) {
	clean, err := SanitizePath(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if fi.Size() > MaxReadFileSize {
		return "", fmt.Errorf("文件过大（%s），超过 3MB 限制，无法在线编辑", FormatBytes(fi.Size()))
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile 写入文本文件（覆盖）。保留原文件权限；新文件使用 0644（不设可执行位）。
func WriteFile(path, content string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(clean); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(clean, []byte(content), mode)
}

// CreateDir 创建目录
func CreateDir(path string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(clean, 0o755)
}

// CreateFile 创建空文件
func CreateFile(path string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(clean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// RenameFile 重命名/移动
func RenameFile(oldPath, newPath string) error {
	oldClean, err := SanitizePath(oldPath)
	if err != nil {
		return err
	}
	newClean, err := SanitizePath(newPath)
	if err != nil {
		return err
	}
	return os.Rename(oldClean, newClean)
}

// DeleteFile 删除文件或目录（目录递归删除）
func DeleteFile(path string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	if clean == string(os.PathSeparator) || clean == "" {
		return errors.New("禁止删除根目录")
	}
	return os.RemoveAll(clean)
}

// GetDiskUsage 获取指定路径的总字节数（按文件"表观大小"，不含文件系统块对齐）
//
// 用 du -sb 而不是 du -sh：
// 1. du -sh 按 4096 字节块统计（ext4 / xfs 默认），空目录也会显示 4.0K，
//    但用户期望的"里面内容有多大"是真实占用：空目录应为 0
// 2. -b 表示 --apparent-size，只算文件实际字节数，不乘块大小
// 3. stdout 输出 "1234567\t/path"，首段就是字节数，前端直接格式化
func GetDiskUsage(path string) (*ExecResult, error) {
	clean, err := SanitizePath(path)
	if err != nil {
		return nil, err
	}
	return ExecCommand("du -sb "+clean, defaultExecTimeout)
}

// uploadTmpPath 返回某上传任务对应的临时文件路径（面板风格）：
//
//	<原文件名>.<文件总大小>.<file_id>.upload.tmp
//
// 例如 macOS Catalina10.15.7.iso.8589934592.aB3x9.upload.tmp
// 字节级续传：整个上传过程始终只有一个临时文件，追加写入，完成后重命名为最终文件。
func uploadTmpPath(dst, fileID string, totalSize int64) string {
	if totalSize >= 0 {
		return fmt.Sprintf("%s.%d.%s.upload.tmp", dst, totalSize, fileID)
	}
	// totalSize 未知时退化为不带大小的形式（仍以 file_id 定位）
	return fmt.Sprintf("%s.%s.upload.tmp", dst, fileID)
}

// SaveUpload 保存上传文件（不分片，单请求直传）。
// 从 offset 0 开始，用 O_TRUNC 直接写最终文件。
func SaveUpload(dstPath string, src io.Reader) error {
	_, _, err := SaveUploadAppend(dstPath, "", 0, -1, src)
	return err
}

// SaveUploadAppend 字节级续传上传：把 src 追加写入临时文件 <dst>.up.<file_id>.part。
// offset 为服务端已写入的字节数（续传起点），前端据此 slice 后从断点继续。
// totalSize 为文件总大小；< 0 表示未知（不分片直传场景，即写满即完成）。
// 返回 (已写入字节数, 是否已完成, error)。完成时把临时文件重命名为最终文件 dst。
func SaveUploadAppend(dstPath, fileID string, offset int64, totalSize int64, src io.Reader) (written int64, complete bool, err error) {
	clean, err := SanitizePath(dstPath)
	if err != nil {
		return 0, false, err
	}
	if dir := filepath.Dir(clean); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, false, err
		}
	}

	// 未携带 file_id 的直传场景：直接写最终文件（脚本 / 兼容旧调用）
	if fileID == "" {
		out, err := os.OpenFile(clean, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return 0, false, err
		}
		defer out.Close()
		n, err := io.Copy(out, src)
		if err != nil {
			return n, false, err
		}
		_ = ChownToWebUser(clean, false)
		return n, true, nil
	}

	tmp := uploadTmpPath(clean, fileID, totalSize)

	// offset == 0 表示全新上传，先清空残留临时文件；否则校验 offset 与已写字节一致
	if offset <= 0 {
		_ = os.Remove(tmp)
	} else {
		if info, e := os.Stat(tmp); e == nil && info.Size() != offset {
			return 0, false, fmt.Errorf("上传偏移不一致：服务端已写 %d 字节，请求续传起点 %d", info.Size(), offset)
		}
	}

	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o755)
	if err != nil {
		return 0, false, err
	}
	n, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return offset + n, false, copyErr
	}
	if closeErr != nil {
		return offset + n, false, closeErr
	}
	written = offset + n

	// 判断是否完成：totalSize < 0 视为"本次请求即整个文件"，直接完成；
	// 否则已写字节数达到 totalSize 才算完成
	if totalSize < 0 || written >= totalSize {
		if err := os.Rename(tmp, clean); err != nil {
			// 某些情况下目标已存在（如覆盖时旧文件未删），先删再改名
			if rmErr := os.Remove(clean); rmErr == nil {
				if err2 := os.Rename(tmp, clean); err2 != nil {
					return written, false, err2
				}
			} else {
				return written, false, err
			}
		}
		_ = ChownToWebUser(clean, false)
		return written, true, nil
	}
	return written, false, nil
}

// ResetUploadParts 删除目标文件及对应 file_id 的残留临时文件，用于"覆盖"上传
func ResetUploadParts(dstPath, fileID string, totalSize int64) error {
	clean, err := SanitizePath(dstPath)
	if err != nil {
		return err
	}
	if fileID != "" {
		_ = os.Remove(uploadTmpPath(clean, fileID, totalSize))
	}
	if _, err := os.Stat(clean); err == nil {
		return os.Remove(clean)
	}
	return nil
}

// FileExists 判断文件是否存在
func FileExists(dstPath string) bool {
	clean, err := SanitizePath(dstPath)
	if err != nil {
		return false
	}
	info, err := os.Stat(clean)
	return err == nil && !info.IsDir()
}

// CleanUploadSubPath 校验并规范化 sub_path（防穿越）
func CleanUploadSubPath(sub string) (string, error) {
	sub = strings.Trim(sub, "/")
	if sub == "" {
		return "", nil
	}
	cleanSub := filepath.Clean(strings.ReplaceAll(sub, "\\", "/"))
	if cleanSub == "." || cleanSub == ".." || strings.HasPrefix(cleanSub, "../") || strings.HasPrefix(cleanSub, "/") {
		return "", errors.New("非法子路径")
	}
	return cleanSub, nil
}

// JoinUploadPath 安全拼接上传目标路径：path 为目录，subPath 为可选的已清洗相对子路径，filename 为文件名。
// 统一用 filepath.Join 消除重复/多余分隔符，避免手工 "path + \"/\" + x" 造成双斜杠。
func JoinUploadPath(path, cleanSub, filename string) string {
	if cleanSub == "" {
		return filepath.Join(path, filename)
	}
	return filepath.Join(path, cleanSub, filename)
}

// ProbeUploadOffset 字节级续传探针：返回已写入临时文件的字节数（续传起点 offset）
func ProbeUploadOffset(dstPath, fileID string, totalSize int64) (int64, error) {
	clean, err := SanitizePath(dstPath)
	if err != nil {
		return 0, err
	}
	if fileID == "" {
		return 0, nil
	}
	info, err := os.Stat(uploadTmpPath(clean, fileID, totalSize))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// Chmod 修改文件/目录权限，mode 形如 "755"
func Chmod(path, mode string) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil || m > 0o7777 {
		return errors.New("权限格式错误，请使用如 755 的八进制数字")
	}
	return os.Chmod(clean, os.FileMode(m))
}

// ZipFile 压缩文件或目录到 zipPath
// ZipFile 压缩单个文件/目录到 zipPath（兼容旧调用）
func ZipFile(path, zipPath string) error {
	return ZipFiles([]string{path}, zipPath)
}

// ZipFiles 压缩多个文件/目录到 zipPath（多选批量压缩）
func ZipFiles(paths []string, zipPath string) error {
	dst, err := SanitizePath(zipPath)
	if err != nil {
		return err
	}
	var srcs []string
	for _, p := range paths {
		s, err := SanitizePath(p)
		if err != nil {
			return err
		}
		// 防止把压缩包放进被压缩目录内导致自包含
		if strings.HasPrefix(dst, s+string(os.PathSeparator)) || dst == s {
			return errors.New("压缩包不能放在被压缩目录内部")
		}
		if _, err := os.Stat(s); err != nil {
			return fmt.Errorf("文件不存在: %s", s)
		}
		srcs = append(srcs, s)
	}
	if len(srcs) == 0 {
		return errors.New("没有可压缩的文件")
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer zw.Close()

	for _, src := range srcs {
		base := filepath.Base(src)
		if err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			name := base
			if rel != "." {
				name = base + "/" + strings.ReplaceAll(rel, "\\", "/")
			}
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(info.Mode())
			if info.IsDir() {
				header.Name += "/"
				_, err := zw.CreateHeader(header)
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				w, err := zw.CreateHeader(header)
				if err != nil {
					return err
				}
				_, err = w.Write([]byte(target))
				return err
			}
			w, err := zw.CreateHeader(header)
			if err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(w, f)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// UnzipFile 解压 zipPath 到 destDir
func UnzipFile(zipPath, destDir string) error {
	zipClean, err := SanitizePath(zipPath)
	if err != nil {
		return err
	}
	destClean, err := SanitizePath(destDir)
	if err != nil {
		return err
	}
	r, err := zip.OpenReader(zipClean)
	if err != nil {
		return errors.New("无法打开压缩包: " + err.Error())
	}
	defer r.Close()

	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		target := filepath.Join(destClean, name)
		// 防 zip slip：解压目标必须位于 destClean 内
		if target != destClean && !strings.HasPrefix(target, destClean+string(os.PathSeparator)) {
			return errors.New("压缩包包含非法路径: " + f.Name)
		}
		mode := f.Mode()
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return err
			}
			continue
		}
		// 防符号链接攻击：拒绝解压 symlink 条目。
		// 压缩包内的软链可指向任意路径（如 /etc），后续文件写入会借软链逃逸到包外，
		// 因此直接拒绝，不做"安全软链"白名单。
		if mode&os.ModeSymlink != 0 {
			return errors.New("压缩包包含符号链接，已拒绝: " + f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// 目标父目录若被预先放置的 symlink 指向包外（如 dest/evil -> /etc），
		// 此处解析真实路径并再次校验，确保实际落盘位置仍在 destClean 内。
		realDir, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil || (realDir != destClean && !strings.HasPrefix(realDir, destClean+string(os.PathSeparator))) {
			return errors.New("压缩包解压路径越界: " + f.Name)
		}
		// 目标文件本身若已存在且为 symlink，拒绝覆盖（防软链被改写指向敏感文件）
		if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("目标位置存在符号链接，已拒绝覆盖: " + f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	_ = ChownToWebUser(destClean, true)
	return nil
}

// RemoteDownload 从 URL 下载文件到目标目录
func RemoteDownload(rawURL, dstDir string) error {
	clean, err := SanitizePath(dstDir)
	if err != nil {
		return err
	}
	di, err := os.Stat(clean)
	if err != nil || !di.IsDir() {
		return errors.New("目标必须是已存在的目录")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return errors.New("URL 格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("仅支持 http/https 下载")
	}
	// SSRF 防护：解析目标主机 IP，拒绝内网 / 回环 / 链路本地地址，
	// 防止通过面板访问云元数据、内网服务等。
	ip, err := checkPublicHost(u.Host)
	if err != nil {
		return err
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" || name == "\\" {
		return errors.New("无法从 URL 解析文件名")
	}
	// 强制用已校验的公网 IP 建连（自定义 DialContext），
	// 避免 http.Client 二次解析域名时被 DNS 重绑定（TOCTOU）指向内网。
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	dialAddr := net.JoinHostPort(ip.String(), port)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, network, dialAddr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return errors.New("URL 格式错误")
	}
	req.Host = u.Host // 保留原始 Host 头，兼容按域名路由的虚拟主机
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("下载失败: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	dst := filepath.Join(clean, name)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return errors.New("写入文件失败: " + err.Error())
	}
	_ = ChownToWebUser(dst, false)
	return nil
}

// checkPublicHost SSRF 防护：解析 host 得到公网 IP 并返回。
// 仅允许公网 IP；解析失败（如域名暂时无法解析）时直接拒绝，保守处理。
// 返回的 IP 由调用方用于强制建连（防止 DNS 重绑定绕过）。
func checkPublicHost(hostport string) (net.IP, error) {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	// 去掉 IPv6 方括号（SplitHostPort 已处理，此处兜底）
	host = strings.Trim(host, "[]")

	// 先按字面 IP 判断
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return nil, errors.New("禁止下载内网地址")
		}
		return ip, nil
	}

	// 域名：解析所有 IP，全部必须是公网
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, errors.New("无法解析目标域名")
	}
	if len(ips) == 0 {
		return nil, errors.New("无法解析目标域名")
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, errors.New("禁止下载内网地址")
		}
	}
	return ips[0], nil
}

// isPrivateIP 判断 IP 是否为内网/回环/链路本地/组播/保留地址
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16(链路本地已含),
		// 127.0.0.0/8(回环已含), 100.64.0.0/10(CGNAT), 0.0.0.0/8
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168) ||
			(ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127) ||
			ip4[0] == 0
	}
	// IPv6：fc00::/7 (ULA)
	return len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc
}

// FormatBytes 字节数人类可读格式
func FormatBytes(n int64) string {
	const k = 1024
	if n < k {
		return strconv.FormatInt(n, 10) + " B"
	}
	if n < k*k {
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	if n < k*k*k {
		return fmt.Sprintf("%.1f MB", float64(n)/k/k)
	}
	return fmt.Sprintf("%.2f GB", float64(n)/k/k/k)
}

// Search 在 root 目录下递归查找文件名包含 keyword 的文件/目录，最多返回 max 个结果
func Search(root, keyword string, max int) ([]FileItem, error) {
	if max <= 0 || max > 500 {
		max = 200
	}
	clean, err := SanitizePath(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(clean)
	if err != nil {
		return nil, errors.New("目录不存在")
	}
	if !info.IsDir() {
		return nil, errors.New("必须是目录")
	}
	if strings.TrimSpace(keyword) == "" {
		return nil, errors.New("关键字不能为空")
	}
	// 用 find 命令高效搜索：-iname 不区分大小写，-printf 输出固定列
	args := []string{
		clean, "-iname", "*" + keyword + "*", "-printf",
		"%p\t%y\t%s\t%m\t%TY-%Tm-%Td %TH:%TM\n",
	}
	out, err := exec.Command("find", args...).Output()
	if err != nil {
		return nil, errors.New("搜索失败: " + err.Error())
	}
	var items []FileItem
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		path := parts[0]
		typeChar := parts[1]
		sizeStr := parts[2]
		modeStr := parts[3]
		modTime := parts[4]
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		isDir := typeChar == "d"
		mode := strings.TrimLeft(modeStr, "0")
		if mode == "" {
			mode = "0"
		}
		user, group := "", ""
		if fi, err := os.Stat(path); err == nil {
			mode = fmt.Sprintf("%o", fi.Mode().Perm())
		}
		// 属主属组依赖平台特定 syscall，跨平台简单置空（前端展示时显示为空即可）
		if user == "" {
			user = "-"
		}
		if group == "" {
			group = "-"
		}
		name := filepath.Base(path)
		dir := filepath.Dir(path)
		items = append(items, FileItem{
			Name:    name,
			Path:    path,
			IsDir:   isDir,
			Size:    size,
			Mode:    mode,
			Owner:   user + ":" + group,
			User:    user,
			Group:   group,
			ModTime: modTime,
			Dir:     dir,
		})
		if len(items) >= max {
			break
		}
	}
	return items, nil
}
