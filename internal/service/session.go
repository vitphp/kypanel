package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"kypanel/internal/model"
)

// ============================ 登录 IP 白名单 ============================

// GetLoginAllowList 返回登录 IP 白名单（逗号分隔的 IP/网段，为空表示不限制）
func GetLoginAllowList() []string {
	raw := model.GetSetting("login_allowlist")
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		// 兼容旧的逗号分隔格式
		return strings.Split(raw, ",")
	}
	return list
}

// SetLoginAllowList 设置登录 IP 白名单
func SetLoginAllowList(list []string) error {
	b, _ := json.Marshal(list)
	return model.SetSetting("login_allowlist", string(b))
}

// CheckLoginAllowed 校验来源 IP 是否允许登录。
// 白名单为空 = 不限制；本机回环始终放行（防止管理员误锁自己）。
func CheckLoginAllowed(ip string) bool {
	list := GetLoginAllowList()
	if len(list) == 0 {
		return true
	}
	if isLoopbackIP(ip) {
		return true
	}
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 网段格式（含 /）
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if parsed := net.ParseIP(ip); parsed != nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		if entry == ip {
			return true
		}
	}
	return false
}

func isLoopbackIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// ============================ 会话管理 ============================

// tokenFingerprint 生成 token 指纹（sha256 前 16 字节 hex）
func tokenFingerprint(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}

// CreateSession 登录成功后记录会话（upsert：同用户同 IP 只保留一条记录）。
// 同一用户在同一 IP 多次登录（无论浏览器/UA），合并为同一行（更新 token_hash/last_seen/active/user_agent/created_at）。
// 不同 IP → 新行（允许多设备/异地同时登录）。
func CreateSession(adminID uint, username, token, ip, userAgent string) {
	now := time.Now()
	sess := model.LoginSession{
		AdminID:   adminID,
		Username:  username,
		TokenHash: tokenFingerprint(token),
		IP:        ip,
		UserAgent: userAgent,
		Active:    true,
		CreatedAt: now,
		LastSeen:  now,
	}
	// OnConflict 按 (admin_id, ip) 唯一索引：更新 token_hash/user_agent/last_seen/active/created_at
	_ = model.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "admin_id"}, {Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"token_hash", "user_agent", "last_seen", "active", "created_at",
		}),
	}).Create(&sess).Error
}

// TouchSession 更新会话最后活跃时间（由鉴权中间件调用，调用方应做低频节流避免每次请求都写 DB）。
func TouchSession(tokenHash string) {
	now := time.Now()
	model.DB.Model(&model.LoginSession{}).
		Where("token_hash = ? AND active = ?", tokenHash, true).
		Updates(map[string]interface{}{
			"last_seen": now,
			"active":    true,
		})
}

// IsSessionActive 检查 token 对应的会话是否仍活跃（未被踢下线）。
// 返回 true 表示活跃可访问；返回 false 表示该会话被踢下线（应在鉴权时拒绝）。
// 如果该 token 还没在 LoginSession 表中（极少见，例如历史数据缺失），按"活跃"处理（不阻塞登录态）。
func IsSessionActive(tokenHash string) bool {
	if tokenHash == "" {
		return true
	}
	var count int64
	model.DB.Model(&model.LoginSession{}).
		Where("token_hash = ? AND active = ?", tokenHash, true).
		Count(&count)
	if count == 0 {
		// 进一步判断：可能根本没记录（历史缺失）也可能被踢了（active=false）
		var total int64
		model.DB.Model(&model.LoginSession{}).Where("token_hash = ?", tokenHash).Count(&total)
		return total == 0 // 无记录 = 放行（不阻塞登录态）；有记录但都 inactive = 拒绝
	}
	return true
}

// CleanSessionDuplicates 启动时调用一次：清理历史重复会话记录。
// 解决历史遗留：早期 LoginSession 每次登录都创建一条独立记录（即便同一用户同 IP 每次都是新 token），
// 导致"在线会话"页堆积几十条同 IP / 同 admin 的记录。
//
// 策略：按 (admin_id, ip) 分组，保留每组最新的（id 最大）一条，删除其他。
// 这与 CreateSession 的 upsert 语义一致：同一用户同 IP 只保留一条会话（忽略 UA 噪声）。
func CleanSessionDuplicates() (int, error) {
	// 1) 找出每组 (admin_id, ip) 保留的 id（MAX(id)）
	type row struct {
		AdminID uint
		IP      string
		KeepID  uint
	}
	keep := map[string]uint{}
	rows, err := model.DB.Model(&model.LoginSession{}).
		Select("admin_id, ip, MAX(id) as keep_id").
		Group("admin_id, ip").
		Rows()
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.AdminID, &r.IP, &r.KeepID); err == nil {
			keep[strconv.FormatUint(uint64(r.AdminID), 10)+"|"+r.IP] = r.KeepID
		}
	}
	rows.Close()

	// 2) 对每组：删掉非 keep_id 的所有行
	deleted := int64(0)
	for key, keepID := range keep {
		parts := strings.SplitN(key, "|", 2)
		adminID, _ := strconv.Atoi(parts[0])
		ip := parts[1]
		res := model.DB.Where("admin_id = ? AND ip = ? AND id <> ?",
			adminID, ip, keepID).Delete(&model.LoginSession{})
		deleted += res.RowsAffected
	}
	return int(deleted), nil
}

