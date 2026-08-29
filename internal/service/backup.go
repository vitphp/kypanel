package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============================ 备份中心 ============================
// 支持网站备份、面板配置备份、数据库备份（复用 database_backup.go），
// 以及远程存储上传（S3 兼容 / 阿里云 OSS / FTP）。

// backupRoot 备份文件根目录
func backupRoot() string {
	return filepath.Join(config.Get().DataDir, "backup")
}

// backupDirFor 按类型返回备份子目录
func backupDirFor(t string) string {
	return filepath.Join(backupRoot(), t)
}

// ListBackupTasks 列出备份任务（按类型过滤，type 为空列出全部）
func ListBackupTasks(t string) []model.BackupTask {
	var tasks []model.BackupTask
	q := model.DB
	if t != "" {
		q = q.Where("type = ?", t)
	}
	q.Order("id desc").Limit(500).Find(&tasks)
	return tasks
}

// CreateBackup 创建一次备份（type: site/panel/database），返回任务记录
func CreateBackup(t, target string) (*model.BackupTask, error) {
	task := model.BackupTask{
		Type:    t,
		Target:  target,
		Storage: "local",
		Status:  "running",
	}
	if err := model.DB.Create(&task).Error; err != nil {
		return nil, err
	}

	// 同步执行（大文件备份会在后台，这里先同步，后续可改异步）
	fileName, size, err := doBackup(t, target)
	if err != nil {
		task.Status = "failed"
		task.Error = err.Error()
		model.DB.Save(&task)
		return &task, err
	}
	task.FileName = fileName
	task.Size = size
	task.Status = "success"
	model.DB.Save(&task)
	return &task, nil
}

// doBackup 执行实际备份，返回文件名和大小
func doBackup(t, target string) (string, int64, error) {
	dir := backupDirFor(t)
	_ = os.MkdirAll(dir, 0o755)
	ts := time.Now().Format("20060102_150405")

	var fileName, src string
	switch t {
	case "site":
		if target == "" {
			return "", 0, fmt.Errorf("网站名不能为空")
		}
		fileName = fmt.Sprintf("%s_%s.tar.gz", target, ts)
		src = filepath.Join(webRootBase, target)
		if _, err := os.Stat(src); err != nil {
			return "", 0, fmt.Errorf("网站目录不存在: %s", src)
		}
	case "panel":
		fileName = fmt.Sprintf("panel_%s.tar.gz", ts)
	case "database":
		// 数据库备份走 database_backup.go 逻辑
		return "", 0, fmt.Errorf("数据库备份请使用数据库页面的备份功能")
	default:
		return "", 0, fmt.Errorf("不支持的备份类型: %s", t)
	}

	dest := filepath.Join(dir, fileName)

	if t == "site" {
		// 打包网站目录 + nginx 配置
		if err := tarSite(src, dest); err != nil {
			return "", 0, err
		}
	} else if t == "panel" {
		if err := tarPanel(dest); err != nil {
			return "", 0, err
		}
	}

	info, err := os.Stat(dest)
	if err != nil {
		return "", 0, err
	}
	return fileName, info.Size(), nil
}

