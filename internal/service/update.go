package service

import (
	"archive/tar"
	"compress/gzip"
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
	DownloadURL string `json:"download_url"` // 后端二进制，必填
	WebURL      string `json:"web_url"`      // 前端包（可选，空=本次不更新前端）
	XdbURL      string `json:"xdb_url"`      // 离线 IP 库（可选，空=本次不更新离线库）
	Force       bool   `json:"force"`
}

// UpgradeStatus 面板升级进度状态（供前端轮询展示）
type UpgradeStatus struct {
	Running    bool   `json:"running"`    // 升级流程是否进行中
	Phase      string `json:"phase"`      // downloading / applying / rebooting / done / failed / idle
	Message    string `json:"message"`    // 当前阶段描述
	File       string `json:"file"`       // 当前正在下载的文件名
	Downloaded int64  `json:"downloaded"` // 已下载字节数
	Total      int64  `json:"total"`      // 总字节数（未知为 0）
	Error      string `json:"error"`      // 失败原因（Phase=failed 时有值）
}

// upgradeState 升级进度全局状态
var upgradeState = struct {
	mu    sync.Mutex
	state UpgradeStatus
}{state: UpgradeStatus{Phase: "idle"}}

// setUpgradeStatus 更新升级进度
func setUpgradeStatus(s UpgradeStatus) {
	upgradeState.mu.Lock()
	upgradeState.state = s
	upgradeState.mu.Unlock()
}

// UpgradeProgress 返回升级进度快照
func UpgradeProgress() UpgradeStatus {
	upgradeState.mu.Lock()
	defer upgradeState.mu.Unlock()
	return upgradeState.state
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
			WebURL      string `json:"web_url"`
			XdbURL      string `json:"xdb_url"`
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
		WebURL:      wrapper.Data.WebURL,
		XdbURL:      wrapper.Data.XdbURL,
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
	absUpdateURL := func(u string) string {
		if u == "" {
			return ""
		}
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			return u
		}
		return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(u, "/")
	}
	info.DownloadURL = absUpdateURL(info.DownloadURL)
	info.WebURL = absUpdateURL(info.WebURL)
	info.XdbURL = absUpdateURL(info.XdbURL)

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

