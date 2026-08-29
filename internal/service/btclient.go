package service

// ==================== 对端面板 API 客户端 ====================
// 通过对端面板官方开放 API（面板设置 → API 接口 → 开启并生成接口密钥）操作对端面板，
// 用于「网站搬家 → 迁出到对端面板」场景。
//
// 鉴权规则（对端面板官方）：
//   request_token = md5(request_time + md5(api_sk))
//   所有请求为 POST form，参数中必须带 request_token 与 request_time。
//
// 注：对端面板 API 部分接口名/参数在不同版本略有差异，接口不通时按目标对端面板版本调整。

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BTClient 对端面板 API 客户端
type BTClient struct {
	BaseURL string
	ApiSK   string
	// apiStyle 面板 API 风格：
	//   ""      未探测（每次请求先试旧格式，404 自动切新格式并缓存）
	//   "legacy" 旧格式  /class?action=xxx
	//   "new"    新版格式 /api/class/xxx（对端面板 9.x 起官方推荐路径）
	apiStyle string
	client   *http.Client
}

// NewBTClient 创建对端面板客户端。baseURL 示例：http://1.2.3.4:8888
func NewBTClient(baseURL, apiSK string) *BTClient {
	return &BTClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		ApiSK:   strings.TrimSpace(apiSK),
		client: &http.Client{
			Timeout: 600 * time.Second,
			Transport: &http.Transport{
				// 对端面板默认使用自签名 HTTPS 证书，必须跳过证书校验才能连接
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func btMD5(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// truncateLog 截断过长的日志内容，防止面板日志被大响应刷屏
func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…(总%d字符)", len(s))
}

// btRequestRaw 发起对端面板 API 请求并返回原始响应体（用于数组/纯文本等非对象响应）
func (c *BTClient) btRequestRaw(class, action string, params url.Values) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	params.Set("request_token", btMD5(ts+btMD5(c.ApiSK)))
	params.Set("request_time", ts)
	bodyStr := params.Encode()
	// 日志脱敏：不打印 request_token 明文
	logBody := strings.Replace(bodyStr, "request_token="+params.Get("request_token"), "request_token=***", 1)

	var lastErr error
	for _, style := range c.apiStyleOrder() {
		var u string
		if style == "new" {
			u = fmt.Sprintf("%s/api/%s/%s", c.BaseURL, class, action)
		} else {
			u = fmt.Sprintf("%s/%s?action=%s", c.BaseURL, class, action)
		}
		slog.Info("对端面板API请求", "url", u, "style", style, "body", truncateLog(logBody, 800))
		req, err := http.NewRequest("POST", u, strings.NewReader(bodyStr))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "kypanel-migrate/1.0")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("请求对端面板失败: %w", err)
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		slog.Info("对端面板API响应", "url", u, "status", resp.StatusCode, "body", truncateLog(strings.TrimSpace(string(body)), 500))
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("对端面板返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			if !strings.Contains(lastErr.Error(), "HTTP 404") {
				return nil, lastErr // 非 404 错误（鉴权失败等），不再切换格式
			}
			continue // 404 → 尝试另一种 API 格式
		}
		c.cacheAPIStyle(style)
		return body, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("对端面板请求失败: 无法访问 %s/%s", class, action)
}

// apiStyleOrder 返回本次请求尝试的 API 风格顺序（已探测则只试命中风格）
func (c *BTClient) apiStyleOrder() []string {
	if c.apiStyle == "new" {
		return []string{"new", "legacy"}
	}
	if c.apiStyle == "legacy" {
		return []string{"legacy", "new"}
	}
	return []string{"legacy", "new"}
}

// cacheAPIStyle 缓存命中风格（首次成功时记录，后续请求直接走该风格）
func (c *BTClient) cacheAPIStyle(style string) {
	if c.apiStyle == "" {
		c.apiStyle = style
	}
}

// btRequest 通用请求：POST form 到 /<class>?action=<action>
// 对端面板接口通常返回 {status: true/false, msg: "..."} 对象；返回数组/纯文本的接口请走 btRequestRaw。
func (c *BTClient) btRequest(class, action string, params url.Values) (map[string]any, error) {
	body, err := c.btRequestRaw(class, action, params)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("对端面板接口响应解析失败: %w", err)
	}
	if ok, exists := m["status"].(bool); exists && !ok {
		msg, _ := m["msg"].(string)
		if msg == "" {
			msg = string(body)
		}
		return nil, errors.New("对端面板接口失败: " + msg)
	}
	return m, nil
}

