package service

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kypanel/internal/model"
)

// cronBackupDirFor 根据任务的模板类型与 target 计算该任务对应的备份目录。
// 返回 (目录绝对路径, 文件名前缀, nil)；不支持的模板返回错误。
// 函数名加 cron 前缀避免与 service/backup.go 中的 backupDirFor 冲突。
func cronBackupDirFor(t *model.Cron) (string, string, error) {
	switch t.Template {
	case "backup_site":
		if strings.TrimSpace(t.SiteName) == "" {
			return "", "", fmt.Errorf("该备份任务缺少站点信息")
		}
		return "/www/backup/site", t.SiteName, nil
	case "backup_db":
		if strings.TrimSpace(t.Database) == "" {
			return "", "", fmt.Errorf("该备份任务缺少数据库信息")
		}
		return "/www/backup/database", t.Database, nil
	case "backup_db_incremental":
		if strings.TrimSpace(t.Database) == "" {
			return "", "", fmt.Errorf("该备份任务缺少数据库信息")
		}
		return "/www/backup/database_incremental/" + t.Database, t.Database, nil
	case "backup_dir":
		dir := filepath.Clean(t.Dir)
		if dir == "" || dir == "/" || dir == "." || !strings.HasPrefix(dir, "/") {
			return "", "", fmt.Errorf("该备份任务缺少目录信息")
		}
		baseName := strings.ReplaceAll(strings.TrimPrefix(dir, "/"), "/", "_")
		return "/www/backup/dir", baseName, nil
	default:
		return "", "", fmt.Errorf("该任务不是备份类型")
	}
}

// ListCronBackupFiles 列出某个备份任务生成的备份文件。
// 返回按修改时间倒序的文件信息列表（最新的在前面）。
func ListCronBackupFiles(taskID uint) ([]map[string]any, error) {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	dir, _, err := cronBackupDirFor(&t)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type item struct {
		Name    string
		Size    int64
		ModTime int64
	}
	var list []item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, item{Name: info.Name(), Size: info.Size(), ModTime: info.ModTime().Unix()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ModTime > list[j].ModTime })
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		out = append(out, map[string]any{
			"name":  it.Name,
			"size":  it.Size,
			"mtime": it.ModTime,
			"path":  filepath.Join(dir, it.Name),
		})
	}
	return out, nil
}

// DeleteCronBackupFile 删除指定任务下的某个备份文件（仅允许删除该任务目录下的文件，
// 防止通过伪造路径删除系统其他文件）。
func DeleteCronBackupFile(taskID uint, name string) error {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在")
	}
	dir, prefix, err := cronBackupDirFor(&t)
	if err != nil {
		return err
	}
	cleanName := filepath.Base(name) // 防路径穿越
	if strings.ContainsAny(cleanName, "/\\") || cleanName == "." || cleanName == ".." {
		return fmt.Errorf("非法文件名")
	}
	if prefix != "" && !strings.HasPrefix(cleanName, prefix+"_") {
		return fmt.Errorf("该文件不属于此任务")
	}
	return os.Remove(filepath.Join(dir, cleanName))
}

// ServeCronBackupDownload 将备份文件流式写入 http.ResponseWriter。
// 包含路径安全校验（同 DeleteCronBackupFile）。
func ServeCronBackupDownload(taskID uint, name string, w http.ResponseWriter) error {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在")
	}
	dir, prefix, err := cronBackupDirFor(&t)
	if err != nil {
		return err
	}
	cleanName := filepath.Base(name)
	if strings.ContainsAny(cleanName, "/\\") || cleanName == "." || cleanName == ".." {
		return fmt.Errorf("非法文件名")
	}
	if prefix != "" && !strings.HasPrefix(cleanName, prefix+"_") {
		return fmt.Errorf("该文件不属于此任务")
	}
	full := filepath.Join(dir, cleanName)
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+cleanName+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	_, err = io.Copy(w, f)
	return err
}

// CronBackupDir 公开接口：返回某个备份任务的目录绝对路径。
func CronBackupDir(taskID uint) (string, error) {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return "", fmt.Errorf("任务不存在")
	}
	dir, _, err := cronBackupDirFor(&t)
	return dir, err
}

// CronBackupPrefix 返回备份文件名前缀（用于服务端上传时定位匹配文件）
func CronBackupPrefix(taskID uint) (string, error) {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return "", fmt.Errorf("任务不存在")
	}
	_, prefix, err := cronBackupDirFor(&t)
	return prefix, err
}