// upgradeScriptTemplate 升级脚本模板：路径通过 @@KEY@@ 占位符替换，避免转义问题。
// 由面板进程同步执行（不停止自身进程），完成二进制/前端/离线库替换后写入完成标记，
// 再由面板进程通过独立 cgroup 触发重启，避免「systemctl stop 把自己连带杀掉」导致面板挂掉。
// 任何失败都会回滚二进制并输出可排查日志。
const upgradeScriptTemplate = `#!/usr/bin/env bash
set -u
LOG_FILE=@@LOG@@
exec >>"$LOG_FILE" 2>&1
echo "===== kypanel upgrade start $(date '+%F %T') ====="

NEW_BIN=@@NEW_BIN@@
BIN=@@BIN@@
BIN_OLD=@@BIN_OLD@@
WEB_UP=@@WEB_UP@@
WEB_DIR=@@WEB_DIR@@
WEB_OLD=@@WEB_OLD@@
XDB_TMP=@@XDB_TMP@@
XDB=@@XDB@@
XDB_OLD=@@XDB_OLD@@
DONE_FILE=@@DONE_FILE@@
VERSION=@@VERSION@@

fail() {
  echo "[upgrade] ERROR: $*"
  # 任一步失败：二进制已回滚，这里再恢复 web / xdb / 数据备份，尽量回到升级前状态
  if [ -d "$WEB_OLD" ]; then
    echo "[upgrade] rollback web"
    rm -rf "$WEB_DIR"
    mv -f "$WEB_OLD" "$WEB_DIR" || true
  fi
  if [ -f "$XDB_OLD" ]; then
    echo "[upgrade] rollback xdb"
    cp -f "$XDB_OLD" "$XDB" || true
  fi
  exit 1
}

rollback_file() {
  # 原子替换失败时回滚目标文件
  cp -f "$1" "$2" || true
}

# 1) 替换二进制：先备份旧的，再原子替换；失败自动回滚
echo "[upgrade] install new binary"
rm -f "$BIN_OLD"
cp -f "$BIN" "$BIN_OLD" || fail "backup old binary failed"
if ! install -m 0755 "$NEW_BIN" "$BIN"; then
  echo "[upgrade] rollback binary"
  rollback_file "$BIN_OLD" "$BIN"
  fail "install binary failed"
fi

# 2) 替换前端（可选）：先整体备份旧 web，替换失败整体恢复
if [ -d "$WEB_UP" ]; then
  echo "[upgrade] backup old web"
  rm -rf "$WEB_OLD"
  mkdir -p "$WEB_DIR"
  cp -a "$WEB_DIR" "$WEB_OLD" || fail "backup web failed"
  echo "[upgrade] replace web files"
  rm -rf "$WEB_DIR/assets"
  if ! cp -a "$WEB_UP/." "$WEB_DIR/"; then
    echo "[upgrade] rollback web"
    rm -rf "$WEB_DIR"
    mv -f "$WEB_OLD" "$WEB_DIR" || true
    fail "copy web failed"
  fi
  rm -rf "$WEB_UP"
  rm -rf "$WEB_OLD"
fi

# 3) 替换离线 IP 库（可选）：先备份旧的，失败恢复
if [ -f "$XDB_TMP" ]; then
  echo "[upgrade] replace xdb"
  rm -f "$XDB_OLD"
  cp -f "$XDB" "$XDB_OLD" || fail "backup xdb failed"
  if ! install -m 0644 "$XDB_TMP" "$XDB"; then
    echo "[upgrade] rollback xdb"
    rollback_file "$XDB_OLD" "$XDB"
    fail "install xdb failed"
  fi
  rm -f "$XDB_TMP"
  rm -f "$XDB_OLD"
fi

rm -f "$NEW_BIN"

# 4) 写完成标记，重启后的新进程据此返回「更新完成」
echo -n "$VERSION" > "$DONE_FILE"

echo "[upgrade] new binary version: $("$BIN" -version 2>/dev/null || echo unknown)"
echo "===== kypanel upgrade finished $(date '+%F %T') ====="
`

// UpgradeRunning 返回是否已有升级任务进行中
func UpgradeRunning() bool {
	return UpgradeProgress().Running
}

