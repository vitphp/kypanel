package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// 正在安装/卸载中的任务（防止重复操作，并向面板右下角任务队列显示）
var (
	taskMu   sync.Mutex
	appTasks = map[string]*AppTask{} // key -> 任务信息
)

// ============================================================================
// 【3】安装/卸载任务执行
// ============================================================================

// 全局安装/卸载串行队列（容量 1，元素即"占用令牌"）：
// 同一时刻只允许一个应用安装/卸载任务真正执行，其余任务排队等待，
// 避免多个 apt-get/yum/dpkg 并发导致包管理器锁冲突。
var installSlot = make(chan struct{}, 1)

// acquireInstallSlot 等待并占用安装令牌（阻塞直到上一个任务完成或 ctx 取消）
func acquireInstallSlot(ctx context.Context) bool {
	select {
	case installSlot <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseInstallSlot 释放安装令牌（下一个排队任务随即开始）
func releaseInstallSlot() {
	<-installSlot
}

// AppTask 当前正在执行的应用任务（暴露给前端）
type AppTask struct {
	Key       string             `json:"key"`
	Name      string             `json:"name"`
	Category  string             `json:"category"`
	Status    string             `json:"status"`     // installing / uninstalling
	Queued    bool               `json:"queued"`     // true = 排队等待中（前面还有任务未完成）
	StartedAt int64              `json:"started_at"` // unix 秒
	cancel    context.CancelFunc // 取消任务（停止安装/卸载）
}

// ListAppTasks 列出当前所有正在执行的应用任务（队列）
func ListAppTasks() []AppTask {
	taskMu.Lock()
	defer taskMu.Unlock()
	list := make([]AppTask, 0, len(appTasks))
	for _, t := range appTasks {
		list = append(list, *t)
	}
	// 按开始时间倒序（最新任务在前）
	sort.Slice(list, func(i, j int) bool { return list[i].StartedAt > list[j].StartedAt })
	return list
}

// CancelAppTask 停止正在执行的安装/卸载任务
func CancelAppTask(key string) error {
	taskMu.Lock()
	defer taskMu.Unlock()
	t, ok := appTasks[key]
	if !ok {
		return errors.New("该应用没有正在执行的任务")
	}
	if t.cancel != nil {
		t.cancel() // 触发 runCmdWithLogCtx 取消，杀掉整个进程组
	}
	return nil
}

const appInstallTimeout = 40 * time.Minute

// AppItem 返回给前端的应用条目（元数据 + 安装状态）
type AppItem struct {
	AppMeta
	Status      string `json:"status"`
	Version     string `json:"version"`
	Error       string `json:"error"`
	ServiceName string `json:"service_name"`
	Source      string `json:"source"` // panel(面板安装) / system(系统自带) / ""(未安装)
}

// StartGhostWatcher 启动后台 goroutine，每 30 秒扫一次「卡住的 installing/uninstalling」记录。
// 判定 ghost：
//  1. record 状态是 installing / uninstalling
//  2. appTasks map 里没有该 key（in-memory 跟踪丢失）
//  3. 系统里没有 apt-get / dpkg 进程在跑（关联的 apt 进程也已结束）
//
// 三者全满足 → 重置 record 为 not_installed（写入 error 提示用户重试）。
// 这样即使 service 启动后才产生的 ghost 也能被自动清理，前端按钮 loading 转圈状态
// 能在 ≤ 60 秒内恢复。
func StartGhostWatcher(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				cleanupRunningGhosts()
			}
		}
	}()
}

func cleanupRunningGhosts() {
	for _, status := range []string{model.AppInstalling, model.AppUninstalling} {
		recs, err := model.ListAppRecordsByStatus(status)
		if err != nil || len(recs) == 0 {
			continue
		}
		for _, rec := range recs {
			// 1. in-memory 任务还在跑 → 不是 ghost
			taskMu.Lock()
			_, alive := appTasks[rec.Key]
			taskMu.Unlock()
			if alive {
				continue
			}
			// 2. apt / dpkg 系统进程在跑 → 可能在后台安装/卸载，不算 ghost
			if aptRunning() {
				continue
			}
			// 3. 都没了 → ghost
			rec.Status = model.AppNotInstalled
			rec.Version = ""
			rec.Error = "后台任务已异常结束，请重试安装"
			rec.InstalledAt = nil
			if err := model.SaveAppRecord(&rec); err == nil {
				slog.Warn("ghost 状态已自动清理", "key", rec.Key, "prev_status", status)
			}
		}
	}
}

// aptRunning 检查系统里是否真有 apt-get / dpkg 在跑（避免误判正在后台安装的应用）
func aptRunning() bool {
	for _, bin := range []string{"apt-get", "dpkg", "apt"} {
		res, err := ExecCommand("pgrep -x "+bin, 5*time.Second)
		if err == nil && strings.TrimSpace(res.Stdout) != "" {
			return true
		}
	}
	return false
}

