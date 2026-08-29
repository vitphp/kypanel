package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/version"
)

// UpdateInfo 官网返回的更新信息
type UpdateInfo struct {
	HasUpdate   bool   `json:"has_update"`
	Version     string `json:"version"`
	MinVersion  string `json:"min_version"`
	Changelog   string `json:"changelog"`
	DownloadURL string `json:"download_url"`
	Force       bool   `json:"force"`
}

// updateCache 缓存一次检查更新结果，避免频繁请求官网
var updateCache struct {
	mu     sync.RWMutex
	info   *UpdateInfo
	err    error
	time   time.Time
	params string
	ttl    time.Duration
}

const (
	// 无更新时的缓存时长：尽量短，官网发布新版本后刷新页面能尽快感知
	updateCacheTTLShort = 60 * time.Second
	// 有更新时的缓存时长：官网后台改版本/日志后希望面板尽快感知，统一用短缓存
	updateCacheTTLLong = 60 * time.Second
)

// CheckUpdate 检查面板是否有新版本。
// force=true 时跳过缓存直接请求官网，适用于用户手动点击「检查更新」。
func CheckUpdate(ctx context.Context, arch string, force bool) (*UpdateInfo, error) {
	current := version.Version
	if arch == "" {
		arch = runtime.GOARCH
	}
	params := url.Values{}
	params.Set("version", current)
	params.Set("arch", arch)
	cacheKey := params.Encode()

	// 非强制刷新时，命中缓存直接返回（无更新 60s，有更新 30min）
	if !force {
		updateCache.mu.RLock()
		if updateCache.params == cacheKey && time.Since(updateCache.time) < updateCache.ttl {
			info, err := updateCache.info, updateCache.err
			updateCache.mu.RUnlock()
			if err != nil {
				return nil, err
			}
			// 返回副本，避免外部修改缓存
			cp := *info
			return &cp, nil
		}
		updateCache.mu.RUnlock()
	}

	baseURL := strings.TrimSuffix(config.Get().Store.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://panel.apihot.cn"
	}
	checkURL := baseURL + "/api/panel/update?" + cacheKey

	// 兼容自签/不完整证书链，避免 HTTPS 握手失败导致永远检测不到更新
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		cacheErr(err, cacheKey)
		return nil, err
	}
	req.Header.Set("User-Agent", "kypanel/"+current)

	resp, err := client.Do(req)
	if err != nil {
		cacheErr(err, cacheKey)
		return nil, fmt.Errorf("连接更新服务器失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		cacheErr(err, cacheKey)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		cacheErr(fmt.Errorf("更新服务器返回 %d: %s", resp.StatusCode, string(body)), cacheKey)
		return nil, fmt.Errorf("更新服务器返回 %d", resp.StatusCode)
	}

	// 官网响应为 {code,msg,data:{...}} 包装格式，先解析嵌套 data
	var wrapper struct {
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Status string `json:"status"`
		Data   struct {
			HasUpdate   bool   `json:"has_update"`
			Version     string `json:"version"`
			MinVersion  string `json:"min_version"`
			Changelog   string `json:"changelog"`
			DownloadURL string `json:"download_url"`
			Force       bool   `json:"force"`
		} `json:"data"`
	}
	info := UpdateInfo{}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		cacheErr(err, cacheKey)
		return nil, fmt.Errorf("解析更新信息失败: %w", err)
	}
	info = UpdateInfo{
		HasUpdate:   wrapper.Data.HasUpdate,
		Version:     wrapper.Data.Version,
		MinVersion:  wrapper.Data.MinVersion,
		Changelog:   wrapper.Data.Changelog,
		DownloadURL: wrapper.Data.DownloadURL,
		Force:       wrapper.Data.Force,
	}
	// 兼容老版本官网直接返回顶层字段的格式
	if info.Version == "" && info.DownloadURL == "" {
		var top UpdateInfo
		if err := json.Unmarshal(body, &top); err == nil {
			info = top
		}
	}

	// 如果官网返回了相对路径下载地址，自动拼上基址
	if info.DownloadURL != "" && !strings.HasPrefix(info.DownloadURL, "http://") && !strings.HasPrefix(info.DownloadURL, "https://") {
		info.DownloadURL = strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(info.DownloadURL, "/")
	}

	// 写入缓存：有更新缓存久一点（30min），无更新缓存短一点（60s），保证发布后能尽快感知
	updateCache.mu.Lock()
	updateCache.info = &info
	updateCache.err = nil
	updateCache.time = time.Now()
	updateCache.params = cacheKey
	if info.HasUpdate {
		updateCache.ttl = updateCacheTTLLong
	} else {
		updateCache.ttl = updateCacheTTLShort
	}
	updateCache.mu.Unlock()

	cp := info
	return &cp, nil
}