// UpgradePanel 异步升级面板：下载 -> 校验 -> 替换 -> 触发重启，全程上报进度供前端轮询。
// 后端二进制必更新；前端包(WebURL)与离线库(XdbURL)由官网发布时决定是否更新（留空=不更新）。
// 关键设计：全程不停止自身进程（Linux 下替换运行中的二进制是安全的），
// 重启通过独立 cgroup 触发，杜绝「升级把自己连带杀掉导致面板挂掉」。
func UpgradePanel(info UpdateInfo) error {
	if info.DownloadURL == "" {
		return fmt.Errorf("下载地址为空")
	}
	if UpgradeRunning() {
		return fmt.Errorf("已有升级任务正在进行，请稍后再试")
	}

	cfg := config.Get()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/opt/kypanel"
	}
	binaryPath := filepath.Join(dataDir, "panel")
	webDir := filepath.Join(dataDir, "web")
	tmpBin := filepath.Join(dataDir, "panel.upgrade.tmp")
	tmpWeb := filepath.Join(dataDir, "panel-web.upgrade.tmp")
	tmpXdb := filepath.Join(dataDir, "data", "ip2region.xdb.upgrade")
	webUpgradeDir := filepath.Join(dataDir, "web.upgrade")
	upgradeLog := filepath.Join(dataDir, "upgrade.log")
	backupDir := filepath.Join(dataDir, "backup")
	rollbackMark := filepath.Join(dataDir, ".upgrade_rollback")
	webOldDir := filepath.Join(dataDir, "web.old.upgrade")
	xdbOld := filepath.Join(dataDir, "data", "ip2region.xdb.old.upgrade")

	setUpgradeStatus(UpgradeStatus{Running: true, Phase: "downloading", Message: "准备开始升级…"})
	slog.Info("开始下载面板更新包",
		"bin", info.DownloadURL,
		"web", info.WebURL,
		"xdb", info.XdbURL,
	)

	// 异步执行：先返回接口响应，再在后台完成下载/替换/触发重启
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("升级流程异常", "panic", r)
				setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "升级出现异常", Error: fmt.Sprint(r)})
			}
		}()
		time.Sleep(500 * time.Millisecond) // 让前端先收到响应

		client := &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		progress := func(file string, d, t int64) {
			setUpgradeStatus(UpgradeStatus{Running: true, Phase: "downloading", Message: "正在下载更新包…", File: file, Downloaded: d, Total: t})
		}

		// 0) 备份数据：与下载并行执行，备份完成才允许进入替换阶段
		type backupResult struct {
			path string
			err  error
		}
		backupDone := make(chan backupResult, 1)
		go func() {
			setUpgradeStatus(UpgradeStatus{Running: true, Phase: "backingup", Message: "正在备份面板数据…"})
			path, err := backupForUpgrade(dataDir, backupDir)
			backupDone <- backupResult{path: path, err: err}
		}()

		// 1) 下载后端二进制并校验 ELF
		if err := downloadUpgradeFile(client, info.DownloadURL, tmpBin, "后端程序", progress); err != nil {
			slog.Error("升级失败：下载后端二进制", "err", err)
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "下载后端程序失败", Error: err.Error()})
			return
		}
		header := make([]byte, 4)
		fh, err := os.Open(tmpBin)
		if err == nil {
			_, _ = fh.Read(header)
			_ = fh.Close()
		}
		if string(header) != "\x7fELF" {
			slog.Error("升级失败：后端文件不是有效的 Linux 可执行文件")
			_ = os.Remove(tmpBin)
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "下载的后端程序不是有效的 Linux 可执行文件", Error: "invalid ELF"})
			return
		}

		// 2) 可选：下载前端包并解压校验到临时目录
		if info.WebURL != "" {
			if err := downloadUpgradeFile(client, info.WebURL, tmpWeb, "前端界面", progress); err != nil {
				slog.Error("升级失败：下载前端包", "err", err)
				_ = os.Remove(tmpBin)
				setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "下载前端界面失败", Error: err.Error()})
				return
			}
			if err := extractTarGz(tmpWeb, webUpgradeDir); err != nil {
				slog.Error("升级失败：前端包解压校验未通过", "err", err)
				_ = os.Remove(tmpBin)
				_ = os.Remove(tmpWeb)
				_ = os.RemoveAll(webUpgradeDir)
				setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "前端包解压校验未通过", Error: err.Error()})
				return
			}
			_ = os.Remove(tmpWeb)
		}

		// 3) 可选：下载离线 IP 库
		if info.XdbURL != "" {
			if err := downloadUpgradeFile(client, info.XdbURL, tmpXdb, "离线IP库", progress); err != nil {
				slog.Error("升级失败：下载离线 IP 库", "err", err)
				_ = os.Remove(tmpBin)
				_ = os.RemoveAll(webUpgradeDir)
				setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "下载离线IP库失败", Error: err.Error()})
				return
			}
		}

		// 4) 等备份完成：只有备份成功才允许替换文件
		slog.Info("更新包下载完成，等待数据备份完成")
		setUpgradeStatus(UpgradeStatus{Running: true, Phase: "downloading", Message: "下载完成，正在备份数据…"})
		backupRes := <-backupDone
		if backupRes.err != nil {
			slog.Error("升级中止：数据备份失败", "err", backupRes.err)
			_ = os.Remove(tmpBin)
			_ = os.RemoveAll(webUpgradeDir)
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "数据备份失败，已中止升级（未改动任何文件）", Error: backupRes.err.Error()})
			return
		}
		backupPath := backupRes.path

		// 5) 写回滚标记：重启后的新进程若启动失败，据此自动恢复旧版本与数据
		if err := writeRollbackMark(rollbackMark, info.Version, binaryPath+".old.upgrade", backupPath); err != nil {
			slog.Error("升级中止：写入回滚标记失败", "err", err)
			_ = os.Remove(tmpBin)
			_ = os.RemoveAll(webUpgradeDir)
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "写入回滚标记失败，已中止升级", Error: err.Error()})
			return
		}

		// 6) 同步执行升级脚本完成替换（进程保持存活，替换运行中的二进制安全）
		slog.Info("数据备份完成，开始替换面板文件")
		setUpgradeStatus(UpgradeStatus{Running: true, Phase: "applying", Message: "正在替换面板文件…"})
		script := upgradeScriptTemplate
		repl := map[string]string{
			"@@LOG@@":       upgradeLog,
			"@@NEW_BIN@@":   tmpBin,
			"@@BIN@@":       binaryPath,
			"@@BIN_OLD@@":   binaryPath + ".old.upgrade",
			"@@WEB_UP@@":    webUpgradeDir,
			"@@WEB_DIR@@":   webDir,
			"@@WEB_OLD@@":   webOldDir,
			"@@XDB_TMP@@":   tmpXdb,
			"@@XDB@@":       filepath.Join(dataDir, "data", "ip2region.xdb"),
			"@@XDB_OLD@@":   xdbOld,
			"@@DONE_FILE@@": filepath.Join(dataDir, ".upgrade_done"),
			"@@VERSION@@":   info.Version,
		}
		for k, v := range repl {
			script = strings.ReplaceAll(script, k, v)
		}

		scriptPath := filepath.Join(dataDir, "upgrade.sh")
		if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "写入升级脚本失败", Error: err.Error()})
			return
		}

		cmd := exec.Command("bash", scriptPath)
		if err := cmd.Run(); err != nil {
			slog.Error("升级失败：替换面板文件", "err", err)
			setUpgradeStatus(UpgradeStatus{Running: false, Phase: "failed", Message: "替换面板文件失败（已尽量回滚），详情见服务器 upgrade.log", Error: tailFile(upgradeLog, 500)})
			return
		}

		// 5) 触发重启（独立 cgroup，不受自身停止影响）
		slog.Info("替换完成，触发面板重启")
		setUpgradeStatus(UpgradeStatus{Running: true, Phase: "rebooting", Message: "更新完成，正在重启面板…"})
		triggerRestart(dataDir, binaryPath)
	}()

	return nil
}