// CleanupGhostInstalls 清理 ghost 状态：服务启动时 appTasks（in-memory map）一定为空，
// 但 DB 里可能残留上一次崩溃/SIGHUP 留下的 installing/uninstalling 记录——
// 这些记录没有对应的协程在跑，浮窗也找不到任务来取消。
// 启动时把这类状态重置为 not_installed，避免「卡片一直转圈 + 浮窗找不到任务」脱钩。
func CleanupGhostInstalls() int {
	cleaned := 0
	for _, status := range []string{model.AppInstalling, model.AppUninstalling} {
		recs, err := model.ListAppRecordsByStatus(status)
		if err != nil {
			continue
		}
		for _, rec := range recs {
			rec.Status = model.AppNotInstalled
			rec.Version = ""
			rec.Error = "服务已重启，原任务已中止"
			rec.InstalledAt = nil
			if err := model.SaveAppRecord(&rec); err == nil {
				cleaned++
			}
		}
	}

	// 清理「未安装但残留历史错误文案」的记录（ghost watcher / 服务重启写入的提示）。
	// 这些应用实际从未安装成功，保留 error 会让应用商店卡片长期挂着红条，显得一团糟。
	var staleRecs []model.AppRecord
	model.DB.Where("status = ? AND error IN ?",
		model.AppNotInstalled,
		[]string{"后台任务已异常结束，请重试安装", "服务已重启，原任务已中止"},
	).Find(&staleRecs)
	for i := range staleRecs {
		staleRecs[i].Error = ""
		if err := model.SaveAppRecord(&staleRecs[i]); err == nil {
			cleaned++
		}
	}

	// 清理「DB 标记为 installed 但二进制/服务实际不存在」的幽灵记录，
	// 避免安装脚本 exit 0 却未真正安装成功时，应用商店显示已安装、
	// 网站页却因 env-status 检测不到而卡在"未检测到环境"。
	var installedRecs []model.AppRecord
	model.DB.Where("status = ?", model.AppInstalled).Find(&installedRecs)
	for _, rec := range installedRecs {
		meta, ok := findApp(rec.Key)
		if !ok {
			continue
		}
		if !isMetaInstalled(meta) {
			rec.Status = model.AppNotInstalled
			rec.Version = ""
			rec.Error = "安装记录与实际环境不一致，已自动重置"
			rec.InstalledAt = nil
			if err := model.SaveAppRecord(&rec); err == nil {
				cleaned++
				slog.Warn("ghost installed 状态已自动清理", "key", rec.Key)
			}
		}
	}

	slog.Info("启动时 ghost 状态清理完成", "cleaned", cleaned)
	return cleaned
}

// 应用商店列表缓存：全局安装浮窗(InstallFloater 每 2.5s 轮询)与多个页面都会请求 /api/apps/list，
// 而每次全量探测都要 spawn 子进程 + 写 SQLite。缓存 + 探测合并避免轮询放大导致进程风暴。
var (
	appsListCache    atomic.Value // 存 []AppItem
	appsListCachedAt time.Time
	appsListProbing  bool
	appsListMu       sync.Mutex
)

// InvalidateAppsCache 使应用商店列表缓存失效（安装/卸载完成时调用，保证前端能立即看到终态）
func InvalidateAppsCache() {
	appsListMu.Lock()
	appsListCachedAt = time.Time{}
	appsListMu.Unlock()
}

// ListApps 返回应用商店列表（含本地状态，已安装的应用探测版本）
// 带 5s TTL 缓存：缓存有效期内直接返回；已有探测在跑时也直接返回旧结果，不重复探测。
func ListApps() []AppItem {
	const cacheTTL = 5 * time.Second

	appsListMu.Lock()
	if cached, ok := appsListCache.Load().([]AppItem); ok && time.Since(appsListCachedAt) < cacheTTL {
		appsListMu.Unlock()
		return cached
	}
	if appsListProbing {
		// 已有探测在跑：返回旧缓存；无旧缓存则返回空列表（仅首次并发极低时发生）
		if cached, ok := appsListCache.Load().([]AppItem); ok {
			appsListMu.Unlock()
			return cached
		}
		appsListMu.Unlock()
		return []AppItem{}
	}
	appsListProbing = true
	appsListMu.Unlock()

	items := listAppsUncached()

	appsListMu.Lock()
	appsListCache.Store(items)
	appsListCachedAt = time.Now()
	appsListProbing = false
	appsListMu.Unlock()
	return items
}