// ---------------- 网站 ----------------

// btParseDataList 兼容解析对端面板返回的列表数据（data 字段可能是 JSON 字符串或数组）
func btParseDataList(res map[string]any) ([]map[string]any, error) {
	var list []map[string]any
	if s, ok := res["data"].(string); ok && s != "" {
		if err := json.Unmarshal([]byte(s), &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	raw, _ := json.Marshal(res["data"])
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SiteList 获取对端面板网站列表（/data?action=getData&table=sites）。
// 老版本面板若无此接口，则回退到 /site?action=GetSiteList。
func (c *BTClient) SiteList() ([]map[string]any, error) {
	params := url.Values{}
	params.Set("table", "sites")
	params.Set("limit", "1000")
	res, err := c.btRequest("data", "getData", params)
	if err != nil {
		// 回退到面板内部接口（老版本对端面板）
		res2, err2 := c.btRequest("site", "GetSiteList", nil)
		if err2 != nil {
			return nil, err
		}
		res = res2
	}
	return btParseDataList(res)
}

// AddSite 在对端面板创建网站
// domains: 逗号分隔的域名列表；path: 网站目录（空则对端面板自动生成 /www/wwwroot/xxx）
func (c *BTClient) AddSite(siteName, domains, path, phpVersion string) (map[string]any, error) {
	// 对端面板官方要求 webname 为 JSON 对象格式（新版不再接受 ["域名","备注"] 数组）：
	//   {"domain":"主域名","domainlist":["域名1","域名2"],"count":2}
	domList := make([]string, 0)
	for _, d := range strings.Split(domains, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			domList = append(domList, d)
		}
	}
	if len(domList) == 0 {
		domList = []string{siteName}
	}
	// 对端面板会把 domain 和 domainlist 合并绑定域名，为避免主域名重复，domainlist 只保留附加域名
	domainListForAPI := make([]string, 0)
	if len(domList) > 1 {
		domainListForAPI = domList[1:]
	}
	wb, _ := json.Marshal(map[string]any{
		"domain":     domList[0],
		"domainlist": domainListForAPI,
		"count":      len(domList),
	})
	params := url.Values{}
	params.Set("webname", string(wb))
	params.Set("type_id", "0")
	params.Set("type", "PHP")
	params.Set("version", phpVersion) // 如 "74" / "80"
	params.Set("ps", "由 kypanel 网站搬家迁入")
	params.Set("ftp", "false")
	params.Set("sql", "false")
	params.Set("port", "80")
	if path != "" {
		params.Set("path", path)
	}
	return c.btRequest("site", "AddSite", params)
}

// PHPVersionList 获取对端面板可用的 PHP 版本
// 对端面板 /site?action=GetPHPVersion 存在多种返回格式（不同版本差异较大）：
//   1) 字符串数组         ["73","74","80"]
//   2) 对象数组           [{"version":"74","name":"PHP-74","status":true}, ...]
//   3) 对象包裹           {"data": [...]} 或 {"data": "74,80"}
//   4) 映射               {"74":"7.4", ...}
func (c *BTClient) PHPVersionList() ([]string, error) {
	body, err := c.btRequestRaw("site", "GetPHPVersion", nil)
	if err != nil {
		return nil, err
	}
	extract := func(v any) string {
		switch t := v.(type) {
		case string:
			return strings.TrimSpace(t)
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		case json.Number:
			return t.String()
		}
		return ""
	}
	var list []string
	// 1) 纯字符串数组
	if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 {
		return list, nil
	}
	// 2) 对象数组 [{"version":..,"name":..}, ...]
	var objs []map[string]any
	if err := json.Unmarshal(body, &objs); err == nil {
		for _, o := range objs {
			if v := extract(o["version"]); v != "" {
				list = append(list, v)
			}
		}
		if len(list) > 0 {
			return list, nil
		}
	}
	// 3) 对象：取 data 字段
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		raw, _ := json.Marshal(m["data"])
		if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
			return list, nil
		}
		if err := json.Unmarshal(raw, &objs); err == nil {
			for _, o := range objs {
				if v := extract(o["version"]); v != "" {
					list = append(list, v)
				}
			}
			if len(list) > 0 {
				return list, nil
			}
		}
		// 4) data 为 {"74":"7.4",...} 映射
		var mm map[string]any
		if err := json.Unmarshal(raw, &mm); err == nil {
			for k := range mm {
				list = append(list, k)
			}
			return list, nil
		}
	}
	return nil, fmt.Errorf("对端面板PHP版本接口响应无法解析: %s", truncateLog(string(body), 200))
}

