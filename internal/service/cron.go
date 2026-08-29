package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ListCrons 列出计划任务
func ListCrons() []model.Cron {
	var list []model.Cron
	model.DB.Order("id DESC").Find(&list)
	return list
}

// CreateCronReq 创建/更新任务请求
type CreateCronReq struct {
	ID         uint   `json:"id"`
	Name       string `json:"name" binding:"required"`
	Spec       string `json:"spec" binding:"required"`
	Command    string `json:"command" binding:"required"`
	Remark     string `json:"remark"`
	Template   string `json:"template"`
	SiteName   string `json:"site_name"`  // 多个用英文逗号分隔；"*" 代表全部
	SiteRoot   string `json:"site_root"`  // 多个用英文逗号分隔，与 SiteName 一一对应
	Database   string `json:"database"`   // 多个用英文逗号分隔；"*" 代表全部
	Dir        string `json:"dir"`
	URL        string `json:"url"`
	Days       int    `json:"days"`
	Keep       int    `json:"keep"`
	Format     string `json:"format"`
	TargetType string `json:"target_type"` // local / remote
	TargetName string `json:"target_name"`
	RemoteKeep int    `json:"remote_keep"`
}

// validCronSpec 校验 cron 表达式（5 段）
func validCronSpec(spec string) error {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return errors.New("cron 表达式需为 5 段，如 */5 * * * *")
	}
	validField := func(f string) bool {
		if f == "*" {
			return true
		}
		for _, part := range strings.Split(f, ",") {
			if part == "" {
				return false
			}
			for _, c := range part {
				if !(c >= '0' && c <= '9' || c == '*' || c == '/' || c == '-' || c == ',') {
					return false
				}
			}
		}
		return true
	}
	for _, f := range fields {
		if !validField(f) {
			return errors.New("cron 表达式包含非法字符")
		}
	}
	return nil
}

// CreateCron 创建或更新计划任务
func CreateCron(req CreateCronReq) error {
	if err := validCronSpec(req.Spec); err != nil {
		return err
	}
	if len(req.Command) > 2000 {
		return errors.New("命令过长")
	}
	// 危险命令校验
	if err := checkForbidden(req.Command); err != nil {
		return err
	}

	var c model.Cron
	if req.ID > 0 {
		if err := model.DB.First(&c, req.ID).Error; err != nil {
			return errors.New("任务不存在")
		}
		c.Name = req.Name
		c.Spec = req.Spec
		c.Command = req.Command
		c.Remark = req.Remark
		c.Template = req.Template
		c.SiteName = req.SiteName
		c.SiteRoot = req.SiteRoot
		c.Database = req.Database
		c.Dir = req.Dir
		c.URL = req.URL
		c.Days = req.Days
		c.Keep = req.Keep
		c.Format = req.Format
		c.TargetType = req.TargetType
		c.TargetName = req.TargetName
		c.RemoteKeep = req.RemoteKeep
		if err := model.DB.Save(&c).Error; err != nil {
			return err
		}
	} else {
		c = model.Cron{
			Name: req.Name, Spec: req.Spec, Command: req.Command, Remark: req.Remark,
			Template: req.Template, SiteName: req.SiteName, SiteRoot: req.SiteRoot,
			Database: req.Database, Dir: req.Dir, URL: req.URL,
			Days: req.Days, Keep: req.Keep, Format: req.Format,
			TargetType: req.TargetType, TargetName: req.TargetName, RemoteKeep: req.RemoteKeep,
			Status: "enabled",
		}
		if err := model.DB.Create(&c).Error; err != nil {
			return err
		}
	}
	return SyncCrontab()
}

// DeleteCron 删除任务
func DeleteCron(id uint) error {
	var c model.Cron
	if err := model.DB.First(&c, id).Error; err != nil {
		return errors.New("任务不存在")
	}
	if err := model.DB.Delete(&c).Error; err != nil {
		return err
	}
	return SyncCrontab()
}