// listAppsUncached 实际的全量探测（ListApps 的缓存保护已剥掉）
func listAppsUncached() []AppItem {
	// 一次性加载所有应用记录，避免 N+1 查询（20+ 应用 = 20+ 次 SELECT）
	var allRecs []model.AppRecord
	model.DB.Find(&allRecs)
	recByKey := make(map[string]*model.AppRecord, len(allRecs))
	for i := range allRecs {
		recByKey[allRecs[i].Key] = &allRecs[i]
	}

	metas := allAppMetas()

	// 并发探测：每个 app 的 probeVersion 都是 subprocess spawn（每次 100-300ms），
	// 串行在装了 20+ runtime 时要 4-6 秒，开 goroutine 后降到 ~500ms（瓶颈是 max 单个探测）
	type probeResult struct {
		idx     int
		version string
		isGhost bool // record 缺失但二进制/服务存在
	}
	results := make([]probeResult, len(metas))

	var wg sync.WaitGroup
	for i, meta := range metas {
		i, meta := i, meta
		wg.Add(1)
		go func() {
			defer wg.Done()
			hasRec := false
			if rec, ok := recByKey[meta.Key]; ok {
				results[i].idx = i
				results[i].version = rec.Version
				hasRec = true
			}
			// 兜底：record 缺失 + 二进制/服务在跑 → 视为 installed（典型：Debian 系统自带的 PHP）
			if !hasRec && isMetaInstalled(meta) {
				results[i].isGhost = true
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					results[i].version = v
				}
			}
			// 已装 app 实时探测版本（写入缓存 / 更新 DB）
			if hasRec || results[i].isGhost {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					results[i].version = v
					if hasRec {
						_ = model.DB.Model(&model.AppRecord{}).Where("key = ?", meta.Key).Update("version", v).Error
					}
				}
			}
		}()
	}
	// 整体超时保护：万一某个 subprocess 探测卡死（ExecCommand 5s 也没杀掉的极端情况），
	// 8s 后不再 wait，剩余探测的 results 保持零值（version=""），list 仍能正常返回，
	// 避免 nginx 触发 60s upstream timeout 导致前端一直 502。
	{
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
		}
	}

	// 按 metas 顺序汇总（保证返回顺序与 meta 注册顺序一致）
	items := make([]AppItem, len(metas))
	for i, meta := range metas {
		item := AppItem{AppMeta: meta}
		item.SystemDefault = systemDefaultKeys[meta.Key]
		hasRec := false
		var rec *model.AppRecord
		if r, ok := recByKey[meta.Key]; ok {
			rec = r
			item.Status = rec.Status
			item.Version = rec.Version
			item.Error = rec.Error
			item.ServiceName = rec.ServiceName
			hasRec = true
		} else {
			item.Status = model.AppNotInstalled
		}
		if !hasRec && item.Status == model.AppNotInstalled && results[i].isGhost {
			item.Status = model.AppInstalled
			item.Source = "system" // DB 无记录但系统里存在且能用 → 系统自带
		} else if hasRec && item.Status == model.AppInstalled {
			item.Source = "panel" // DB 有 installed 记录 → 面板安装
		}
		// 清理孤儿失败记录：failed 但实际不能用（探测不到）→ 修正为 not_installed（隐藏）
		// 典型场景：安装失败残留 failed 记录，但应用目录/二进制不存在，应归入「未安装」而非「已安装/失败」
		if hasRec && item.Status == model.AppFailed && !isMetaInstalled(meta) {
			item.Status = model.AppNotInstalled
			item.Source = ""
			item.Error = ""
		}
		// 修复「安装时版本探测失败被标记为 failed，但实际已安装且可用」的记录。
		// 典型场景：FTP 的 VersionCmd 使用了 bash 花括号语法，在 /bin/sh(dash) 下执行失败，
		// 导致 vsftpd 实际已安装却被记录为 failed；修复命令后再次探测应自动纠正状态。
		if hasRec && item.Status == model.AppFailed && isMetaInstalled(meta) {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Status = model.AppInstalled
				item.Version = v
				item.Source = "panel"
				item.Error = ""
				rec.Status = model.AppInstalled
				rec.Version = v
				rec.Error = ""
				now := time.Now()
				rec.InstalledAt = &now
				_ = model.SaveAppRecord(rec)
			}
		}
		if results[i].version != "" {
			item.Version = results[i].version
		}
		items[i] = item
	}
	return items
}

// AppLogPath 返回某应用的安装/卸载日志文件路径
func AppLogPath(key string) string {
	return filepath.Join(config.Get().DataDir, "logs", "apps", key+".log")
}

