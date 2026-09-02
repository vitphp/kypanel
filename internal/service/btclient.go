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
	"context"
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
	"regexp"
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
	// action 同时写进 POST body：URL query 里虽有 ?action=xxx，但部分面板版本
	// 只从 POST body 取 action，缺失时会报「没有在模型中找到指定模块」。
	params.Set("action", action)
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
		// 「模块不存在」往往也是路径风格不匹配导致（legacy 的 /class?action=
		// 与 new 的 /api/class/action 并不等价），切换风格再试一次，
		// 避免整条迁移链在这一步中断。
		if btIsModuleNotFound(body) {
			lastErr = fmt.Errorf("对端面板接口失败: %s", btErrMsg(body))
			continue
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

// btIsModuleNotFound 判断响应是否为「模块不存在」类错误。
// 这类错误多为路径风格不匹配（legacy 的 /class?action= 与 new 的 /api/class/action），
// 切换风格重试即可；真正的业务失败（如数据库已存在）不算。
func btIsModuleNotFound(body []byte) bool {
	if bytes.Contains(body, []byte(`"status":true`)) || bytes.Contains(body, []byte(`"status": true`)) {
		return false // 成功响应
	}
	return bytes.Contains(body, []byte("没有在模型中找到指定模块")) ||
		bytes.Contains(body, []byte(`\u6ca1\u6709\u5728\u6a21\u578b\u4e2d\u627e\u5230\u6307\u5b9a\u6a21\u5757`))
}

// btErrMsg 从错误响应体中取出 msg 字段，取不到则返回原文
func btErrMsg(body []byte) string {
	var m struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(body, &m) == nil && m.Msg != "" {
		return m.Msg
	}
	return strings.TrimSpace(string(body))
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
	// 先识别 {"status": false, "msg": "..."} 这类错误响应：
	// 否则下面的兜底解析会把错误对象当成空列表成功返回，
	// 导致环境预检误判成「目标面板已装所需 PHP」，迁移才会在中途失败。
	var errObj struct {
		Status bool   `json:"status"`
		Msg    string `json:"msg"`
	}
	if json.Unmarshal(body, &errObj) == nil && !errObj.Status && errObj.Msg != "" {
		return nil, errors.New("对端面板接口失败: " + errObj.Msg)
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
//
// 参数严格按对端面板新版 UI 实际提交的 AddDatabase 表单补齐：
//   dtype 必填，缺省会「创建成功但不写入面板数据库列表」——这就是之前
//   php666 在 MySQL 里真实存在、面板 UI 却不显示、需手动「同步数据库」的原因。
//   dataAccess / listen_ip / host 为新版面板新增字段，一并带上保证兼容。
func (c *BTClient) AddDatabase(name, user, password string) (map[string]any, error) {
	params := url.Values{}
	params.Set("name", name)
	params.Set("db_user", user)
	params.Set("password", password)
	params.Set("dataAccess", "127.0.0.1")
	params.Set("address", "127.0.0.1")
	params.Set("codeing", "utf8mb4")
	params.Set("dtype", "MySQL")
	params.Set("ps", "由 kypanel 网站搬家迁入")
	params.Set("sid", "0")
	params.Set("listen_ip", "0.0.0.0/0")
	params.Set("host", "")
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

// ImportDatabase 导入 SQL 到对端面板指定数据库（服务端路径方式）。
// dbID 为 databases 表记录 ID；dbName 为数据库名；sqlPath 为对端面板服务器上已存在的 SQL 文件路径。
// 对端 InputSql 接口必填参数为 name 与 file（只传 id+file_name 会依次报「缺少参数！name / file」）。
func (c *BTClient) ImportDatabase(dbID, dbName, sqlPath string) (map[string]any, error) {
	params := url.Values{}
	params.Set("id", dbID)
	params.Set("name", dbName)
	params.Set("file", sqlPath)
	params.Set("sid", "0")
	return c.btRequest("database", "InputSql", params)
}

// ImportDatabaseFile 以「本地上传」方式导入 SQL：把本地 SQL 文件内容 multipart 上传给对端 InputSql，
// 由对端面板自行保存并执行导入，绕开对端不同版本对 file 参数路径解析不一致的问题
// （实测相对路径报「导入路径不存在!」、绝对路径报「数据库导入包含异常」，
// 均因对端新版 InputSql 对 file 的解析与旧版不同）。
func (c *BTClient) ImportDatabaseFile(dbID, dbName, localSQL string) (map[string]any, error) {
	f, err := os.Open(localSQL)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("id", dbID)
	_ = mw.WriteField("name", dbName)
	_ = mw.WriteField("sid", "0")
	_ = mw.WriteField("codeing", "utf8")
	fw, err := mw.CreateFormFile("file", filepath.Base(localSQL))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	_ = mw.WriteField("request_time", ts)
	_ = mw.WriteField("request_token", btMD5(ts+btMD5(c.ApiSK)))
	if err := mw.Close(); err != nil {
		return nil, err
	}
	payload := body.Bytes()
	contentType := mw.FormDataContentType()

	var lastErr error
	for _, style := range c.apiStyleOrder() {
		var u string
		if style == "new" {
			u = fmt.Sprintf("%s/api/database/InputSql", c.BaseURL)
		} else {
			u = fmt.Sprintf("%s/database?action=InputSql", c.BaseURL)
		}
		slog.Info("对端面板API请求(multipart InputSql)", "url", u, "style", style, "db", dbName)
		req, err := http.NewRequest("POST", u, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("User-Agent", "kypanel-migrate/1.0")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("请求对端面板失败: %w", err)
		}
		rb, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		slog.Info("对端面板API响应", "url", u, "status", resp.StatusCode, "body", truncateLog(strings.TrimSpace(string(rb)), 500))
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("对端面板返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
			if !strings.Contains(lastErr.Error(), "HTTP 404") {
				return nil, lastErr
			}
			continue // 404 → 尝试另一种 API 格式
		}
		if btIsModuleNotFound(rb) {
			lastErr = fmt.Errorf("对端面板接口失败: %s", btErrMsg(rb))
			continue
		}
		c.cacheAPIStyle(style)
		var m map[string]any
		if err := json.Unmarshal(rb, &m); err != nil {
			return nil, fmt.Errorf("对端面板响应解析失败: %s", truncateLog(strings.TrimSpace(string(rb)), 300))
		}
		if ok, exists := m["status"].(bool); exists && !ok {
			msg, _ := m["msg"].(string)
			return nil, fmt.Errorf("对端面板接口失败: %s", msg)
		}
		return m, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("对端面板请求失败: database/InputSql")
}

// ImportStatus 获取对端面板当前数据库导入状态（database?action=GetImportStatus）
func (c *BTClient) ImportStatus() (map[string]any, error) {
	return c.btRequest("database", "GetImportStatus", nil)
}

// ImportLog 获取对端面板数据库导入日志文本（database?action=GetImportLog）
func (c *BTClient) ImportLog() (string, error) {
	body, err := c.btRequestRaw("database", "GetImportLog", nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// ReadFile 读取对端面板服务器上文件内容（files?action=GetFileBody）
func (c *BTClient) ReadFile(path string) (map[string]any, error) {
	params := url.Values{}
	params.Set("path", path)
	return c.btRequest("files", "GetFileBody", params)
}

// ---------------- 站点配置（迁出到对端面板后补齐） ----------------
// 这些接口只通过官方 API 修改对端面板的站点配置，不直接写 nginx/apache 配置文件。
// 对端面板的站点配置由它自己生成，直接搬运 kypanel 的配置片段会因语法
// 与结构差异导致 web 服务器校验失败（典型：a duplicate default server for 0.0.0.0:80）。

// SetSiteRunPath 设置对端面板网站的运行目录（/site?action=SetSiteRunPath）。
// id: 网站 ID；runPath: 相对网站根目录的路径，"/" 表示根目录，"/public" 表示根下的 public。
func (c *BTClient) SetSiteRunPath(siteID, runPath string) error {
	params := url.Values{}
	params.Set("id", siteID)
	params.Set("runPath", runPath)
	_, err := c.btRequest("site", "SetSiteRunPath", params)
	return err
}

// SetSSL 为对端面板网站部署自定义 SSL 证书（/site?action=SetSSL）。
// siteName 传网站主域名；key 为私钥 PEM，csr 为证书 PEM（需保留换行符）。
func (c *BTClient) SetSSL(siteName, key, csr string) error {
	params := url.Values{}
	params.Set("siteName", siteName)
	params.Set("key", key)
	params.Set("csr", csr)
	_, err := c.btRequest("site", "SetSSL", params)
	return err
}

// HttpToHttps 开启对端面板网站的 HTTP → HTTPS 强制跳转（/site?action=HttpToHttps）。
func (c *BTClient) HttpToHttps(siteName string) error {
	params := url.Values{}
	params.Set("siteName", siteName)
	_, err := c.btRequest("site", "HttpToHttps", params)
	return err
}

// ApplyCustomRewrite 把自定义伪静态规则直接写入对端面板的站点 rewrite 配置文件。
// 路径：/www/server/panel/vhost/rewrite/<siteName>.conf（对端面板 nginx 站点配置默认 include 的路径）。
// 写入后对端面板在面板 UI 里能看到内容；用户重启 nginx 或在对端面板里点保存都会重新加载。
//
// 旧实现走的是 /site?action=SetRewriteTel + /site?action=SetRewriteLists 两条链路，
// 但 SetRewriteLists 的 rewrite_data 是对端面板内置模板名（wordpress / thinkphp 等），
// 传自定义模板名会被静默忽略——这是之前「伪静态在对端面板里没添加上」的真实原因。
func (c *BTClient) ApplyCustomRewrite(siteName, ruleContent string) error {
	confPath := "/www/server/panel/vhost/rewrite/" + siteName + ".conf"
	params := url.Values{}
	params.Set("path", confPath)
	params.Set("data", ruleContent)
	// 对端面板 SaveFileBody 必须显式传 encoding，否则报 FILE_SAVE_ERR
	// 且错误为 'dict_obj' object has no attribute 'encoding'
	params.Set("encoding", "utf-8")
	_, err := c.btRequest("files", "SaveFileBody", params)
	return err
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
// sfile: 源目录绝对路径；destFile: 压缩包完整目标路径（含目录与文件名）；
// zipType: "zip" 或 "tar.gz"。
// 参数随对端面板版本有差异：新版为 path（源父目录）+ sfile（相对名）+ dfile（目标完整路径）+
// z_type（类型），老版本为 sfile（绝对路径）+ dfile（目标目录）+ type + z_file（文件名），
// 新版参数失败时回退老参数。
func (c *BTClient) ZipDir(sfile, destFile, zipType string) error {
	trimmed := strings.TrimRight(sfile, "/")
	parent, relName := "/", trimmed
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		parent = trimmed[:idx+1]
		relName = trimmed[idx+1:]
	}
	params := url.Values{}
	params.Set("path", parent)
	params.Set("sfile", relName)
	params.Set("dfile", destFile)
	params.Set("z_type", zipType)
	if _, err := c.btRequest("files", "Zip", params); err == nil {
		return nil
	}
	params = url.Values{}
	params.Set("sfile", sfile)
	params.Set("dfile", filepath.Dir(destFile))
	params.Set("type", zipType)
	params.Set("z_file", filepath.Base(destFile))
	_, err := c.btRequest("files", "Zip", params)
	return err
}

// DeleteFile 删除对端面板服务器上的文件（files?action=DeleteFile）
func (c *BTClient) DeleteFile(path string) error {
	// 注意：对端面板 files?action=DeleteFile 用的是 path 参数（name 会报「没有在模型中找到指定模块」或参数无效）
	params := url.Values{}
	params.Set("path", path)
	_, err := c.btRequest("files", "DeleteFile", params)
	return err
}

// CreateRemoteDir 在对端面板服务器上创建目录（files?action=CreateDir）。
// 目录已存在时对端面板会返回错误，此处静默忽略，仅尽力而为。
func (c *BTClient) CreateRemoteDir(dir string) {
	params := url.Values{}
	params.Set("dpath", dir)
	_, _ = c.btRequest("files", "CreateDir", params)
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

// WaitSiteBackup 轮询等待对端面板网站备份完成，返回备份文件的远程绝对路径。
func (c *BTClient) WaitSiteBackup(siteName string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p := c.NewestSiteBackup(siteName); p != "" {
			return p, nil
		}
		time.Sleep(3 * time.Second)
	}
	return "", errors.New("等待对端面板网站备份超时")
}

// btRemoteFile 对端面板目录中的文件条目
type btRemoteFile struct {
	Name    string `json:"nm"`
	Size    int64  `json:"sz"`
	ModTime int64  `json:"mt"`
}

// ListDir 列对端面板服务器目录（files?action=GetDirNew），返回文件与子目录名。
// 对端面板 backup 表 id 与站点/数据库 id 无关（按主键查会拿到全表），
// 且 getData 的 type/search 参数在新版面板不可靠，因此直接列文件系统目录最稳妥。
func (c *BTClient) ListDir(path string) (files []btRemoteFile, dirs []string, err error) {
	params := url.Values{}
	params.Set("path", path)
	body, err := c.btRequestRaw("files", "GetDirNew", params)
	if err != nil {
		return nil, nil, err
	}
	var m struct {
		Files []btRemoteFile `json:"files"`
		Dirs  []struct {
			Nm string `json:"nm"`
		} `json:"dir"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, nil, fmt.Errorf("解析对端面板目录列表失败: %w", err)
	}
	for _, d := range m.Dirs {
		dirs = append(dirs, d.Nm)
	}
	return m.Files, dirs, nil
}

// newestBackupInDir 取目录中最新（按修改时间）且匹配指定后缀的非空备份文件完整路径
func (c *BTClient) newestBackupInDir(dir string, suffixes ...string) string {
	files, _, err := c.ListDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestMt int64
	for _, f := range files {
		if f.Size <= 0 {
			continue
		}
		lower := strings.ToLower(f.Name)
		matched := false
		for _, s := range suffixes {
			if strings.HasSuffix(lower, s) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if f.ModTime > bestMt {
			bestMt = f.ModTime
			best = f.Name
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(dir, best)
}

// NewestSiteBackup 返回对端面板网站目录下最新的 .tar.gz 网站备份完整路径
// （对端面板网站备份固定位于 /www/backup/site/<站点名>/，文件名 <站点名>_<时间戳>.tar.gz）
func (c *BTClient) NewestSiteBackup(siteName string) string {
	return c.newestBackupInDir(filepath.Join("/www/backup/site", siteName), ".tar.gz")
}

// NewestDBBackup 返回对端面板数据库目录下最新的数据库备份完整路径
// （对端面板手动备份在 /www/backup/database/mysql/<库名>/，旧版本在 /www/backup/database/<库名>/）
func (c *BTClient) NewestDBBackup(dbName string) string {
	for _, dir := range []string{
		filepath.Join("/www/backup/database/mysql", dbName),
		filepath.Join("/www/backup/database", dbName),
	} {
		if p := c.newestBackupInDir(dir, ".sql.gz", ".sql.zip", ".sql"); p != "" {
			return p
		}
	}
	return ""
}

// ReadRemoteFile 读取对端面板服务器上文本文件内容（files?action=GetFileBody）
func (c *BTClient) ReadRemoteFile(path string) (string, error) {
	params := url.Values{}
	params.Set("path", path)
	res, err := c.btRequest("files", "GetFileBody", params)
	if err != nil {
		return "", err
	}
	// 返回 {"status":true,"data":"..."} 或直接 data 字段
	if v, ok := res["data"].(string); ok {
		return v, nil
	}
	return "", errors.New("读取对端面板文件失败")
}

// GetSiteWebRoot 解析对端面板站点 nginx 配置中的 root 目录（用于临时放置可下载文件）。
// 兼容不同版本对端面板的 vhost 路径，且配置文件可能以主域名或站点名命名，逐一尝试。
func (c *BTClient) GetSiteWebRoot(domain string) string {
	candidates := []string{
		"/www/server/panel/vhost/nginx/" + domain + ".conf",
		"/www/server/nginx/conf/vhost/" + domain + ".conf",
	}
	for _, p := range candidates {
		conf, err := c.ReadRemoteFile(p)
		if err != nil || strings.TrimSpace(conf) == "" {
			continue
		}
		// GetFileBody 返回的 JSON 中换行是转义字面量（\n），先还原成真实换行再解析
		conf = strings.ReplaceAll(conf, "\\n", "\n")
		re := regexp.MustCompile(`(?m)^\s*root\s+([^;#\s]+)`)
		if m := re.FindStringSubmatch(conf); len(m) > 1 {
			root := strings.Trim(strings.TrimSpace(m[1]), `"'`)
			if root != "" && strings.HasPrefix(root, "/") {
				return root
			}
		}
	}
	return ""
}

// GetSiteRewrite 读取对端面板站点的伪静态规则内容。
// 对端面板伪静态配置路径为 /www/server/panel/vhost/rewrite/<网站名>.conf，
// 部分场景按主域名命名，故 name 与 domain 都尝试一遍，命中非空即返回。
func (c *BTClient) GetSiteRewrite(name, domain string) string {
	for _, cand := range []string{name, domain} {
		if cand == "" {
			continue
		}
		confPath := "/www/server/panel/vhost/rewrite/" + cand + ".conf"
		content, err := c.ReadRemoteFile(confPath)
		if err == nil {
			if s := strings.TrimSpace(content); s != "" {
				return s
			}
		}
	}
	return ""
}

// GetSiteSSLCert 从对端面板站点 nginx 配置中提取 SSL 证书与私钥内容。
// 直接解析 nginx 配置文件中的 ssl_certificate / ssl_certificate_key 路径并读取，
// 兼容不同版本对端面板的证书存放位置（vhost/cert 或 vhost/ssl），无需猜测具体目录。
// 站点未启用 HTTPS 或无证书时返回空字符串。
func (c *BTClient) GetSiteSSLCert(domain string) (cert, key string) {
	if domain == "" {
		return "", ""
	}
	conf, err := c.ReadRemoteFile("/www/server/panel/vhost/nginx/" + domain + ".conf")
	if err != nil {
		return "", ""
	}
	// GetFileBody 返回的 JSON 中换行是转义字面量（\n），先还原成真实换行再解析
	conf = strings.ReplaceAll(conf, "\\n", "\n")
	reCert := regexp.MustCompile(`(?m)^\s*ssl_certificate\b\s+([^;#\s]+)`)
	reKey := regexp.MustCompile(`(?m)^\s*ssl_certificate_key\b\s+([^;#\s]+)`)
	certPath, keyPath := "", ""
	if m := reCert.FindStringSubmatch(conf); len(m) > 1 {
		certPath = strings.TrimSpace(m[1])
	}
	if m := reKey.FindStringSubmatch(conf); len(m) > 1 {
		keyPath = strings.TrimSpace(m[1])
	}
	if certPath == "" || keyPath == "" || !strings.HasPrefix(certPath, "/") || !strings.HasPrefix(keyPath, "/") {
		return "", ""
	}
	certContent, err1 := c.ReadRemoteFile(certPath)
	keyContent, err2 := c.ReadRemoteFile(keyPath)
	if err1 != nil || err2 != nil || strings.TrimSpace(certContent) == "" || strings.TrimSpace(keyContent) == "" {
		return "", ""
	}
	return strings.TrimSpace(certContent), strings.TrimSpace(keyContent)
}

// btSiteConfServerNames 已移除：文件直链下载方式整体废弃，统一走面板端口 /download。

// DownloadViaPanel 通过对端面板端口直接下载文件（BTPanel /download 路由，
// 面板内「下载数据库备份/网站备份」即走此路由），不依赖站点域名/SSL/伪静态。
// 依次尝试 GET（query 鉴权）与 POST（form 鉴权），内容校验通过才认为成功。
// isZipFile 检查文件是否为 zip 压缩（魔数 PK\x03\x04）
func isZipFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic[0] == 'P' && magic[1] == 'K' && magic[2] == 0x03 && magic[3] == 0x04
}

// isTransferArchive 检查文件是否为迁移传输对象的有效压缩格式（gzip 或 zip）。
// 网站包为 tar.gz（gzip 流）；数据库备份新版对端面板为 .sql.zip，老版本为 .sql.gz。
func isTransferArchive(path string) bool {
	return isGzipFile(path) || isZipFile(path)
}

// DownloadViaPanel 通过对端面板端口直接下载文件（BTPanel /download 路由，
// 面板内「下载数据库备份/网站备份」即走此路由），不依赖站点域名/SSL/伪静态。
// 依次尝试 GET（query 鉴权）与 POST（form 鉴权），内容校验通过才认为成功。
func (c *BTClient) DownloadViaPanel(ctx context.Context, remotePath, dest string, onProgress func(done, total int64)) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	token := btMD5(ts + btMD5(c.ApiSK))
	q := url.Values{}
	q.Set("filename", remotePath)
	q.Set("request_token", token)
	q.Set("request_time", ts)

	attempts := []struct{ url, body string }{
		{url: c.BaseURL + "/download?" + q.Encode()},
		{url: c.BaseURL + "/download", body: "filename=" + url.QueryEscape(remotePath) +
			"&request_token=" + token + "&request_time=" + ts},
	}

	var lastErr error
	for _, attempt := range attempts {
		if err := ctx.Err(); err != nil {
			_ = os.Remove(dest)
			return err
		}
		method, body := "GET", io.Reader(nil)
		if attempt.body != "" {
			method, body = "POST", strings.NewReader(attempt.body)
		}
		req, err := http.NewRequestWithContext(ctx, method, attempt.url, body)
		if err != nil {
			lastErr = err
			continue
		}
		if attempt.body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("User-Agent", "kypanel-migrate/1.0")
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求面板下载失败: %w", err)
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			lastErr = fmt.Errorf("面板 /download 返回 HTTP %d", resp.StatusCode)
			continue
		}
		f, err := os.Create(dest)
		if err != nil {
			resp.Body.Close()
			return err
		}
		pw := &progressWriter{w: f, total: resp.ContentLength, lastReport: time.Now(), onProgress: onProgress}
		_, copyErr := io.Copy(pw, resp.Body)
		f.Close()
		resp.Body.Close()
		if copyErr != nil {
			_ = os.Remove(dest)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = copyErr
			continue
		}
		if !isTransferArchive(dest) {
			_ = os.Remove(dest)
			lastErr = errors.New("面板 /download 返回内容不是压缩包（鉴权未通过或路径无效）")
			continue
		}
		return nil
	}
	_ = os.Remove(dest)
	return lastErr
}



// btBackupRemotePath 从备份记录中提取远程完整路径：优先 filename（完整路径），
// 其次用 name + 默认备份目录。
func btBackupRemotePath(b map[string]any) string {
	if p := toStr(b["filename"]); p != "" {
		return p
	}
	if n := toStr(b["name"]); n != "" {
		return filepath.Join("/www/backup/site", n)
	}
	return ""
}

// btDBBackupRemotePath 提取数据库备份远程完整路径
func btDBBackupRemotePath(b map[string]any) string {
	if p := toStr(b["filename"]); p != "" {
		return p
	}
	if n := toStr(b["name"]); n != "" {
		return filepath.Join("/www/backup/database", n)
	}
	return ""
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
	list, err := btParseDataList(res)
	if err != nil {
		return nil, err
	}
	// 归一化字段：对端面板 ftps 表用户名字段为 name（部分接口为 username），
	// 统一填充为 username，并兜底 path，避免上层按 username 解析时全部为空。
	for _, item := range list {
		if toStr(item["username"]) == "" {
			item["username"] = item["name"]
		}
		if toStr(item["path"]) == "" {
			item["path"] = item["dir"]
		}
	}
	return list, nil
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
