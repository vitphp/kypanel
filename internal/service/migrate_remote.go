package service

// ==================== 远程 kypanel 面板客户端 ====================
// 目标端作为「迁入方」时，通过 API 令牌连接源面板：
//   - GET 请求带 ?token= 查询参数（读取类）
//   - POST 请求带 Authorization: Bearer <token>（写操作强制 Header）
// 源面板侧对应的接口在 internal/router/migrate.go 的 RemoteGroup 中注册，
// 使用 ApiTokenOrAuth("api") 鉴权 + PermissionGuard("migrate") 校验 scope。

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kypanel/internal/model"
)

// RemotePanel 远程 kypanel 面板客户端
type RemotePanel struct {
	BaseURL string
	Token   string
	Version string
	client  *http.Client
}

// NewRemotePanel 创建远程面板客户端
func NewRemotePanel(baseURL, token string) *RemotePanel {
	return &RemotePanel{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   strings.TrimSpace(token),
		client: &http.Client{
			Timeout: 900 * time.Second,
			Transport: &http.Transport{
				// 自签名证书（内网面板常见）不校验证书
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Ping 探测源面板是否可访问，返回面板版本
func (r *RemotePanel) Ping() error {
	var data map[string]any
	if err := r.do("GET", "/api/migrate/remote/ping", nil, &data); err != nil {
		return err
	}
	r.Version, _ = data["version"].(string)
	return nil
}

// GetSites 拉取源面板网站列表
func (r *RemotePanel) GetSites() ([]map[string]any, error) {
	var data []map[string]any
	if err := r.do("GET", "/api/migrate/remote/sites", nil, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// GetEnv 拉取源面板环境状态
func (r *RemotePanel) GetEnv() (MigrateEnvInfo, error) {
	var env MigrateEnvInfo
	err := r.do("GET", "/api/migrate/remote/env", nil, &env)
	return env, err
}

// Pack 请求源面板打包指定的网站/数据库/FTP，返回迁移包 ID
func (r *RemotePanel) Pack(sites, databases, ftps []string) (string, error) {
	body := map[string]any{"sites": sites, "databases": databases, "ftps": ftps}
	var data struct {
		ID string `json:"id"`
	}
	if err := r.do("POST", "/api/migrate/remote/pack", body, &data); err != nil {
		return "", err
	}
	if data.ID == "" {
		return "", errors.New("源面板未返回迁移包 ID")
	}
	return data.ID, nil
}

// Download 下载源面板迁移包到本地，返回本地文件路径。
// ctx 用于「取消迁移」时立即中断下载；onProgress 每约 500ms 回调一次下载进度。
func (r *RemotePanel) Download(ctx context.Context, pkgID string, onProgress func(done, total int64)) (string, error) {
	_ = os.MkdirAll(migrateRoot(), 0o755)
	dest := filepath.Join(migrateRoot(), "import-"+pkgID+".tar.gz")
	dlURL := fmt.Sprintf("%s/api/migrate/remote/download?id=%s&token=%s",
		r.BaseURL, url.QueryEscape(pkgID), url.QueryEscape(r.Token))
	if err := downloadWithProgress(ctx, dlURL, dest, onProgress); err != nil {
		return "", fmt.Errorf("下载迁移包失败: %w", err)
	}
	return dest, nil
}

// do 发起 API 请求并解析统一响应 {code, msg, data}
func (r *RemotePanel) do(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, r.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodGet {
		q := req.URL.Query()
		q.Set("token", r.Token)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求源面板失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("源面板返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("源面板响应解析失败: %w", err)
	}
	if envelope.Code != 0 {
		return errors.New(envelope.Msg)
	}
	if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

// ---------------- 源面板侧被调用的处理函数 ----------------

// RemoteSiteList 返回本面板网站列表（供目标面板迁入时选择）
func RemoteSiteList() []map[string]any {
	var sites []model.Site
	model.DB.Order("id asc").Find(&sites)
	out := make([]map[string]any, 0, len(sites))
	for _, s := range sites {
		out = append(out, map[string]any{
			"id":              s.ID,
			"name":            s.Name,
			"domain":          s.Domain,
			"domains":         s.Domains,
			"port":            s.Port,
			"type":            s.Type,
			"root":            s.Root,
			"php_version":     phpVersionFromFpm(s.PhpFpm),
			"runtime_version": s.RuntimeVersion,
			"ssl_enabled":     s.SslEnabled,
			"dbs":             relatedDBsForSite(s.Name),
			"ftps":            relatedFTPsForSite(s.Name, s.Root),
			"created_at":      s.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return out
}

// RemotePack 源面板打包（供目标面板迁入时调用）
func RemotePack(sites, databases, ftps []string) (string, error) {
	exp, err := ExportMigration(sites, databases, ftps)
	if err != nil {
		return "", err
	}
	return exp.ID, nil
}