// InstallApp 异步安装应用；phpVersion 为 phpMyAdmin 等应用选择的 PHP 版本（空则用最高版本），version 为通用版本选择
func InstallApp(key string, phpVersion string, version string) error {
	meta, ok := findApp(key)
	if !ok {
		return errors.New("未知应用: " + key)
	}
	// Nginx 与 Apache 互斥：只能安装其中一个
	if err := checkWebServerConflict(meta.Key); err != nil {
		return err
	}
	if meta.Key == "phpmyadmin" {
		ver, _, err := selectPhpFpm(phpVersion)
		if err != nil {
			// 没有匹配的 PHP-FPM：
			// 1) 若系统还有其他 PHP 版本则回退到已安装的最高版本
			// 2) 完全没有任何 PHP 时标记为自动安装 PHP（默认版）
			if v, _, err2 := selectPhpFpm(""); err2 == nil {
				phpVersion = v
			} else {
				phpVersion = "auto"
			}
		} else {
			phpVersion = ver
		}
	}

	taskMu.Lock()
	if _, busy := appTasks[key]; busy {
		taskMu.Unlock()
		return errors.New("应用正在安装或卸载中，请稍候")
	}
	ctx, cancel := context.WithCancel(context.Background())
	appTasks[key] = &AppTask{
		Key:       key,
		Name:      meta.Name,
		Category:  meta.Category,
		Status:    model.AppInstalling,
		Queued:    true, // 先进入全局串行队列，等前面的任务完成后再真正开始
		StartedAt: time.Now().Unix(),
		cancel:    cancel,
	}
	taskMu.Unlock()

	rec, err := model.GetAppRecord(key)
	if err != nil {
		rec = &model.AppRecord{Key: key, Status: model.AppInstalling}
	} else {
		rec.Status = model.AppInstalling
		rec.Error = ""
	}
	_ = model.SaveAppRecord(rec)

	go func() {
		acquired := false
		defer func() {
			cancel()
			taskMu.Lock()
			delete(appTasks, key)
			taskMu.Unlock()
			if acquired {
				releaseInstallSlot()
			}
		}()
		// 全局串行队列：同一时刻只允许一个应用安装/卸载任务真正执行
		if !acquireInstallSlot(ctx) {
			return // 任务已被取消（排队期间点了停止）
		}
		acquired = true
		taskMu.Lock()
		if t, ok := appTasks[key]; ok {
			t.Queued = false // 正式开始执行
		}
		taskMu.Unlock()
		runInstall(ctx, meta, phpVersion, version)
	}()
	return nil
}