// tailFile 读取文件末尾最多 n 字节，用于失败时返回可排查日志
func tailFile(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	if fi.Size() > int64(n) {
		_, _ = f.Seek(fi.Size()-int64(n), io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	return strings.TrimSpace(string(b))
}

// packTarGz 把一组路径（相对 baseDir 保留层级）打包成 tar.gz，用于升级前的数据备份。
// 自动排除 data/logs 与 data/web：日志体积大且非关键，web 目录由升级脚本单独备份。
func packTarGz(paths []string, baseDir, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	skip := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		return rel == "data/logs" || strings.HasPrefix(rel, "data/logs/") ||
			rel == "data/web" || strings.HasPrefix(rel, "data/web/")
	}

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(baseDir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." || rel == "" {
			return nil
		}
		if skip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		hdr, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return herr
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			src, oerr := os.Open(path)
			if oerr != nil {
				return oerr
			}
			_, cerr := io.Copy(tw, src)
			_ = src.Close()
			if cerr != nil {
				return cerr
			}
		}
		return nil
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue // 文件不存在（如尚未生成 xdb）则跳过
		}
		if err := filepath.Walk(p, walkFn); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = os.Remove(dest)
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// backupForUpgrade 备份面板关键数据（config.json + data/ 目录）到 dataDir/backup/upgrade-backup-<时间戳>.tar.gz。
// 返回备份文件绝对路径，升级失败时用于自动恢复。
func backupForUpgrade(dataDir, backupDir string) (string, error) {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	name := "upgrade-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	dest := filepath.Join(backupDir, name)
	paths := []string{
		filepath.Join(dataDir, "config.json"),
		filepath.Join(dataDir, "data"),
	}
	if err := packTarGz(paths, dataDir, dest); err != nil {
		_ = os.Remove(dest)
		return "", fmt.Errorf("打包数据备份失败: %w", err)
	}
	return dest, nil
}