// ---------------- 数据库 ----------------

// AddDatabase 在对端面板创建数据库
func (c *BTClient) AddDatabase(name, user, password string) (map[string]any, error) {
	// 参数名严格按对端面板官方 AddDatabase 接口：
	// name / db_user / password / address / codeing / sid
	params := url.Values{}
	params.Set("name", name)
	params.Set("db_user", user)
	params.Set("password", password)
	params.Set("address", "%")
	params.Set("codeing", "utf8mb4")
	params.Set("sid", "0")
	return c.btRequest("database", "AddDatabase", params)
}

// DatabaseList 获取对端面板数据库列表（含 id）
func (c *BTClient) DatabaseList() ([]map[string]any, error) {
	params := url.Values{}
	params.Set("table", "databases")
	params.Set("limit", "1000")
	res, err := c.btRequest("data", "getData", params)
	if err != nil {
		res2, err2 := c.btRequest("database", "GetDatabases", nil)
		if err2 != nil {
			return nil, err
		}
		res = res2
	}
	return btParseDataList(res)
}

// ImportDatabase 导入 SQL 到对端面板指定数据库（id 为数据库 ID，sqlPath 为对端面板服务器上的 SQL 文件路径）
func (c *BTClient) ImportDatabase(dbID string, sqlPath string) (map[string]any, error) {
	params := url.Values{}
	params.Set("id", dbID)
	params.Set("file_name", sqlPath)
	return c.btRequest("database", "InputSql", params)
}

// ---------------- FTP ----------------

// AddFtpUser 在对端面板创建 FTP 账号
func (c *BTClient) AddFtpUser(username, password, path string) (map[string]any, error) {
	params := url.Values{}
	params.Set("ftp_username", username)
	params.Set("ftp_password", password)
	params.Set("ftp_path", path)
	return c.btRequest("ftp", "AddUser", params)
}

// ---------------- 文件 ----------------

// Upload 上传本地文件到对端面板服务器。
// 使用对端面板文件管理器的真实分片接口 /files?action=upload：
//   multipart 字段：f_path=目标目录, f_name=文件名, f_size=文件总大小,
//                    f_start=本分片起始偏移, blob=分片内容
// 对端面板按 f_start 偏移写入，自动合并为完整文件；前几片返回已写累计字节数，
// 最后一片返回 {"status": true, "msg": "上传成功!"}。
// 单片大小限制约 8MB（10MB 会连接中断），这里取 4MB 保证兼容。
func (c *BTClient) Upload(localPath, remotePath string, onProgress func(done, total int64)) error {
	return c.uploadChunked(localPath, remotePath, onProgress)
}

// btChunkSize 分片大小：对端面板 action=upload 单片实测 8MB 以内稳定成功
const btChunkSize = 4 * 1024 * 1024