// ToggleCron 启用/禁用任务
func ToggleCron(id uint, enable bool) error {
	var c model.Cron
	if err := model.DB.First(&c, id).Error; err != nil {
		return errors.New("任务不存在")
	}
	status := "disabled"
	if enable {
		status = "enabled"
	}
	if err := model.DB.Model(&c).Update("status", status).Error; err != nil {
		return err
	}
	return SyncCrontab()
}

// RunCronNow 立即执行一次任务
func RunCronNow(id uint) error {
	var c model.Cron
	if err := model.DB.First(&c, id).Error; err != nil {
		return errors.New("任务不存在")
	}
	go func(task model.Cron) {
		now := time.Now()
		logFile := cronLogPath(task.ID)
		_ = os.MkdirAll(cronLogDir(), 0o755)
		// 包一层确保即使任务命令完全静默（无 stdout/stderr）也能留下开始/结束/退出码日志，
		// 避免"执行成功但日志文件为空、看着像没跑"。
		wrapped := fmt.Sprintf(
			"{\n  echo \"==== 开始执行 [%s] %s ====\";\n  echo \"命令: %s\";\n  %s;\n  ec=$?;\n  echo \"==== 结束执行，退出码 $ec ====\";\n  exit $ec;\n} >> %s 2>&1",
			now.Format("2006-01-02 15:04:05"), task.Name, task.Command,
			task.Command, shellQuote(logFile))
		res, err := ExecCommand(wrapped, 10*time.Minute)
		result := ""
		if err != nil {
			result = "错误: " + err.Error()
		} else if res.ExitCode != 0 {
			result = fmt.Sprintf("退出码 %d: %s", res.ExitCode, firstLine(res.Stderr+res.Stdout))
		} else {
			result = "执行成功"
		}
		_ = model.DB.Model(&model.Cron{}).Where("id = ?", task.ID).
			Updates(map[string]any{"last_run": &now, "last_result": result, "run_count": gorm.Expr("COALESCE(run_count, 0) + 1")}).Error
	}(c)
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

// cronLogDir 计划任务日志目录
func cronLogDir() string {
	return filepath.Join(config.Get().DataDir, "logs", "cron")
}

// cronLogPath 单个任务的日志文件路径
func cronLogPath(cronID uint) string {
	return filepath.Join(cronLogDir(), fmt.Sprintf("%d.log", cronID))
}

// cronWrapperScript 返回 cron 任务统一 wrapper 脚本的绝对路径。
// 在程序启动时确保该脚本存在，crontab 行通过它执行用户命令并自动写入
// 开始/结束/退出码标记，保证即使静默命令也能留下日志。
func cronWrapperScript() string {
	return filepath.Join(config.Get().DataDir, "scripts", "cron-wrapper.sh")
}

// EnsureCronWrapper 确保 wrapper 脚本存在并有可执行权限。
// 写入时机：面板启动初始化时（InitCrons）以及每次刷新 crontab 前，
// 保证手动删掉脚本后也能自动恢复。
func EnsureCronWrapper() error {
	p := cronWrapperScript()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	content := `#!/bin/bash
# kypanel 计划任务统一 wrapper：由 cron daemon 调用此脚本
# 用法: cron-wrapper.sh <task_id> <command...>
# 行为:
#   1. 输出开始执行标记（包含时间、任务名）
#   2. 执行用户提供的命令，捕获 stdout/stderr
#   3. 输出结束执行标记（含退出码）
# 注意：本脚本通过 stdout 被 crontab 重定向到任务日志文件

TASK_ID="$1"
shift
CMD="$*"

printf "\n==== [kypanel cron #%s] 开始执行 %s ====\n" "$TASK_ID" "$(date '+%Y-%m-%d %H:%M:%S')"
printf "命令: %s\n" "$CMD"

# 执行用户命令（保留原始退出码）
# 注意：crontab 行把整条命令（可能含空格）作为单个参数传入，因此必须用 bash -c 重新解析执行，
# 直接用 "$@" 会把整条命令当成单个可执行文件路径而报 No such file or directory。
bash -c "$CMD"
EC=$?

printf "==== [kypanel cron #%s] 结束执行，退出码 %s ====\n" "$TASK_ID" "$EC"

# 通知面板更新执行计数 + last_run + last_result（不阻塞，失败也不影响退出码）
# 通过环境变量 KYPANEL_API_BASE 指定面板地址，缺省默认 localhost:9999
KYPANEL_API_BASE="${KYPANEL_API_BASE:-http://127.0.0.1:9999}"
KYPANEL_INTERNAL_TOKEN="${KYPANEL_INTERNAL_TOKEN:-${KYPANEL_TOKEN:-}}"
HEADERS=(-H "Content-Type: application/json" -H "X-Kypanel-Internal: 1")
if [ -n "$KYPANEL_INTERNAL_TOKEN" ]; then
  HEADERS+=(-H "Authorization: Bearer $KYPANEL_INTERNAL_TOKEN")
fi
curl -fsS -m 5 "${HEADERS[@]}" \
  -d "{\"id\":$TASK_ID,\"exit_code\":$EC}" \
  "${KYPANEL_API_BASE}/api/cron/run-complete" >/dev/null 2>&1 || true

# 若该任务配置了"备份到远程存储"，触发面板上传（面板内部判断，未配置则直接跳过）。
# 超时设 2 小时以覆盖大文件传输；失败不影响任务退出码。
if [ "$EC" -eq 0 ]; then
  curl -fsS -m 7200 "${HEADERS[@]}" \
    -d "{\"id\":$TASK_ID}" \
    "${KYPANEL_API_BASE}/api/cron/backup-upload" >/dev/null 2>&1 || true
fi

exit $EC
`
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		return err
	}
	return nil
}

