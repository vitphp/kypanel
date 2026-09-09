package service

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 操作来源常量
const (
	OpSourceLogin = "login" // 登录会话 JWT（前端）
	OpSourceAPI   = "api"   // api token（开放 API）
	OpSourceMCP   = "mcp"   // mcp token（AI 助手）
	OpSourceTemp  = "temp"  // 临时访问（临时登录链接）
)

// RecordOp 记录操作日志（默认 source=login，向后兼容所有现存调用点）
func RecordOp(adminID uint, action, detail, ip, status string, rawCmd ...string) {
	rc := ""
	if len(rawCmd) > 0 {
		rc = rawCmd[0]
	}
	op := model.OperationLog{
		AdminID:   adminID,
		Action:    action,
		Detail:    detail,
		RawCmd:    rc,
		IP:        ip,
		Status:    status,
		Source:    OpSourceLogin,
		CreatedAt: time.Now(),
	}
	if err := model.DB.Create(&op).Error; err != nil {
		slog.Warn("写入操作日志失败", "action", action, "err", err)
	}
}

// RecordOpWithSource 按来源记录操作日志（纯参数形式，不依赖任何 Web 框架）。
// 由 router 层从 gin.Context 提取字段后调用本函数。
//   - source=api   → action 加 "api." 前缀
//   - source=mcp   → action 加 "mcp." 前缀
//   - source=temp  → action 加 "temp." 前缀，并写入 tempAccessID
//   - source=login → action 不变
//   rawCmd 为可选的原始命令/输入，存储到 RawCmd 字段便于明文追溯。
func RecordOpWithSource(source string, adminID, tempAccessID uint, ip, action, detail, status string, rawCmd ...string) {
	rc := ""
	if len(rawCmd) > 0 {
		rc = rawCmd[0]
	}
	now := time.Now()
	switch source {
	case OpSourceAPI, "api_token":
		op := model.OperationLog{
			AdminID:   adminID,
			Action:    "api." + action,
			Detail:    detail,
			RawCmd:    rc,
			IP:        ip,
			Status:    status,
			Source:    OpSourceAPI,
			CreatedAt: now,
		}
		if err := model.DB.Create(&op).Error; err != nil {
			slog.Warn("写入操作日志失败", "action", action, "err", err)
		}
	case OpSourceMCP, "mcp_token":
		op := model.OperationLog{
			AdminID:   adminID,
			Action:    "mcp." + action,
			Detail:    detail,
			RawCmd:    rc,
			IP:        ip,
			Status:    status,
			Source:    OpSourceMCP,
			CreatedAt: now,
		}
		if err := model.DB.Create(&op).Error; err != nil {
			slog.Warn("写入操作日志失败", "action", action, "err", err)
		}
	case OpSourceTemp:
		op := model.OperationLog{
			AdminID:      adminID,
			Action:       "temp." + action,
			Detail:       detail,
			RawCmd:       rc,
			IP:           ip,
			Status:       status,
			Source:       OpSourceTemp,
			TempAccessID: tempAccessID,
			CreatedAt:    now,
		}
		if err := model.DB.Create(&op).Error; err != nil {
			slog.Warn("写入操作日志失败", "action", action, "err", err)
		}
	default:
		// jwt / 空 → 视为登录会话操作
		RecordOp(adminID, action, detail, ip, status, rawCmd...)
	}
}