func runInstall(ctx context.Context, meta AppMeta, phpVersion string, version string) {
	logPath := AppLogPath(meta.Key)
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	_ = os.WriteFile(logPath, nil, 0o644) // 清空旧日志

	logf := func(format string, args ...any) {
		line := fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
		appendToFile(logPath, line)
		slog.Info("应用安装", "key", meta.Key, "msg", fmt.Sprintf(format, args...))
	}

	// 任务被取消（用户点了"停止"）时，把记录重置为未安装
	defer func() {
		if ctx.Err() == context.Canceled {
			rec, _ := model.GetAppRecord(meta.Key)
			rec.Status = model.AppNotInstalled
			rec.Version = ""
			rec.ServiceName = ""
			rec.Error = "已手动取消安装"
			rec.InstalledAt = nil
			_ = model.SaveAppRecord(rec)
			logf("安装已取消")
		}
	}()

	status := model.AppInstalled
	var ver, serviceName, errMsg string

	pkgMgr := detectPkgManager()
	logf("检测到包管理器: %s", pkgMgr)

	if meta.InstallScript != "" {
		// 自定义安装脚本（多版本 PHP / phpMyAdmin / MongoDB / SQLServer 等）
		script := meta.InstallScript
		if meta.Key == "phpmyadmin" {
			socket := ""
			if phpVersion != "auto" {
				socket = resolvePhpFpm("PHP " + phpVersion)
			}
			if socket == "" {
				// 未检测到已安装的 PHP-FPM：自动安装 PHP 7.4 作为兜底，安装完成后再继续
				logf("未检测到已安装的 PHP-FPM，自动安装 PHP 7.4 ...")
				if err := installDefaultPhp74(ctx, logPath, logf); err != nil {
					status = model.AppFailed
					errMsg = "自动安装 PHP 7.4 失败: " + err.Error()
					logf("%s", errMsg)
				} else {
					opts := ListPhpFpms()
					if len(opts) == 0 {
						status = model.AppFailed
						errMsg = "PHP 7.4 已安装但未检测到 FPM 运行入口，请稍后重试"
						logf("%s", errMsg)
					} else {
						sort.SliceStable(opts, func(i, j int) bool {
							return phpVersionNum(opts[i].Label) > phpVersionNum(opts[j].Label)
						})
						phpVersion = phpMinorVersion(opts[0].Label)
						socket = opts[0].Value
						logf("PHP 7.4 安装完成，phpMyAdmin 使用 PHP %s（%s）", phpVersion, socket)
					}
				}
			}
			if socket != "" {
				script = "export SELECTED_PHP_VERSION='" + phpVersion + "'\nexport SELECTED_PHP_SOCKET='" + socket + "'\n" + script
				logf("phpMyAdmin 使用 PHP %s（%s）", phpVersion, socket)
				logf("执行自定义安装脚本 ...")
				if err := runCmdWithLogCtx(ctx, script, logPath, appInstallTimeout); err != nil {
					status = model.AppFailed
					errMsg = err.Error()
					logf("自定义安装脚本执行失败: %v", err)
				}
			}
		} else {
			// 其他自定义脚本（如 MongoDB、SQLServer）支持版本选择
			if version != "" {
				script = "export SELECTED_VERSION='" + version + "'\n" + script
				logf("选择版本: %s", version)
			}
			logf("执行自定义安装脚本 ...")
			if err := runCmdWithLogCtx(ctx, script, logPath, appInstallTimeout); err != nil {
				status = model.AppFailed
				errMsg = err.Error()
				logf("自定义安装脚本执行失败: %v", err)
			}
		}
	} else if pkgMgr == "apt" {
		logf("开始 apt-get update ...")
		if err := runCmdWithLogCtx(ctx, "apt-get update", logPath, appInstallTimeout); err != nil {
			logf("apt-get update 失败: %v", err)
			// update 失败不阻塞安装，继续尝试
		}
		cmd := fmt.Sprintf("DEBIAN_FRONTEND=noninteractive apt-get install -y %s", strings.Join(meta.AptPackages, " "))
		logf("执行安装命令: %s", cmd)
		if err := runCmdWithLogCtx(ctx, cmd, logPath, appInstallTimeout); err != nil {
			if len(meta.AptFallbackPackages) > 0 {
				// 原包不可用（如 Debian 新版本仓库无 mysql-server），尝试兼容替代包
				fb := strings.Join(meta.AptFallbackPackages, " ")
				logf("%s 安装失败，尝试兼容替代包: %s", strings.Join(meta.AptPackages, " "), fb)
				fbCmd := fmt.Sprintf("DEBIAN_FRONTEND=noninteractive apt-get install -y %s", fb)
				logf("执行安装命令: %s", fbCmd)
				if fbErr := runCmdWithLogCtx(ctx, fbCmd, logPath, appInstallTimeout); fbErr != nil {
					status = model.AppFailed
					errMsg = fbErr.Error()
					logf("替代包安装失败: %v", fbErr)
				} else {
					// 替代包安装成功，改用对应的服务名（如 mariadb）
					if meta.AptFallbackService != "" {
						meta.Service = meta.AptFallbackService
					}
					logf("已通过替代包安装成功")
				}
			} else {
				status = model.AppFailed
				errMsg = err.Error()
			}
		}
	} else if pkgMgr == "yum" || pkgMgr == "dnf" {
		cmd := fmt.Sprintf("%s install -y %s", pkgMgr, strings.Join(meta.YumPackages, " "))
		logf("执行安装命令: %s", cmd)
		if err := runCmdWithLogCtx(ctx, cmd, logPath, appInstallTimeout); err != nil {
			status = model.AppFailed
			errMsg = err.Error()
		}
	} else {
		status = model.AppFailed
		errMsg = "不支持的包管理器: " + pkgMgr
		logf("安装失败: %s", errMsg)
	}

	if status != model.AppFailed {
		if meta.Key == "phpmyadmin" {
			// 配置免密登录：创建专用 MySQL 管理账号并写入 phpMyAdmin 配置
			logf("配置 phpMyAdmin 免密登录 ...")
			if err := configurePhpMyAdmin(); err != nil {
				status = model.AppFailed
				errMsg = "配置免密登录失败: " + err.Error()
				logf("%s", errMsg)
			}
		}
		if meta.Key == "mysql" || meta.Key == "mariadb" {
			// 安装完成后自动重置 root@localhost 密码为随机串，失败仅记录日志，不影响安装状态
			logf("自动重置 %s root 密码 ...", meta.Name)
			if pwd, err := ResetMysqlRootPwdAfterInstall(); err != nil {
				logf("自动重置 root 密码失败: %v (可在数据库页手动设置)", err)
			} else {
				logf("已自动生成 root 密码（请到「数据库 > MySQL > root 密码」查看）")
				_ = pwd
			}
		}
		// 安装完成后必须成功探测到版本才视为成功，否则标记失败。
		// 避免脚本 exit 0 但实际没安装成功，导致应用商店显示 installed
		// 而 env-status 检测不到、网站页一直卡在"未检测到环境"的情况。
		if status != model.AppFailed {
			if v, err := probeVersion(meta.VersionCmd); err != nil || v == "" {
				status = model.AppFailed
				errMsg = "安装后未能检测到版本，可能未正确安装"
				logf("版本探测失败: %v", err)
			} else {
				ver = v
				status = model.AppInstalled
			}
		}

		serviceName = resolveServiceName(meta)
		// 安装成功后尝试启动服务
		if serviceName != "" {
			_ = runCmdWithLog(fmt.Sprintf("systemctl enable --now %s", serviceName), logPath, 2*time.Minute)
		}
		logf("安装完成，版本: %s", ver)
	}

	rec, _ := model.GetAppRecord(meta.Key)
	now := time.Now()
	if status == model.AppInstalled {
		rec.InstalledAt = &now
		// 安装成功后自动放行软件所需端口（仅 FTP）。
		// FTP 需要 20/21/39000-40000 被动端口段才能被外部访问；
		// MySQL/Redis 等数据库、Nginx/Apache 等 Web 服务的端口不应自动暴露到公网，避免安全隐患。
		// 备注带上应用名 + AutoSource 标识，便于防火墙列表识别和卸载时精准回收。
		if meta.Key == "ftp" && len(meta.OpenPorts) > 0 {
			appRemark := meta.Name
			if appRemark == "" {
				appRemark = meta.Key
			}
			for _, p := range meta.OpenPorts {
				if err := AllowPortWithSource(p, "tcp", appRemark+" 放行", "app:"+meta.Key); err != nil {
					logf("自动放行端口 %s 失败: %v", p, err)
				} else {
					logf("已自动放行端口 %s (tcp)", p)
				}
			}
		}
	}
	rec.Status = status
	rec.Version = ver
	rec.Error = errMsg
	rec.ServiceName = serviceName
	_ = model.SaveAppRecord(rec)
	InvalidateAppsCache()
	InvalidateEnvStatusCache() // 环境状态已变化，失效缓存待重新探测
}