func cacheErr(err error, key string) {
	updateCache.mu.Lock()
	updateCache.info = nil
	updateCache.err = err
	updateCache.time = time.Now()
	updateCache.params = key
	updateCache.ttl = updateCacheTTLShort
	updateCache.mu.Unlock()
}

// UpgradePanel 异步升级面板：下载新版本 → 替换二进制 → 重启服务
func UpgradePanel(downloadURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("下载地址为空")
	}

	cfg := config.Get()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/opt/kypanel"
	}
	binaryPath := filepath.Join(dataDir, "panel")
	tmpPath := filepath.Join(dataDir, "panel.upgrade.tmp")

	slog.Info("开始下载面板更新包", "url", downloadURL)

	// 异步执行：先返回接口响应，再在后台完成下载/替换/重启
	go func() {
		time.Sleep(500 * time.Millisecond) // 让前端先收到响应

		client := &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
		if err != nil {
			slog.Error("升级失败：构造下载请求", "err", err)
			return
		}
		req.Header.Set("User-Agent", "kypanel/"+version.Version)

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("升级失败：下载更新包", "err", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			slog.Error("升级失败：下载返回非 200", "status", resp.StatusCode)
			return
		}

		if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
			slog.Error("升级失败：创建临时目录", "err", err)
			return
		}
		f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			slog.Error("升级失败：创建临时文件", "err", err)
			return
		}
		_, err = io.Copy(f, io.LimitReader(resp.Body, 300<<20))
		_ = f.Close()
		if err != nil {
			slog.Error("升级失败：写入临时文件", "err", err)
			return
		}

		// 校验是否是 ELF 可执行文件（简单魔数校验）
		header := make([]byte, 4)
		fh, err := os.Open(tmpPath)
		if err == nil {
			_, _ = fh.Read(header)
			_ = fh.Close()
		}
		if string(header) != "\x7fELF" {
			slog.Error("升级失败：下载文件不是有效的 Linux 可执行文件")
			_ = os.Remove(tmpPath)
			return
		}

		slog.Info("更新包下载完成，准备替换并重启")

		// 使用一个独立脚本完成替换+重启，避免进程替换自己导致失败
		script := fmt.Sprintf(`#!/usr/bin/env bash
set -e
sleep 2
systemctl stop kypanel || true
install -m 0755 %q %q
rm -f %q
systemctl start kypanel || true
`, tmpPath, binaryPath, tmpPath)

		scriptPath := filepath.Join(dataDir, "upgrade.sh")
		if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
			slog.Error("升级失败：写入升级脚本", "err", err)
			return
		}

		cmd := exec.Command("nohup", "bash", scriptPath)
		if err := cmd.Start(); err != nil {
			slog.Error("升级失败：启动升级脚本", "err", err)
			return
		}
		// 启动脚本后，本进程会被 systemctl stop 终止，不需要 wait
		_ = cmd.Process.Release()
	}()

	return nil
}

// ParseVersionToInt 把 "0.1.0" 解析成可比较的整数 10000
func ParseVersionToInt(v string) int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	var major, minor, patch int
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		// patch 可能带 "-beta.1" 后缀，只取数字部分
		patchStr := parts[2]
		for i, c := range patchStr {
			if c < '0' || c > '9' {
				patchStr = patchStr[:i]
				break
			}
		}
		patch, _ = strconv.Atoi(patchStr)
	}
	return major*10000 + minor*100 + patch
}

// NeedUpdate 比较当前版本与远程版本，判断是否需要更新
func NeedUpdate(current, remote string) bool {
	return ParseVersionToInt(remote) > ParseVersionToInt(current)
}