// SessionView 会话列表展示（含 is_current 标记）
type SessionView struct {
	ID        uint      `json:"id"`
	AdminID   uint      `json:"admin_id"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	IsCurrent bool      `json:"is_current"`  // 是否当前请求的会话（用于避免误踢自己）
	Region    *IpRegion `json:"region,omitempty"` // IP 归属地（依赖 ip2region.xdb 离线库）
}

// ListSessions 列出会话（仅活跃会话，按 last_seen 倒序）。
// 只返回 active=true 的会话：被踢下线 / 已掉线的记录（active=false）不展示，
// 从「在线会话」列表自动消失（但记录仍保留在 DB，用于 IsSessionActive 拒绝被踢 token）。
// currentTokenFingerprint 传当前请求的 token 指纹，对应记录会标 is_current=true。
func ListSessions(currentTokenFingerprint string) []SessionView {
	var sess []model.LoginSession
	model.DB.Where("active = ?", true).Order("last_seen desc").Limit(200).Find(&sess)
	out := make([]SessionView, 0, len(sess))
	for _, s := range sess {
		view := SessionView{
			ID:        s.ID,
			AdminID:   s.AdminID,
			Username:  s.Username,
			IP:        s.IP,
			UserAgent: s.UserAgent,
			Active:    s.Active,
			CreatedAt: s.CreatedAt,
			LastSeen:  s.LastSeen,
			IsCurrent: currentTokenFingerprint != "" && s.TokenHash == currentTokenFingerprint,
		}
		// IP 归属地查询（依赖 ip2region.xdb 离线库，未加载时该字段为 nil，前端可省略展示）
		if region, ok := SearchIp(s.IP); ok {
			view.Region = region
		}
		out = append(out, view)
	}
	return out
}

// KickSession 踢下线：标记指定会话为 inactive（不影响该 admin 的其他会话/TokenVer）。
// Auth() 鉴权时不再主动调 TouchSession 恢复 active=true（避免被踢后自动复活）。
func KickSession(sessionID uint) error {
	res := model.DB.Model(&model.LoginSession{}).
		Where("id = ?", sessionID).
		Update("active", false)
	return res.Error
}

// KickOtherSessions 踢掉指定管理员的所有会话（保留当前 token 不受影响）。
// currentTokenFingerprint 传当前请求的 token 指纹，该 token 对应的会话不会被踢。
func KickOtherSessions(adminID uint, currentTokenFingerprint string) int64 {
	q := model.DB.Model(&model.LoginSession{}).
		Where("admin_id = ? AND active = ?", adminID, true)
	if currentTokenFingerprint != "" {
		q = q.Where("token_hash <> ?", currentTokenFingerprint)
	}
	res := q.Update("active", false)
	return res.RowsAffected
}

// CleanInactiveSessions 清理已下线（active=false）的会话记录，避免列表堆积。
// keepRecent: 保留最近 N 条已下线的作为历史；0 = 全部删除已下线的。
func CleanInactiveSessions(keepRecent int) int64 {
	q := model.DB.Model(&model.LoginSession{}).Where("active = ?", false)
	if keepRecent > 0 {
		sub := model.DB.Model(&model.LoginSession{}).
			Select("id").Where("active = ?", false).
			Order("last_seen DESC").Limit(keepRecent)
		q = q.Where("id NOT IN (?)", sub)
	}
	res := q.Delete(&model.LoginSession{})
	return res.RowsAffected
}

// bumpTokenVer 使管理员 TokenVer+1，所有旧 token 立即失效
func bumpTokenVer(adminID uint) error {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return err
	}
	admin.TokenVer++
	if err := model.DB.Save(&admin).Error; err != nil {
		return err
	}
	InvalidateTokenVer(adminID)
	return nil
}