// tarSite 打包网站目录（含 Web 服务器配置）为 tar.gz
// 结构：wwwroot/<name>/... + conf/<name>.conf，恢复时可分别还原
func tarSite(src, dest string) error {
	baseName := filepath.Base(src)
	confFile := siteConfPathFor(baseName, WebServerType())

	// 临时目录：wwwroot/<name> + conf/<name>.conf
	tmpDir, err := os.MkdirTemp("", "lp_bak_site_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	wwwroot := filepath.Join(tmpDir, "wwwroot", baseName)
	if err := os.MkdirAll(wwwroot, 0o755); err != nil {
		return err
	}
	// 复制网站目录内容
	if _, err := ExecCommand(fmt.Sprintf("cp -a %s/. %s/ 2>/dev/null", shellQuote(src), shellQuote(wwwroot)), 600*time.Second); err != nil {
		return err
	}

	// 复制 Web 服务器配置（若有）
	if _, err := os.Stat(confFile); err == nil {
		confDir := filepath.Join(tmpDir, "conf")
		_ = os.MkdirAll(confDir, 0o755)
		_, _ = ExecCommand(fmt.Sprintf("cp -f %s %s/ 2>/dev/null", shellQuote(confFile), shellQuote(confDir)), 60*time.Second)
	}

	// 打包整个临时目录
	cmd := fmt.Sprintf("tar -czf %s -C %s . 2>/dev/null", shellQuote(dest), shellQuote(tmpDir))
	res, err := ExecCommand(cmd, 600*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("网站打包失败: %s", res.Stderr)
	}
	return nil
}

// tarPanel 打包面板核心数据（panel.db + config.json + 关键目录，排除 backup/logs）
func tarPanel(dest string) error {
	cfg := config.Get()
	dataDir := cfg.DataDir
	items := []string{
		"panel.db",
		"config.json",
		"ip2region.xdb",
		"database", // 面板管理的数据库数据（若有）
	}
	var args []string
	for _, it := range items {
		p := filepath.Join(dataDir, it)
		if _, err := os.Stat(p); err == nil {
			args = append(args, "-C", dataDir, it)
		}
	}
	if len(args) == 0 {
		return fmt.Errorf("面板数据目录为空")
	}
	cmd := fmt.Sprintf("tar -czf %s %s 2>/dev/null", shellQuote(dest), strings.Join(args, " "))
	res, err := ExecCommand(cmd, 300*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("面板备份失败: %s", res.Stderr)
	}
	return nil
}

// DeleteBackup 删除备份文件
func DeleteBackup(id uint) error {
	var task model.BackupTask
	if err := model.DB.First(&task, id).Error; err != nil {
		return err
	}
	path := filepath.Join(backupDirFor(task.Type), task.FileName)
	_ = os.Remove(path)
	return model.DB.Delete(&task).Error
}

// BackupFilePath 返回备份文件完整路径（用于下载/恢复/上传）
func BackupFilePath(id uint) (string, error) {
	var task model.BackupTask
	if err := model.DB.First(&task, id).Error; err != nil {
		return "", err
	}
	path := filepath.Join(backupDirFor(task.Type), task.FileName)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("备份文件不存在")
	}
	return path, nil
}

// RestoreBackup 恢复备份
// site: 解压回网站目录；panel: 解压回数据目录（危险操作，需谨慎）
func RestoreBackup(id uint) error {
	var task model.BackupTask
	if err := model.DB.First(&task, id).Error; err != nil {
		return err
	}
	path := filepath.Join(backupDirFor(task.Type), task.FileName)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("备份文件不存在")
	}
	switch task.Type {
	case "site":
		// tar 结构：wwwroot/<name>/... + conf/<name>.conf
		// 解压到 webRootBase，wwwroot/<name> 自动落到 /www/wwwroot/<name>
		if err := os.MkdirAll(webRootBase, 0o755); err != nil {
			return err
		}
		cmd := fmt.Sprintf("tar -xzf %s -C %s 2>/dev/null", shellQuote(path), shellQuote(webRootBase))
		res, err := ExecCommand(cmd, 600*time.Second)
		if err != nil {
			return err
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("网站恢复失败: %s", res.Stderr)
		}
		// 还原 Web 服务器配置（conf/<name>.conf -> 对应配置目录）
		confInBak := filepath.Join(webRootBase, "conf", "lp_"+task.Target+".conf")
		if _, err := os.Stat(confInBak); err == nil {
			confDest := siteConfPathFor(task.Target, WebServerType())
			_ = os.MkdirAll(filepath.Dir(confDest), 0o755)
			if _, err := ExecCommand(fmt.Sprintf("cp -f %s %s 2>/dev/null", shellQuote(confInBak), shellQuote(confDest)), 60*time.Second); err == nil {
				if WebServerType() == webApache {
					_ = apacheEnableSite(task.Target)
				}
				_ = webReload()
			}
		}
	case "panel":
		return fmt.Errorf("面板数据恢复属于高危操作，请手动处理备份文件")
	default:
		return fmt.Errorf("不支持恢复该类型的备份")
	}
	return nil
}

