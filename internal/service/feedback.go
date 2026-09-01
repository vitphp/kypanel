package service

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/version"
)

// AppErrorReport 应用安装/卸载失败时上报到官网的错误报告。
// 只包含问题排查必需的系统信息与应用错误，不含任何面板数据/用户数据。
type AppErrorReport struct {
	Type    string `json:"type"`     // app_error
	Version string `json:"version"`  // 面板版本
	AppKey  string `json:"app_key"`  // 应用 key（如 node24）
	Action  string `json:"action"`   // install / uninstall
	Error   string `json:"error"`    // 错误摘要（前 500 字符）
	LogTail string `json:"log_tail"` // 安装/卸载日志尾部（前 2000 字符）
	OS      string `json:"os"`       // 系统发行版描述
	Arch    string `json:"arch"`     // 系统架构
	At      string `json:"at"`       // 上报时间 RFC3339
}

// feedbackDedup 去重记录：同一应用 + 动作 + 错误签名 24h 内只上报一次，避免重复刷屏。
var (
	feedbackDedupMu   sync.Mutex
	feedbackDedupSeen = map[string]time.Time{}
)

const feedbackDedupTTL = 24 * time.Hour

// ReportAppError 应用安装/卸载失败时异步上报错误到官网（面板官网，即 config.Store.BaseURL）。
// 特性：
//   - 开关控制：config.Store.ReportErrors 为 false 或未配置官网 base_url 时不上报；
//   - 异步发送：不阻塞安装/卸载流程；
//   - 幂等去重：同一错误 24h 内只上报一次；
//   - 静默失败：上报失败仅记日志，绝不影响面板功能。
func ReportAppError(appKey, action, errMsg, logPath string) {
	cfg := config.Get()
	if !cfg.Store.ReportErrors {
		return
	}
	baseURL := strings.TrimSuffix(cfg.Store.BaseURL, "/")
	if baseURL == "" {
		return
	}

	// 去重：错误签名 = appKey|action|error 前 120 字符
	sig := appKey + "|" + action + "|" + truncate(errMsg, 120)
	feedbackDedupMu.Lock()
	if last, ok := feedbackDedupSeen[sig]; ok && time.Since(last) < feedbackDedupTTL {
		feedbackDedupMu.Unlock()
		return
	}
	feedbackDedupSeen[sig] = time.Now()
	feedbackDedupMu.Unlock()

	report := AppErrorReport{
		Type:    "app_error",
		Version: version.Version,
		AppKey:  appKey,
		Action:  action,
		Error:   truncate(errMsg, 500),
		LogTail: truncate(readLogTail(logPath, 2000), 2000),
		OS:      readOSName(),
		Arch:    runtime.GOARCH,
		At:      time.Now().Format(time.RFC3339),
	}
	endpoint := baseURL + "/api/panel/feedback"
	go func() {
		if err := postFeedback(endpoint, report); err != nil {
			slog.Warn("错误上报失败（不影响功能）", "endpoint", endpoint, "app", appKey, "err", err)
		}
	}()
}

// postFeedback 发送错误报告到官网。
func postFeedback(endpoint string, report AppErrorReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kypanel/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &reportHTTPError{code: resp.StatusCode}
	}
	return nil
}

type reportHTTPError struct{ code int }

func (e *reportHTTPError) Error() string { return "官网返回 " + http.StatusText(e.code) }

// readLogTail 读取文件末尾内容（用于附带日志便于排查）。
func readLogTail(path string, maxBytes int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > maxBytes {
		b = b[len(b)-maxBytes:]
	}
	return string(b)
}

// readOSName 读取 /etc/os-release 的 PRETTY_NAME，失败回退到 ID 信息。
func readOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

// truncate 截断字符串到指定长度（按字节，末尾补省略号）。
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