// installDefaultPhp74 同步安装 PHP 7.4（apt/yum 通用流程）。
// 用于安装 phpMyAdmin 等依赖 PHP 的应用时，若系统无任何 PHP 环境则自动带装 PHP 7.4。
// 「默认版 PHP」应用定义已删除（2026-08-24），固定装 PHP 7.4 作为兜底版本。
func installDefaultPhp74(ctx context.Context, logPath string, logf func(format string, args ...any)) error {
	php74Meta, ok := findApp("php74")
	if !ok {
		return errors.New("未找到 PHP 7.4 应用定义")
	}
	if php74Meta.InstallScript == "" {
		return errors.New("PHP 7.4 应用未配置安装脚本")
	}
	logf("执行 PHP 7.4 安装脚本 ...")
	if err := runCmdWithLogCtx(ctx, php74Meta.InstallScript, logPath, appInstallTimeout); err != nil {
		return err
	}
	return nil
}

// UninstallApp 异步卸载应用
func UninstallApp(key string) error {
	meta, ok := findApp(key)
	if !ok {
		return errors.New("未知应用: " + key)
	}
	// 系统发行版自带的应用（Debian 默认 sqlite3 / python3 / golang / nodejs）：
	// 它们的 AptPackages 要么被系统其它组件依赖、要么卸载会静默成功但无实际效果，
	// 前端看上去「任务已启动」实际什么也没做。直接拒绝卸载，统一让用户走「启动 / 停止」控制服务。
	if systemDefaultKeys[key] {
		return errors.New("系统发行版自带的应用，不支持卸载（请用「启动 / 停止」控制服务）")
	}

	taskMu.Lock()
	if _, busy := appTasks[key]; busy {
		taskMu.Unlock()
		return errors.New("应用正在安装或卸载中，请稍候")
	}
	ctx, cancel := context.WithCancel(context.Background())
	appTasks[key] = &AppTask{
		Key:       key,
		Name:      meta.Name,
		Category:  meta.Category,
		Status:    model.AppUninstalling,
		Queued:    true, // 先进入全局串行队列，等前面的任务完成后再真正开始
		StartedAt: time.Now().Unix(),
		cancel:    cancel,
	}
	taskMu.Unlock()

	rec, err := model.GetAppRecord(key)
	if err != nil {
		// record 缺失：可能是系统自带环境（默认版 PHP 等），或通过其它途径装上但未走本面板安装流程。
		// 兜底探测：二进制/服务在跑 → 创建 installed record，让卸载流程能正常走下去
		if isMetaInstalled(meta) {
			rec = &model.AppRecord{Key: key, Status: model.AppInstalled, ServiceName: resolveServiceName(meta)}
			if v, verr := probeVersion(meta.VersionCmd); verr == nil {
				rec.Version = v
			}
		} else {
			taskMu.Lock()
			delete(appTasks, key)
			taskMu.Unlock()
			return errors.New("应用尚未安装")
		}
	}
	rec.Status = model.AppUninstalling
	_ = model.SaveAppRecord(rec)

	go func() {
		acquired := false
		defer func() {
			cancel()
			taskMu.Lock()
			delete(appTasks, key)
			taskMu.Unlock()
			if acquired {
				releaseInstallSlot()
			}
		}()
		// 全局串行队列：同一时刻只允许一个应用安装/卸载任务真正执行
		if !acquireInstallSlot(ctx) {
			return // 任务已被取消（排队期间点了停止）
		}
		acquired = true
		taskMu.Lock()
		if t, ok := appTasks[key]; ok {
			t.Queued = false // 正式开始执行
		}
		taskMu.Unlock()
		runUninstall(ctx, meta)
	}()
	return nil
}