// ============================ 远程存储 ============================

// backupStorageSettingKey 远程存储配置的 Setting key
const backupStorageSettingKey = "backup_storage"

// getBackupStoragesRaw 读取远程存储配置列表（内部用，含明文密钥；落库为密文，此处解密）
func getBackupStoragesRaw() []model.BackupStorage {
	raw := model.GetSetting(backupStorageSettingKey)
	if raw == "" {
		return []model.BackupStorage{}
	}
	var list []model.BackupStorage
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return []model.BackupStorage{}
	}
	for i := range list {
		list[i].SecretKey = decryptSetting(list[i].SecretKey)
		list[i].Pass = decryptSetting(list[i].Pass)
	}
	return list
}

// encryptBackupStorages 保存前对敏感密钥字段加密落库（不修改原列表）
func encryptBackupStorages(list []model.BackupStorage) []model.BackupStorage {
	out := make([]model.BackupStorage, len(list))
	copy(out, list)
	for i := range out {
		out[i].SecretKey = encryptSetting(out[i].SecretKey)
		out[i].Pass = encryptSetting(out[i].Pass)
	}
	return out
}

// GetBackupStorages 读取远程存储配置列表（对外，密钥脱敏）。
// SecretKey / Pass 只保留后 4 位，防止面板数据被读取时泄露云厂商凭据。
func GetBackupStorages() []model.BackupStorage {
	list := getBackupStoragesRaw()
	for i := range list {
		list[i].SecretKey = MaskSecret(list[i].SecretKey)
		list[i].Pass = MaskSecret(list[i].Pass)
	}
	return list
}

// MaskSecret 脱敏密钥：非空时返回 "****" + 末尾 4 位（导出供 router 层对数据库密码等脱敏）
func MaskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

// restoreMaskedStorageValue 若新配置的密钥字段是掩码值（**** 开头），
// 则从旧配置还原真实值（编辑保存时前端会回填脱敏后的值）。
func restoreMaskedStorageValue(old, s *model.BackupStorage) {
	if old == nil || s == nil {
		return
	}
	if strings.HasPrefix(s.SecretKey, "****") {
		s.SecretKey = old.SecretKey
	}
	if strings.HasPrefix(s.Pass, "****") {
		s.Pass = old.Pass
	}
}

// SaveBackupStorages 保存远程存储配置（全量替换，掩码值自动还原）
func SaveBackupStorages(list []model.BackupStorage) error {
	oldList := getBackupStoragesRaw()
	oldByName := make(map[string]*model.BackupStorage, len(oldList))
	for i := range oldList {
		oldByName[oldList[i].Type+"\x00"+oldList[i].Name] = &oldList[i]
	}
	for i := range list {
		old, ok := oldByName[list[i].Type+"\x00"+list[i].Name]
		if !ok {
			if strings.HasPrefix(list[i].SecretKey, "****") || strings.HasPrefix(list[i].Pass, "****") {
				return fmt.Errorf("存储「%s」的密钥缺失，请重新输入完整密钥", list[i].Name)
			}
			continue
		}
		restoreMaskedStorageValue(old, &list[i])
	}
	b, _ := json.Marshal(encryptBackupStorages(list))
	return model.SetSetting(backupStorageSettingKey, string(b))
}

// AppendBackupStorage 追加一条远程存储（弹窗"添加"用），不去重，按 JSON 顺序累加。
// 用于 /backup/storages 单条添加模式。新存储必须提供完整密钥，不接受掩码值。
func AppendBackupStorage(s model.BackupStorage) error {
	if strings.HasPrefix(s.SecretKey, "****") || strings.HasPrefix(s.Pass, "****") {
		return fmt.Errorf("新存储请重新输入完整密钥")
	}
	list := getBackupStoragesRaw()
	list = append(list, s)
	b, _ := json.Marshal(encryptBackupStorages(list))
	return model.SetSetting(backupStorageSettingKey, string(b))
}