// uploadChunked 分片上传文件到对端面板（对端面板文件管理器真实上传接口）
func (c *BTClient) uploadChunked(localPath, remotePath string, onProgress func(done, total int64)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	total := fi.Size()
	if total == 0 {
		return fmt.Errorf("文件为空: %s", localPath)
	}
	dir := filepath.Dir(remotePath)
	name := filepath.Base(remotePath)

	var start int64
	for start < total {
		chunkLen := total - start
		if chunkLen > btChunkSize {
			chunkLen = btChunkSize
		}
		buf := make([]byte, chunkLen)
		if _, err := io.ReadFull(f, buf); err != nil {
			return fmt.Errorf("读取分片失败: %w", err)
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("f_path", dir)
		_ = mw.WriteField("f_name", name)
		_ = mw.WriteField("f_size", strconv.FormatInt(total, 10))
		_ = mw.WriteField("f_start", strconv.FormatInt(start, 10))
		fw, err := mw.CreateFormFile("blob", name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(buf); err != nil {
			return err
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		_ = mw.WriteField("request_time", ts)
		_ = mw.WriteField("request_token", btMD5(ts+btMD5(c.ApiSK)))
		if err := mw.Close(); err != nil {
			return err
		}

		u := fmt.Sprintf("%s/files?action=upload", c.BaseURL)
		req, err := http.NewRequest("POST", u, &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("User-Agent", "kypanel-migrate/1.0")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		resp, err := c.client.Do(req)
		if err != nil {
			return fmt.Errorf("上传到对端面板失败（分片 %d/%d）: %w", start/btChunkSize+1, (total+btChunkSize-1)/btChunkSize, err)
		}
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		respBody := strings.TrimSpace(string(rb))
		slog.Info("对端面板文件上传分片响应", "start", start, "len", len(buf), "status", resp.StatusCode, "body", truncateLog(respBody, 300))
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("上传到对端面板失败（HTTP %d, 分片 %d/%d）: %s", resp.StatusCode, start/btChunkSize+1, (total+btChunkSize-1)/btChunkSize, respBody)
		}
		// 解析响应：{"status": true/false} 或纯数字（已写累计字节）
		var m map[string]any
		if err := json.Unmarshal(rb, &m); err == nil {
			if ok, exists := m["status"].(bool); exists && !ok {
				msg, _ := m["msg"].(string)
				return errors.New("上传到对端面板失败: " + msg)
			}
		}
		start += chunkLen
		if onProgress != nil {
			onProgress(start, total)
		}
	}
	return nil
}

// Unzip 在对端面板服务器上解压压缩包到指定目录
func (c *BTClient) Unzip(zipPath, destDir string) error {
	params := url.Values{}
	params.Set("sfile", zipPath)
	params.Set("dfile", destDir)
	params.Set("type", "tar.gz")
	params.Set("z_file", filepath.Base(zipPath))
	_, err := c.btRequest("files", "UnZip", params)
	return err
}

// ZipDir 在对端面板服务器上压缩目录/文件到指定压缩包（files?action=Zip）。
// sfile: 源绝对路径（目录）；dfile: 目标目录（必须已存在，通常 /www/backup）；
// zfile: 压缩包文件名；zipType: "zip" 或 "tar.gz"。压缩结果位于 dfile/zfile。
func (c *BTClient) ZipDir(sfile, dfile, zfile, zipType string) error {
	params := url.Values{}
	params.Set("sfile", sfile)
	params.Set("dfile", dfile)
	params.Set("type", zipType)
	params.Set("z_file", zfile)
	_, err := c.btRequest("files", "Zip", params)
	return err
}

// DeleteFile 删除对端面板服务器上的文件（files?action=DeleteFile）
func (c *BTClient) DeleteFile(path string) error {
	params := url.Values{}
	params.Set("name", path)
	_, err := c.btRequest("files", "DeleteFile", params)
	return err
}

// ---------------- 备份与下载 ----------------

// btGetBackupList 通过 /data?action=getData&table=backup 获取备份列表（type: sites/databases，id: 站点或数据库 id）
func (c *BTClient) btGetBackupList(table, id string) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("table", table)
	params.Set("limit", "500")
	if id != "" {
		params.Set("id", id)
	}
	res, err := c.btRequest("data", "getData", params)
	if err != nil {
		return nil, err
	}
	return btParseDataList(res)
}

// SiteBackupList 获取网站备份列表（/data?action=getData&table=backup&id=网站 id）
func (c *BTClient) SiteBackupList(siteID string) ([]map[string]any, error) {
	list, err := c.btGetBackupList("backup", siteID)
	if err == nil {
		return list, nil
	}
	// 老版本回退：site?action=GetSiteBackup
	params := url.Values{}
	params.Set("id", siteID)
	res2, err2 := c.btRequest("site", "GetSiteBackup", params)
	if err2 != nil {
		return nil, err
	}
	return btParseDataList(res2)
}

// DatabaseBackupNow 立即备份数据库（database?action=ToBackup，id=数据库 id）
func (c *BTClient) DatabaseBackupNow(dbID string) error {
	params := url.Values{}
	params.Set("id", dbID)
	_, err := c.btRequest("database", "ToBackup", params)
	return err
}

// DatabaseBackupList 获取数据库备份列表（/data?action=getData&table=backup&id=数据库 id）
func (c *BTClient) DatabaseBackupList(dbID string) ([]map[string]any, error) {
	list, err := c.btGetBackupList("backup", dbID)
	if err == nil {
		return list, nil
	}
	// 老版本回退：database?action=GetDatabaseBackup
	params := url.Values{}
	params.Set("id", dbID)
	res2, err2 := c.btRequest("database", "GetDatabaseBackup", params)
	if err2 != nil {
		return nil, err
	}
	return btParseDataList(res2)
}

// FtpUserList 获取对端面板 FTP 账号列表（/data?action=getData&table=ftps）
func (c *BTClient) FtpUserList() ([]map[string]any, error) {
	params := url.Values{}
	params.Set("table", "ftps")
	params.Set("limit", "1000")
	res, err := c.btRequest("data", "getData", params)
	if err != nil {
		res2, err2 := c.btRequest("ftp", "GetUsers", nil)
		if err2 != nil {
			return nil, err
		}
		res = res2
	}
	return btParseDataList(res)
}

// DownloadFile 从对端面板服务器下载文件（files?action=Download）到本地 destPath。
// 对端面板接口返回文件二进制流；若返回 JSON 错误则解析并返回错误。
func (c *BTClient) DownloadFile(remotePath, destPath string) error {
	params := url.Values{}
	params.Set("filename", remotePath)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	params.Set("request_token", btMD5(ts+btMD5(c.ApiSK)))
	params.Set("request_time", ts)

	u := fmt.Sprintf("%s/files?action=Download", c.BaseURL)
	req, err := http.NewRequest("POST", u, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "kypanel-migrate/1.0")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求对端面板下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("对端面板下载失败（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "json") || strings.HasPrefix(ct, "text/plain") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			if ok, exists := m["status"].(bool); exists && !ok {
				msg, _ := m["msg"].(string)
				return errors.New("对端面板下载失败: " + msg)
			}
		}
		return errors.New("对端面板下载失败: " + strings.TrimSpace(string(body)))
	}
	_ = os.MkdirAll(filepath.Dir(destPath), 0o755)
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

// ---------------- 删除（迁移覆盖用） ----------------

// DeleteSite 删除对端面板网站。id 为网站 ID，webname 为对端面板网站 webname JSON 字符串（从 SiteList 获取）。
func (c *BTClient) DeleteSite(id, webname string) (map[string]any, error) {
	params := url.Values{}
	params.Set("id", id)
	params.Set("webname", webname)
	return c.btRequest("site", "DeleteSite", params)
}

// DeleteDatabase 删除对端面板数据库（id 为数据库 ID）
func (c *BTClient) DeleteDatabase(id, name string) (map[string]any, error) {
	params := url.Values{}
	params.Set("id", id)
	params.Set("name", name)
	return c.btRequest("database", "DeleteDatabase", params)
}

// DeleteFtpUser 删除对端面板 FTP 账号
func (c *BTClient) DeleteFtpUser(username string) (map[string]any, error) {
	params := url.Values{}
	params.Set("ftp_username", username)
	return c.btRequest("ftp", "DeleteUser", params)
}

// ---------------- 迁移前检测 ----------------

// BTPrecheckRequest 迁出到对端面板前的冲突检测请求
type BTPrecheckRequest struct {
	BTURL     string   `json:"bt_url"`
	BTSK      string   `json:"bt_sk"`
	Sites     []string `json:"sites"`
	Databases []string `json:"databases"`
	FTPs      []string `json:"ftps"`
}

// BTPrecheckResult 冲突检测结果
type BTPrecheckResult struct {
	ExistsSites     []string `json:"exists_sites"`
	ExistsDatabases []string `json:"exists_databases"`
	ExistsFTPs      []string `json:"exists_ftps"`
}

// BTPrecheck 检测目标对端面板上已存在的同名网站/数据库/FTP，供前端弹窗让用户选择覆盖或跳过
func BTPrecheck(req BTPrecheckRequest) (*BTPrecheckResult, error) {
	bt := NewBTClient(req.BTURL, req.BTSK)
	res := &BTPrecheckResult{ExistsSites: []string{}, ExistsDatabases: []string{}, ExistsFTPs: []string{}}

	wantSites := map[string]bool{}
	for _, s := range req.Sites {
		wantSites[strings.TrimSpace(s)] = true
	}
	if len(wantSites) > 0 {
		if list, err := bt.SiteList(); err == nil {
			for _, item := range list {
				name, _ := item["name"].(string)
				if wantSites[name] {
					res.ExistsSites = append(res.ExistsSites, name)
				}
			}
		}
	}

	wantDbs := map[string]bool{}
	for _, d := range req.Databases {
		wantDbs[strings.TrimSpace(d)] = true
	}
	if len(wantDbs) > 0 {
		if list, err := bt.DatabaseList(); err == nil {
			for _, item := range list {
				name, _ := item["name"].(string)
				if wantDbs[name] {
					res.ExistsDatabases = append(res.ExistsDatabases, name)
				}
			}
		}
	}

	wantFtps := map[string]bool{}
	for _, f := range req.FTPs {
		wantFtps[strings.TrimSpace(f)] = true
	}
	if len(wantFtps) > 0 {
		if list, err := bt.FtpUserList(); err == nil {
			for _, item := range list {
				name, _ := item["username"].(string)
				if wantFtps[name] {
					res.ExistsFTPs = append(res.ExistsFTPs, name)
				}
			}
		}
	}
	return res, nil
}

// btParseWebname 从 SiteList 站点记录构造对端面板 DeleteSite 需要的 webname JSON。
// 站点记录的 name/domainlist 可能来自 getData（domainlist 为 JSON 字符串）或 GetSiteList。
func btParseWebname(item map[string]any) string {
	name, _ := item["name"].(string)
	domains := make([]string, 0)
	switch dl := item["domainlist"].(type) {
	case string:
		var arr []string
		if json.Unmarshal([]byte(dl), &arr) == nil {
			domains = arr
		} else {
			domains = []string{dl}
		}
	case []any:
		for _, d := range dl {
			if s, ok := d.(string); ok {
				domains = append(domains, s)
			}
		}
	case []string:
		domains = dl
	}
	if len(domains) == 0 {
		domains = []string{name}
	}
	payload := []map[string]any{{"name": name, "domainlist": domains}}
	b, _ := json.Marshal(payload)
	return string(b)
}