// CronLog 读取某个任务的执行日志（最近 200 行）
func CronLog(cronID uint) (string, error) {
	path := cronLogPath(cronID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "（暂无执行日志，任务执行后这里会显示输出）", nil
		}
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	return strings.Join(lines, "\n"), nil
}

// syncCrontab 把面板任务同步到系统 crontab
func SyncCrontab() error {
	// 先确保 wrapper 脚本存在，否则 cron 行无法工作
	_ = EnsureCronWrapper()
	// 确保任务日志目录存在（crontab 行通过 >> 重定向写日志，crond 不会自动创建父目录）
	_ = os.MkdirAll(cronLogDir(), 0o755)

	var list []model.Cron
	model.DB.Order("id ASC").Find(&list)

	// 读取现有 crontab，保留非面板管理的行
	existing := ""
	if res, err := ExecCommand("crontab -l 2>/dev/null", 15*time.Second); err == nil && res.ExitCode == 0 {
		existing = res.Stdout
	}
	var keep []string
	inBlock := false
	for _, line := range strings.Split(existing, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# === kypanel-task-") {
			inBlock = true
			continue
		}
		if strings.HasPrefix(t, "# === end-kypanel-task-") {
			inBlock = false
			continue
		}
		if !inBlock {
			keep = append(keep, line)
		}
	}

	// 生成新的 crontab 内容
	var sb strings.Builder
	for _, l := range keep {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	for _, c := range list {
		if c.Status != "enabled" {
			continue
		}
		// 输出追加到任务专属日志文件，面板可查看每次执行详情。
		// 使用统一的 wrapper 脚本：保证即使任务命令完全静默也能留下开始/结束/退出码日志。
		logFile := cronLogPath(c.ID)
		block := fmt.Sprintf("# === kypanel-task-%d ===\n%s /bin/bash %s %d %s >> %s 2>&1\n# === end-kypanel-task-%d ===\n",
			c.ID, c.Spec, shellQuote(cronWrapperScript()), c.ID, shellQuote(c.Command), shellQuote(logFile), c.ID)
		sb.WriteString(block)
	}

	// 写回
	res, err := ExecCommand(fmt.Sprintf("echo %s | crontab -", shellQuote(sb.String())), 15*time.Second)
	if err != nil {
		return errors.New("写入 crontab 失败: " + err.Error())
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}