func runUninstall(ctx context.Context, meta AppMeta) {
	logPath := AppLogPath(meta.Key)
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	_ = os.WriteFile(logPath, nil, 0o644)

	logf := func(format string, args ...any) {
		line := fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
		appendToFile(logPath, line)
	}

	// 一进入卸载流程就落一行状态，让前端「实时日志」立刻有内容，
	// 避免用户误以为卸载没在进行（尤其 serviceName 为空、走 apt/yum 卸载的纯包应用）
	logf("开始卸载 %s ...", meta.Name)

	// 任务被取消（用户点了"停止"）时，恢复为已安装状态
	defer func() {
		if ctx.Err() == context.Canceled {
			rec, _ := model.GetAppRecord(meta.Key)
			rec.Status = model.AppInstalled
			rec.Error = "已手动取消卸载"
			_ = model.SaveAppRecord(rec)
			InvalidateAppsCache()
			logf("卸载已取消")
		}
	}()

	serviceName := resolveServiceName(meta)
	if serviceName != "" {
		logf("停止并禁用服务 %s ...", serviceName)
		_ = runCmdWithLogCtx(ctx, fmt.Sprintf("systemctl stop %s; systemctl disable %s", serviceName, serviceName), logPath, 2*time.Minute)
	}

	if meta.UninstallScript != "" {
		// 自定义卸载脚本（多版本 PHP / phpMyAdmin 等）
		logf("执行自定义卸载脚本 ...")
		if err := runCmdWithLogCtx(ctx, meta.UninstallScript, logPath, appInstallTimeout); err != nil {
			logf("自定义卸载脚本执行失败: %v", err)
			return
		}
		rec, _ := model.GetAppRecord(meta.Key)
		rec.Status = model.AppNotInstalled
		rec.Version = ""
		rec.Error = ""
		rec.ServiceName = ""
		rec.InstalledAt = nil
		_ = model.SaveAppRecord(rec)
		removeOpenPorts(meta, logf)
		InvalidateAppsCache()
		InvalidateEnvStatusCache() // 环境状态已变化，失效缓存待重新探测
		logf("卸载完成")
		return
	}

	pkgMgr := detectPkgManager()
	var cmd string
	switch pkgMgr {
	case "apt":
		// 卸载时同时带上 fallback 包：实际安装的可能是替代包
		// （如 Debian 上 MySQL 走 AptFallbackPackages 装了 mariadb-server，
		//   只 remove mysql-server 会因包不存在而"假成功"，mariadb-server 残留在系统里）
		pkgs := append([]string{}, meta.AptPackages...)
		pkgs = append(pkgs, meta.AptFallbackPackages...)
		if len(pkgs) == 0 {
			logf("该应用没有可卸载的软件包（缺少 AptPackages/UninstallScript），请检查应用定义")
			return
		}
		cmd = fmt.Sprintf("apt-get remove -y --purge %s && apt-get autoremove -y", strings.Join(pkgs, " "))
	case "yum", "dnf":
		if len(meta.YumPackages) == 0 {
			logf("该应用没有可卸载的软件包（缺少 YumPackages/UninstallScript），请检查应用定义")
			return
		}
		cmd = fmt.Sprintf("%s remove -y %s", pkgMgr, strings.Join(meta.YumPackages, " "))
	default:
		logf("不支持的包管理器: %s", pkgMgr)
		return
	}
	logf("执行卸载命令: %s", cmd)
	if err := runCmdWithLogCtx(ctx, cmd, logPath, appInstallTimeout); err != nil {
		logf("卸载失败: %v", err)
		return
	}

	rec, _ := model.GetAppRecord(meta.Key)
	rec.Status = model.AppNotInstalled
	rec.Version = ""
	rec.Error = ""
	rec.ServiceName = ""
	rec.InstalledAt = nil
	_ = model.SaveAppRecord(rec)
	removeOpenPorts(meta, logf)
	InvalidateAppsCache()
	InvalidateEnvStatusCache() // 环境状态已变化，失效缓存待重新探测
	logf("卸载完成")
}