// rollbackMark 升级回滚标记内容：记录目标版本与恢复所需文件路径
type rollbackMark struct {
	Version string `json:"version"` // 升级目标版本
	BinOld  string `json:"bin_old"` // 旧二进制备份路径
	Backup  string `json:"backup"`  // 数据备份 tar.gz 路径
}

// writeRollbackMark 写升级回滚标记。替换阶段开始前写入，新版本启动失败时据此自动恢复旧版。
func writeRollbackMark(path, version, binOld, backup string) error {
	data, err := json.Marshal(rollbackMark{Version: version, BinOld: binOld, Backup: backup})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// triggerRestart 触发面板重启：
// systemd 环境用 systemd-run 创建独立 transient unit，kypanel.service 停止时不会连带杀掉重启命令；
// 非 systemd 环境用 setsid 脱离会话后 pkill 旧进程并用 nohup 拉起新进程。
func triggerRestart(dataDir, binPath string) {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		unit := fmt.Sprintf("kypanel-upgrade-%d", time.Now().Unix())
		cmd := exec.Command("systemd-run", "--no-block", "--unit="+unit, "--collect", "--", "bash", "-c", "sleep 1; systemctl restart kypanel")
		if err := cmd.Start(); err != nil {
			slog.Error("systemd-run 触发重启失败，改用兜底方式", "err", err)
			fallbackRestart(dataDir, binPath)
		}
		return
	}
	fallbackRestart(dataDir, binPath)
}

// fallbackRestart 非 systemd 环境的兜底重启方式
func fallbackRestart(dataDir, binPath string) {
	cfgPath := filepath.Join(dataDir, "config.json")
	cmd := fmt.Sprintf(`sleep 1; pkill -f "^%s -config"; sleep 1; cd %s && nohup %s -config %s >/dev/null 2>&1 &`,
		binPath, dataDir, binPath, cfgPath)
	_ = exec.Command("setsid", "bash", "-c", cmd).Start()
}

// ConsumeUpgradeDone 读取并清除升级完成标记，返回目标版本号。
// 升级脚本替换成功后写入；重启后的新进程据此向前端返回「更新完成」。
func ConsumeUpgradeDone() (string, bool) {
	cfg := config.Get()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/opt/kypanel"
	}
	p := filepath.Join(dataDir, ".upgrade_done")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	_ = os.Remove(p)
	return strings.TrimSpace(string(b)), true
}

// rollbackScriptTemplate 升级自愈回滚脚本：新版本启动失败时，恢复旧二进制与数据备份。
// 路径通过 @@KEY@@ 占位符替换，避免转义问题。
const rollbackScriptTemplate = `#!/usr/bin/env bash
set -u
exec >>@@LOG@@ 2>&1
echo "===== kypanel auto rollback start $(date '+%F %T') ====="
BIN=@@BIN@@
BIN_OLD=@@BIN_OLD@@
BACKUP=@@BACKUP@@
DATA_DIR=@@DATA_DIR@@
DONE_FILE=@@DONE_FILE@@

# 1) 恢复旧二进制（磁盘上当前是升级后的新二进制，覆盖回旧版）
if [ -f "$BIN_OLD" ]; then
  echo "[rollback] restore old binary"
  install -m 0755 "$BIN_OLD" "$BIN" || echo "[rollback] WARN restore binary failed"
fi

# 2) 恢复数据备份（config.json + data/）
if [ -f "$BACKUP" ]; then
  echo "[rollback] restore data from $BACKUP"
  tar -xzf "$BACKUP" -C "$DATA_DIR" || echo "[rollback] WARN restore data failed"
fi

# 3) 清理升级残留标记/临时文件
rm -f "$DONE_FILE"
rm -f "$BIN_OLD"
echo "===== kypanel auto rollback finished $(date '+%F %T') ====="
`

