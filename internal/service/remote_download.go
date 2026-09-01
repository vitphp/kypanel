package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RemoteDownloadTask 远程下载任务（异步，带进度）
type RemoteDownloadTask struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Name     string  `json:"name"`     // 文件名
	Dir      string  `json:"dir"`      // 目标目录
	Total    int64   `json:"total"`    // 文件总大小（-1 未知）
	Loaded   int64   `json:"loaded"`   // 已下载字节
	Status   string  `json:"status"`   // downloading | done | failed
	Error    string  `json:"error"`    // 错误信息
	Speed    int64   `json:"speed"`    // bytes/s
	startedAt time.Time `json:"-"`
	ip       net.IP   // SSRF 校验通过的固定公网 IP（建连用，防 DNS 重绑定）
	port     string   // 目标端口（80/443 或 URL 显式指定）
}

var (
	remoteDlTasks   = make(map[string]*RemoteDownloadTask)
	remoteDlMu      sync.Mutex
	remoteDlSeq     int
)

// StartRemoteDownload 启动一个异步远程下载任务，返回任务 ID。
// 校验 URL 和目录后立即返回，后台 goroutine 执行下载并实时更新进度。
func StartRemoteDownload(rawURL, dstDir string) (string, error) {
	clean, err := SanitizePath(dstDir)
	if err != nil {
		return "", err
	}
	di, err := os.Stat(clean)
	if err != nil || !di.IsDir() {
		return "", errors.New("目标必须是已存在的目录")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", errors.New("URL 格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("仅支持 http/https 下载")
	}
	// SSRF 防护：解析并锁定公网 IP，下载时强制用该 IP 建连，防止 DNS 重绑定绕过
	ip, err := checkPublicHost(u.Host)
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" || name == "\\" {
		return "", errors.New("无法从 URL 解析文件名")
	}

	remoteDlMu.Lock()
	remoteDlSeq++
	id := fmt.Sprintf("rd%d", remoteDlSeq)
	task := &RemoteDownloadTask{
		ID:        id,
		URL:       rawURL,
		Name:      name,
		Dir:       clean,
		Total:     -1,
		Status:    "downloading",
		startedAt: time.Now(),
		ip:        ip,
		port:      port,
	}
	remoteDlTasks[id] = task
	remoteDlMu.Unlock()

	go runRemoteDownload(task)
	return id, nil
}

// runRemoteDownload 后台执行下载，实时更新 task 的 total/loaded/speed/status
func runRemoteDownload(task *RemoteDownloadTask) {
	// 使用启动时校验并锁定的公网 IP 建连（防 DNS 重绑定 SSRF）
	dialAddr := net.JoinHostPort(task.ip.String(), task.port)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, network, dialAddr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Hour}
	u, _ := url.Parse(task.URL)
	req, err := http.NewRequest(http.MethodGet, task.URL, nil)
	if err != nil {
		finishRemoteTask(task, "failed", "URL 格式错误")
		return
	}
	if u != nil {
		req.Host = u.Host // 保留原始 Host 头，兼容按域名路由的虚拟主机
	}
	resp, err := client.Do(req)
	if err != nil {
		finishRemoteTask(task, "failed", "下载失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		finishRemoteTask(task, "failed", fmt.Sprintf("下载失败: HTTP %d", resp.StatusCode))
		return
	}
	if resp.ContentLength > 0 {
		task.Total = resp.ContentLength
	}

	dst := filepath.Join(task.Dir, task.Name)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		finishRemoteTask(task, "failed", err.Error())
		return
	}
	defer out.Close()

	// 带进度读取
	pr := &progressReader{task: task, r: resp.Body}
	if _, err := io.Copy(out, pr); err != nil {
		finishRemoteTask(task, "failed", "写入文件失败: "+err.Error())
		return
	}
	_ = ChownToWebUser(dst, false)
	finishRemoteTask(task, "done", "")
}

func finishRemoteTask(task *RemoteDownloadTask, status, errMsg string) {
	task.Status = status
	if errMsg != "" {
		task.Error = errMsg
	}
	task.Speed = 0
	// 完成任务保留 60 秒供前端查看，之后自动清理
	go func(id string) {
		time.Sleep(60 * time.Second)
		remoteDlMu.Lock()
		delete(remoteDlTasks, id)
		remoteDlMu.Unlock()
	}(task.ID)
}

// progressReader 包装 resp.Body，统计下载字节数并计算速度
type progressReader struct {
	task      *RemoteDownloadTask
	r         io.Reader
	lastBytes int64
	lastTime  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.task.Loaded += int64(n)
		now := time.Now()
		if p.lastTime.IsZero() {
			p.lastTime = now
			p.lastBytes = p.task.Loaded
		} else if now.Sub(p.lastTime) >= time.Second {
			dt := now.Sub(p.lastTime).Seconds()
			if dt > 0 {
				p.task.Speed = int64(float64(p.task.Loaded-p.lastBytes) / dt)
			}
			p.lastTime = now
			p.lastBytes = p.task.Loaded
		}
	}
	return n, err
}

// GetRemoteDownloadTasks 返回当前所有远程下载任务（含进度）
func GetRemoteDownloadTasks() []*RemoteDownloadTask {
	remoteDlMu.Lock()
	defer remoteDlMu.Unlock()
	out := make([]*RemoteDownloadTask, 0, len(remoteDlTasks))
	for _, t := range remoteDlTasks {
		out = append(out, t)
	}
	return out
}