// removeOpenPorts 卸载成功后回收安装时自动放行的端口（OpenPorts）。
// 与 runInstall 里的自动放行成对，避免卸载后防火墙残留无用放行规则。
func removeOpenPorts(meta AppMeta, logf func(format string, args ...any)) {
	// 与 runInstall 成对：只回收 FTP 自动放行的端口，避免误删其它规则
	if meta.Key != "ftp" {
		return
	}
	for _, p := range meta.OpenPorts {
		if err := RemovePortWithSource(p, "tcp", "app:"+meta.Key); err != nil {
			logf("回收端口 %s 失败: %v", p, err)
		} else {
			logf("已回收端口 %s (tcp)", p)
		}
	}
}

// ServiceActionReq 服务操作请求
type ServiceActionReq struct {
	Key    string `json:"key" binding:"required"`
	Action string `json:"action" binding:"required,oneof=start stop restart"`
}

// ServiceAction 启停应用服务
func ServiceAction(req ServiceActionReq) error {
	meta, ok := findApp(req.Key)
	if !ok {
		return errors.New("未知应用: " + req.Key)
	}
	rec, err := model.GetAppRecord(meta.Key)
	installed := false
	if err != nil {
		// DB 无记录：允许对系统自带（二进制/systemd 服务存在）的应用操作
		installed = isMetaInstalled(meta)
	} else {
		installed = rec.Status == model.AppInstalled || isMetaInstalled(meta)
	}
	if !installed {
		return errors.New("应用尚未安装")
	}

	var serviceName string
	if rec != nil {
		serviceName = rec.ServiceName
	}
	if serviceName == "" {
		serviceName = resolveServiceName(meta)
	}
	if serviceName == "" {
		return errors.New("该应用没有可管理的服务")
	}

	cmd := fmt.Sprintf("systemctl %s %s", req.Action, serviceName)
	res, err := ExecCommand(cmd, 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("执行失败: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	// 记录操作时间（用于概览页服务按最近操作排序）
	serviceActionTimes[meta.Key] = time.Now()
	return nil
}

// ServiceStatus 返回某应用服务的运行状态
func ServiceStatus(key string) (string, error) {
	meta, ok := findApp(key)
	if !ok {
		return "", errors.New("未知应用: " + key)
	}
	rec, err := model.GetAppRecord(meta.Key)
	if err != nil {
		// DB 无记录：可能是系统自带应用（python3/sqlite 等，探测为 installed 但未写库）。
		// 系统自带应用没有面板可管理的 systemd 服务，返回 unknown 而非报错，
		// 避免前端 loadRunningKeys 对它们调 status 时弹「应用尚未安装」提示。
		if isMetaInstalled(meta) {
			if sn := resolveServiceName(meta); sn != "" {
				rec = &model.AppRecord{Key: key, Status: model.AppInstalled, ServiceName: sn}
			} else {
				return "unknown", nil
			}
		} else {
			return "", errors.New("应用尚未安装")
		}
	}
	serviceName := rec.ServiceName
	if serviceName == "" {
		serviceName = resolveServiceName(meta)
	}
	if serviceName == "" {
		return "unknown", nil
	}
	// 检测运行状态（active/inactive/failed）
	// 在 systemctl 不可用的环境（如腾讯云轻量）命令会挂起，只给 3s，失败一律按 unknown
	// （上层已用 10s 节流限制调用频率，避免空等堆积）
	res, err := ExecCommand(fmt.Sprintf("systemctl is-active %s", serviceName), 3*time.Second)
	if err != nil {
		return "unknown", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}

// findApp 按 key 查找应用元数据
// checkWebServerConflict 校验 Nginx 与 Apache 互斥：
// 两者均占用 80/443 端口，同时安装会冲突，只允许安装其中一个。
// 通过 PATH 中的二进制、systemd 服务单元、面板安装记录三层检测对方是否已安装。
func checkWebServerConflict(key string) error {
	var other, otherName string
	switch key {
	case "nginx":
		other, otherName = "apache", "Apache"
	case "apache":
		other, otherName = "nginx", "Nginx"
	default:
		return nil
	}
	installed := false
	if other == "apache" {
		_, err1 := LookPathBin("apache2")
		_, err2 := LookPathBin("httpd")
		installed = err1 == nil || err2 == nil || systemdUnitExists("apache2") || systemdUnitExists("httpd")
	} else {
		_, err := LookPathBin("nginx")
		installed = err == nil || systemdUnitExists("nginx")
	}
	// 面板安装记录兜底（对方二进制可能被改名或手动移除）
	if !installed {
		if rec, err := model.GetAppRecord(other); err == nil && rec.Status == model.AppInstalled {
			installed = true
		}
	}
	if installed {
		return fmt.Errorf("检测到系统已安装 %s。Nginx 与 Apache 同时安装会抢占 80/443 端口导致冲突，请先在「应用商店」卸载 %s 后再安装", otherName, otherName)
	}
	return nil
}