// AutoRollbackUpgrade 升级自愈回滚：面板启动早期调用。
// 若存在升级回滚标记（新版本已替换但启动失败/不稳定），自动恢复旧二进制与数据备份，
// 并触发一次重启让 systemd 拉起恢复后的旧版本；随后继续正常启动当前进程。
// 标记删除后不再回滚，重复执行回滚脚本幂等无害。
func AutoRollbackUpgrade() {
	cfg := config.Get()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/opt/kypanel"
	}
	markPath := filepath.Join(dataDir, ".upgrade_rollback")
	data, err := os.ReadFile(markPath)
	if err != nil {
		return // 无回滚标记，正常启动
	}
	var mk rollbackMark
	if err := json.Unmarshal(data, &mk); err != nil {
		_ = os.Remove(markPath)
		return
	}
	slog.Warn("检测到升级后回滚标记，自动恢复旧版本", "target_version", mk.Version)

	script := rollbackScriptTemplate
	repl := map[string]string{
		"@@LOG@@":       filepath.Join(dataDir, "upgrade.log"),
		"@@BIN@@":       filepath.Join(dataDir, "panel"),
		"@@BIN_OLD@@":   mk.BinOld,
		"@@BACKUP@@":    mk.Backup,
		"@@DATA_DIR@@":  dataDir,
		"@@DONE_FILE@@": filepath.Join(dataDir, ".upgrade_done"),
	}
	for k, v := range repl {
		script = strings.ReplaceAll(script, k, v)
	}
	scriptPath := filepath.Join(dataDir, "upgrade-rollback.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		slog.Error("写入回滚脚本失败", "err", err)
		return
	}
	if err := exec.Command("bash", scriptPath).Run(); err != nil {
		slog.Error("执行回滚脚本失败", "err", err)
	}
	_ = os.Remove(markPath)
	// 触发一次重启：当前进程内存中是升级后的代码，重启后由 systemd 拉起恢复好的旧版本
	triggerRestart(dataDir, filepath.Join(dataDir, "panel"))
}

// downloadUpgradeFile 下载升级文件到指定路径，限制最大体积避免异常包撑爆磁盘。
// onProgress 回调当前文件已下载/总字节数（total 未知时为 0），用于前端展示下载进度。
func downloadUpgradeFile(client *http.Client, url, dest, name string, onProgress func(file string, downloaded, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "kypanel/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回非 200: %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	total := resp.ContentLength
	src := io.LimitReader(resp.Body, 1<<30) // 上限 1GB
	if onProgress != nil {
		var written int64
		last := int64(0)
		src = io.TeeReader(src, writerFunc(func(p []byte) (int, error) {
			written += int64(len(p))
			// 每 256KB 更新一次进度，避免频繁加锁；结束前再补一次
			if written-last >= 256<<10 {
				last = written
				onProgress(name, written, total)
			}
			return len(p), nil
		}))
	}
	_, err = io.Copy(f, src)
	if err == nil && onProgress != nil {
		// ContentLength 缺失（如 chunked 响应）时回读文件大小兜底
		if st, serr := f.Stat(); serr == nil {
			onProgress(name, st.Size(), total)
		}
	}
	return err
}

// writerFunc 适配 io.Writer 的函数类型
type writerFunc func(p []byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

// extractTarGz 把 tar.gz 包解压到 dst（先清空 dst）。
// 升级前先解压校验，避免损坏的前端包覆盖现有 web 目录。
func extractTarGz(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			continue // 忽略根目录与路径穿越条目
		}
		target := filepath.Join(dst, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		}
	}
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