// UpdateBackupStorage 按 oldName 替换为新的存储配置（用于"编辑"）。
// 若 oldName 不存在则报错；替换时保留其在列表中的位置。
// 掩码值（前端回填）自动用旧配置还原。
func UpdateBackupStorage(oldName string, s model.BackupStorage) error {
	list := getBackupStoragesRaw()
	found := false
	for i := range list {
		if list[i].Name == oldName {
			restoreMaskedStorageValue(&list[i], &s)
			list[i] = s
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("存储「%s」不存在", oldName)
	}
	b, _ := json.Marshal(encryptBackupStorages(list))
	return model.SetSetting(backupStorageSettingKey, string(b))
}

// DeleteBackupStorage 按 name 删除一条远程存储
func DeleteBackupStorage(name string) error {
	list := getBackupStoragesRaw()
	out := make([]model.BackupStorage, 0, len(list))
	found := false
	for _, s := range list {
		if s.Name == name {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		return fmt.Errorf("存储「%s」不存在", name)
	}
	b, _ := json.Marshal(encryptBackupStorages(out))
	return model.SetSetting(backupStorageSettingKey, string(b))
}

// UploadBackupToStorage 将备份上传到指定远程存储
func UploadBackupToStorage(taskID uint, storageType string) error {
	return UploadBackupTaskToRemote(taskID, storageType)
}

// UploadBackupTaskToRemote 备份中心"创建备份"时使用：
// 接受 BackupTask ID + 存储名称（按 name 查找存储配置）上传本地备份文件到远程。
// 备份中心的备份记录表（BackupTask）与计划任务（cron 备份目录 + 文件前缀）虽然底层走的都是
// uploadS3/OSS SDK，但存储位置/前缀不同，所以分开两个函数，共享底层传输逻辑。
func UploadBackupTaskToRemote(taskID uint, storageName string) error {
	var task model.BackupTask
	if err := model.DB.First(&task, taskID).Error; err != nil {
		return err
	}
	path := filepath.Join(backupDirFor(task.Type), task.FileName)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("备份文件不存在")
	}

	storages := getBackupStoragesRaw()
	for _, st := range storages {
		if st.Name == storageName && st.Enabled {
			var err error
			switch st.Type {
			case "s3", "oss":
				err = uploadS3(st, path, task.FileName)
			case "ftp":
				err = uploadFTP(st, path, task.FileName)
			default:
				err = fmt.Errorf("不支持的存储类型: %s", st.Type)
			}
			if err != nil {
				return err
			}
			// 更新任务的存储标记
			task.Storage = storageName
			model.DB.Save(&task)
			return nil
		}
	}
	return fmt.Errorf("未找到启用的存储「%s」", storageName)
}

// uploadS3 上传文件到 S3 兼容存储（含阿里云 OSS，通过自定义 endpoint）
func uploadS3(st model.BackupStorage, filePath, remoteName string) error {
	if st.Endpoint == "" || st.Bucket == "" || st.AccessKey == "" || st.SecretKey == "" {
		return fmt.Errorf("S3 配置不完整（需 endpoint/bucket/accessKey/secretKey）")
	}
	// 先流式计算文件的 SHA256（阿里云 OSS 不接受 UNSIGNED-PAYLOAD，必须真实 payload hash）
	hashF, err := os.Open(filePath)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, hashF); err != nil {
		hashF.Close()
		return err
	}
	hashF.Close()
	payloadHash := hex.EncodeToString(h.Sum(nil))

	// 再打开文件作为请求体（流式上传，避免大文件读入内存）
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	remotePath := remoteName
	if st.Path != "" {
		remotePath = strings.Trim(st.Path, "/") + "/" + remoteName
	}
	url := strings.TrimRight(st.Endpoint, "/") + "/" + st.Bucket + "/" + remotePath

	req, err := http.NewRequest("PUT", url, f)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalURI := "/" + st.Bucket + "/" + remotePath
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	host := strings.TrimPrefix(strings.TrimPrefix(st.Endpoint, "https://"), "http://")
	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := "PUT\n" + canonicalURI + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash

	scope := dateStamp + "/" + st.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))

	kDate := hmacSHA256([]byte("AWS4"+st.SecretKey), dateStamp)
	kRegion := hmacSHA256(kDate, st.Region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := "AWS4-HMAC-SHA256 Credential=" + st.AccessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	req.Header.Set("Authorization", auth)

		// 上传专用 client：备份文件可能很大（GB 级），不能用全局 httpClient（10s 超时）。
	// 这里给 2 小时超时，覆盖大文件慢速上传场景。
	uploadClient := &http.Client{Timeout: 2 * time.Hour}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("上传失败: HTTP %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// uploadFTP 通过 curl 上传到 FTP
func uploadFTP(st model.BackupStorage, filePath, remoteName string) error {
	if st.Host == "" || st.User == "" {
		return fmt.Errorf("FTP 配置不完整（需 host/user/pass）")
	}
	port := st.Port
	if port == 0 {
		port = 21
	}
	remoteDir := strings.Trim(st.Path, "/")
	base := fmt.Sprintf("ftp://%s:%d", st.Host, port)
	// 先创建远程目录
	_, _ = ExecCommand(fmt.Sprintf("curl -s --ftp-create-dirs -u %s:%s %s/%s/ -o /dev/null", shellQuote(st.User), shellQuote(st.Pass), base, remoteDir), 30*time.Second)
	// 上传
	cmd := fmt.Sprintf("curl -s -T %s -u %s:%s %s/%s/%s", shellQuote(filePath), shellQuote(st.User), shellQuote(st.Pass), base, remoteDir, remoteName)
	res, err := ExecCommand(cmd, 600*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("FTP 上传失败: %s", res.Stderr)
	}
	return nil
}

// ============================ 工具函数 ============================

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// s3SignedRequest 构造并发送一个带 AWS SigV4 签名的 S3/OSS 请求。
// method: GET/PUT/DELETE；pathAndQuery: 形如 "/bucket/key?list-type=2&prefix=xxx"；
// body: 可为 nil（GET/DELETE 无 body）。返回响应体（若出错返回 error）。
// 复用于 uploadS3 / listS3Objects / deleteS3Object，统一签名逻辑。
func s3SignedRequest(st model.BackupStorage, method, pathAndQuery string, body []byte) ([]byte, int, error) {
	endpoint := strings.TrimRight(st.Endpoint, "/")
	url := endpoint + pathAndQuery

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	// payload hash：GET/DELETE 无 body 时用空串的 SHA256
	payloadHash := "UNSIGNED-PAYLOAD"
	if method == "GET" || method == "DELETE" {
		payloadHash = sha256Hex([]byte{})
	} else if body != nil {
		payloadHash = sha256Hex(body)
	}
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// 解析 pathAndQuery 里的 path 部分（不含 query）用于 canonical URI
	pathOnly := pathAndQuery
	if i := strings.IndexByte(pathAndQuery, '?'); i >= 0 {
		pathOnly = pathAndQuery[:i]
	}
	canonicalURI := pathOnly

	host := strings.TrimPrefix(strings.TrimPrefix(st.Endpoint, "https://"), "http://")
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := method + "\n" + canonicalURI + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash

	scope := dateStamp + "/" + st.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))

	kDate := hmacSHA256([]byte("AWS4"+st.SecretKey), dateStamp)
	kRegion := hmacSHA256(kDate, st.Region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := "AWS4-HMAC-SHA256 Credential=" + st.AccessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	req.Header.Set("Authorization", auth)

	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// BackupTaskListSorted 返回排序后的备份任务（按时间倒序）
func BackupTaskListSorted(t string) []model.BackupTask {
	tasks := ListBackupTasks(t)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks
}