// ListOps 分页查询操作日志
// source 过滤：
//   - "login" → action NOT LIKE 'api.%' AND NOT LIKE 'mcp.%'（前端会话操作，默认）
//   - "api"   → action LIKE 'api.%'（API token 调用）
//   - "mcp"   → action LIKE 'mcp.%'（MCP token 调用）
//   - "" / 其他 → 不过滤
func ListOps(page, pageSize int, source string) ([]model.OperationLog, int64, error) {
	var total int64
	var list []model.OperationLog

	q := model.DB.Model(&model.OperationLog{})
	switch source {
	case OpSourceAPI:
		q = q.Where("action LIKE ?", "api.%")
	case OpSourceMCP:
		q = q.Where("action LIKE ?", "mcp.%")
	case OpSourceTemp:
		q = q.Where("action LIKE ?", "temp.%")
	case OpSourceLogin, "":
		// login：排除 api. / mcp. / temp. 前缀，让"操作日志"Tab 只看前端会话操作
		q = q.Where("action NOT LIKE ? AND action NOT LIKE ? AND action NOT LIKE ?", "api.%", "mcp.%", "temp.%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListOpsByTempAccess 按临时访问 ID 分页查询关联的操作日志（Source=temp）
func ListOpsByTempAccess(tempAccessID uint, page, pageSize int) ([]model.OperationLog, int64, error) {
	var total int64
	var list []model.OperationLog
	if err := model.DB.Model(&model.OperationLog{}).Where("temp_access_id = ?", tempAccessID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := model.DB.Where("temp_access_id = ?", tempAccessID).
		Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ExportOpsCSV 导出全部操作日志为 CSV 内容（含 UTF-8 BOM，Excel 兼容）
func ExportOpsCSV() ([]byte, error) {
	var list []model.OperationLog
	if err := model.DB.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	var sb strings.Builder
	// UTF-8 BOM
	sb.WriteString("\xEF\xBB\xBF")
	sb.WriteString("ID,账号ID,操作类型,操作详情,原始命令,来源,来源IP,状态,时间\n")
	for _, op := range list {
		line := fmt.Sprintf("%d,%d,%s,%s,%s,%s,%s,%s,%s\n",
			op.ID, op.AdminID,
			csvEscape(op.Action),
			csvEscape(op.Detail),
			csvEscape(op.RawCmd),
			op.Source,
			csvEscape(op.IP),
			op.Status,
			op.CreatedAt.Format("2006-01-02 15:04:05"),
		)
		sb.WriteString(line)
	}
	return []byte(sb.String()), nil
}

// ClearOps 清空所有操作日志
func ClearOps() error {
	return model.DB.Where("1 = 1").Delete(&model.OperationLog{}).Error
}

// ClearOpsBySource 按来源清空操作日志（如单独清空 API 日志/MCP 日志）
func ClearOpsBySource(source string) error {
	q := model.DB.Model(&model.OperationLog{})
	switch source {
	case OpSourceAPI:
		q = q.Where("action LIKE ?", "api.%")
	case OpSourceMCP:
		q = q.Where("action LIKE ?", "mcp.%")
	default:
		return nil
	}
	return q.Delete(&model.OperationLog{}).Error
}

// CleanOpsBefore 清理指定时间之前的操作日志（用于自动清理策略），返回删除条数
func CleanOpsBefore(before time.Time) int64 {
	res := model.DB.Where("created_at < ?", before).Delete(&model.OperationLog{})
	if res.Error != nil {
		slog.Warn("清理过期操作日志失败", "err", res.Error)
	}
	return res.RowsAffected
}

// opsLogRetention 操作日志保留时长（默认 90 天；超期自动清理，避免表无限膨胀拖累 SQLite）。
const opsLogRetention = 90 * 24 * time.Hour

// StartOpLogJanitor 启动操作日志定期清理协程（每日一次）：
// 删除超过保留期的操作日志，并顺带对 panel.db 执行 VACUUM 回收 SQLite 文件空间
// （WAL 模式下日常 Delete 不会收缩 db 文件，长期不整理会持续膨胀）。
// 低频（每日、选在整点后空闲段）执行，VACUUM 短暂锁库的影响可忽略。
func StartOpLogJanitor() {
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		// 启动 1 分钟后再首跑，避开服务刚启动的繁忙期
		time.Sleep(time.Minute)
		housekeeping()
		for range t.C {
			housekeeping()
		}
	}()
}

func housekeeping() {
	if model.DB == nil {
		return
	}
	// 1) 操作日志按保留期清理
	if n := CleanOpsBefore(time.Now().Add(-opsLogRetention)); n > 0 {
		slog.Info("自动清理过期操作日志", "removed", n)
	}
	// 2) VACUUM 回收磁盘空间（WAL 下自动 checkpoint 后收缩）
	if err := model.DB.Exec("VACUUM").Error; err != nil {
		slog.Warn("数据库 VACUUM 失败（可忽略，下次重试）", "err", err)
	}
}

// csvEscape CSV 字段转义（含逗号/引号/换行时用引号包裹）
func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}