// UploadCronBackupToRemote 将 cron 任务的最新备份文件上传到该任务指定的远程存储。
// 调用流程：查询 cron.TargetName 找到存储配置 -> 找出任务目录下最新的备份文件 -> uploadS3 -> 清理远端多余。
// 仅在该任务 Template 为 backup_* 且 TargetType=remote 时有效；否则返回 nil（无操作）。
func UploadCronBackupToRemote(taskID uint) error {
	var t model.Cron
	if err := model.DB.First(&t, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在")
	}
	if t.TargetType != "remote" || strings.TrimSpace(t.TargetName) == "" {
		return nil // 未配置远程目标，不算错误
	}
	// 找存储配置
	storages := getBackupStoragesRaw()
	var st model.BackupStorage
	found := false
	for _, s := range storages {
		if s.Name == t.TargetName && s.Enabled {
			st = s
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("未找到启用的远程存储「%s」", t.TargetName)
	}
	if st.Type != "s3" && st.Type != "oss" {
		return fmt.Errorf("暂仅支持 s3/oss 类型远程存储，当前存储类型: %s", st.Type)
	}
	// 找任务目录下最新的备份文件
	dir, _, err := cronBackupDirFor(&t)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("任务备份目录不存在: %s", dir)
		}
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("任务备份目录为空，没有可上传的文件")
	}
	// 按 mtime 选最新
	type cand struct {
		Info os.FileInfo
	}
	var list []cand
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, cand{info})
	}
	if len(list) == 0 {
		return fmt.Errorf("没有可上传的备份文件")
	}
	// 排序：最新的在前
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Info.ModTime().After(list[i].Info.ModTime()) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	latest := list[0].Info
	localPath := filepath.Join(dir, latest.Name())

	// 上传
	if err := uploadS3(st, localPath, latest.Name()); err != nil {
		return fmt.Errorf("上传到 OSS 失败: %w", err)
	}

	// 清理远端多余文件（按 RemoteKeep）
	keep := t.RemoteKeep
	if keep <= 0 {
		keep = t.Keep
	}
	if keep > 0 {
		if err := cleanupRemotePrefix(st, t.TargetName, latest.Name(), keep); err != nil {
			// 不致命：仅记录
			slog.Warn("清理远端多余备份失败", "err", err)
		}
	}
	return nil
}

// cleanupRemotePrefix 清理远端存储上以指定前缀开头的旧文件（保留最新 keep 个）。
// 当前仅对 S3/OSS 实现：用 List Objects 列出该 prefix 下的对象，按 LastModified 排序，删除多余的。
func cleanupRemotePrefix(st model.BackupStorage, storageName, latestName string, keep int) error {
	// 构造 prefix：拼接 storage.Path 和备份文件前缀
	prefix := ""
	if st.Path != "" {
		prefix = strings.Trim(st.Path, "/") + "/"
	}
	// 提取备份文件名前缀（latestName 形如 "ppp.n.05v.cn_20260826_210735.tar.gz"，前缀是 "ppp.n.05v.cn_"）
	basePrefix := strings.TrimSuffix(latestName, extractDateSuffix(latestName))
	prefix += basePrefix

	// 列远端对象
	objects, err := listS3Objects(st, prefix)
	if err != nil {
		return err
	}
	if len(objects) <= keep {
		return nil
	}
	// 按 LastModified 升序（最旧的在前），删除 keep 之后的
	extras := objects[:len(objects)-keep]
	for _, obj := range extras {
		// 不删除刚上传的那个
		if obj.Key == latestName || strings.HasSuffix(obj.Key, latestName) {
			continue
		}
		if err := deleteS3Object(st, obj.Key); err != nil {
			slog.Warn("删除远端文件失败", "key", obj.Key, "err", err)
		}
	}
	return nil
}

// extractDateSuffix 从文件名提取日期后缀 "_20260826_210735"（用于剥离得到前缀）
func extractDateSuffix(name string) string {
	// 取最后一个 "_" 之前的部分（含）作为前缀
	idx := strings.LastIndex(name, "_")
	if idx < 0 {
		return name
	}
	// 找第二个 "_"（日期格式有 2 个下划线：Ymd_HMS）
	first := strings.Index(name, "_")
	second := strings.Index(name[first+1:], "_")
	if second < 0 {
		return name
	}
	second += first + 1
	// 包含到第二个下划线（prefix = "name_Ymd_"）
	return name[:second+1]
}

// s3Object 表示 ListObjectsV2 响应中的一个对象
type s3Object struct {
	Key          string
	LastModified string
}

// s3ListResult 解析 ListObjectsV2 响应
type s3ListResult struct {
	XMLName xml.Name `xml:"ListBucketResult"`
	Objects []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
	} `xml:"Contents"`
}

// listS3Objects 列出 S3/OSS 上指定 prefix 下的对象（按 LastModified 升序，最旧在前）。
func listS3Objects(st model.BackupStorage, prefix string) ([]s3Object, error) {
	query := url.Values{}
	query.Set("list-type", "2")
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	pathAndQuery := "/" + st.Bucket + "/?" + query.Encode()

	body, status, err := s3SignedRequest(st, "GET", pathAndQuery, nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("ListObjects 失败: HTTP %d %s", status, string(body))
	}
	var res s3ListResult
	if err := xml.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("解析 ListObjects 响应失败: %w", err)
	}
	objs := make([]s3Object, 0, len(res.Objects))
	for _, o := range res.Objects {
		objs = append(objs, s3Object{Key: o.Key, LastModified: o.LastModified})
	}
	// 按 LastModified 升序（最旧在前），保证删除时从最旧的开始
	sort.Slice(objs, func(i, j int) bool { return objs[i].LastModified < objs[j].LastModified })
	return objs, nil
}

// deleteS3Object 删除 S3/OSS 上的单个对象。
func deleteS3Object(st model.BackupStorage, key string) error {
	pathAndQuery := "/" + st.Bucket + "/" + url.PathEscape(key)
	body, status, err := s3SignedRequest(st, "DELETE", pathAndQuery, nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("删除对象失败: HTTP %d %s", status, string(body))
	}
	_ = body
	return nil
}