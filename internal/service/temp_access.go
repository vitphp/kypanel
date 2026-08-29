package service

import (
	"fmt"
	"time"

	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// TempAccessTokenPrefix 临时访问 token 前缀（区别于登录 JWT / API token）
const TempAccessTokenPrefix = "lp_temp_"

// CreateTempAccessReq 创建临时访问请求
type CreateTempAccessReq struct {
	Name       string `json:"name"`        // 备注名
	MaxUses    int    `json:"max_uses"`    // 最大使用次数，0=不限
	ExpireSecs int    `json:"expire_secs"` // 有效期（秒），0=永不过期
}

// TempAccessView 临时访问展示（脱敏，不含完整 token）
type TempAccessView struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	MaxUses     int        `json:"max_uses"`
	UsedCount   int        `json:"used_count"`
	ExpireAt    *time.Time `json:"expire_at"`
	LastIP      string     `json:"last_ip"`
	LastRegion  string     `json:"last_region"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	Status      int        `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateTempAccess 创建临时访问，返回 (明文 token, 完整链接, view, error)
func CreateTempAccess(req CreateTempAccessReq, baseURL string) (string, string, *TempAccessView, error) {
	key, err := utils.GenerateAPIKey()
	if err != nil {
		return "", "", nil, err
	}
	token := TempAccessTokenPrefix + key

	// token 不以明文落库：加密存储（可还原用于复制链接），并保存 SHA-256 指纹供索引查询。
	// 即使 panel.db 泄露，攻击者也无法直接得到可用 token。
	encToken, err := utils.EncryptString(token)
	if err != nil {
		return "", "", nil, err
	}
	t := model.TempAccess{
		Token:     encToken,
		TokenHash: utils.SHA256Hex(token),
		Name:      req.Name,
		MaxUses:   req.MaxUses,
		Status:    1,
	}
	if req.ExpireSecs > 0 {
		exp := time.Now().Add(time.Duration(req.ExpireSecs) * time.Second)
		t.ExpireAt = &exp
	}
	if err := model.DB.Create(&t).Error; err != nil {
		return "", "", nil, err
	}
	view := toTempAccessView(&t)
	link := fmt.Sprintf("%s/temp-login?token=%s", baseURL, token)
	return token, link, view, nil
}

func toTempAccessView(t *model.TempAccess) *TempAccessView {
	v := &TempAccessView{
		ID:         t.ID,
		Name:       t.Name,
		MaxUses:    t.MaxUses,
		UsedCount:  t.UsedCount,
		ExpireAt:   t.ExpireAt,
		LastIP:     t.LastIP,
		LastUsedAt: t.LastUsedAt,
		Status:     t.Status,
		CreatedAt:  t.CreatedAt,
	}
	// 归属地：从最后使用 IP 反查（若有）
	if t.LastIP != "" {
		if reg, ok := SearchIp(t.LastIP); ok {
			v.LastRegion = fmt.Sprintf("%s %s %s", reg.Country, reg.Province, reg.City)
		}
	}
	return v
}

// ListTempAccess 列出所有临时访问
func ListTempAccess() []TempAccessView {
	var list []model.TempAccess
	model.DB.Order("id DESC").Find(&list)
	out := make([]TempAccessView, 0, len(list))
	for i := range list {
		out = append(out, *toTempAccessView(&list[i]))
	}
	return out
}

// DeleteTempAccess 删除临时访问
func DeleteTempAccess(id uint) error {
	// 同时清理使用日志
	model.DB.Where("temp_access_id = ?", id).Delete(&model.TempAccessUseLog{})
	return model.DB.Delete(&model.TempAccess{}, id).Error
}

// ToggleTempAccess 启用/禁用临时访问
func ToggleTempAccess(id uint) error {
	var t model.TempAccess
	if err := model.DB.First(&t, id).Error; err != nil {
		return err
	}
	if t.Status == 1 {
		t.Status = 0
	} else {
		t.Status = 1
	}
	return model.DB.Save(&t).Error
}

// GetTempAccessLink 按 id 重新拼接完整链接（解密加密存储的 token）
func GetTempAccessLink(id uint, baseURL string) (string, error) {
	var t model.TempAccess
	if err := model.DB.First(&t, id).Error; err != nil {
		return "", fmt.Errorf("临时链接不存在")
	}
	plain, err := utils.DecryptString(t.Token)
	if err != nil {
		return "", fmt.Errorf("临时链接解码失败")
	}
	return fmt.Sprintf("%s/temp-login?token=%s", baseURL, plain), nil
}

// CheckTempAccessValid 只读校验临时访问 token 是否有效（不消耗使用次数、不写日志、不更新最后使用信息）。
// 供 /temp-login 路由放行判断使用：避免 NoRoute 放行校验扣 1 次次数后，
// 前端随后调 /system/info 二次校验时因次数耗尽（MaxUses=1）而 401。
func CheckTempAccessValid(token string) bool {
	if token == "" {
		return false
	}
	var t model.TempAccess
	hash := utils.SHA256Hex(token)
	err := model.DB.Where("token_hash = ?", hash).First(&t).Error
	if err != nil {
		if err2 := model.DB.Where("token = ?", token).First(&t).Error; err2 != nil {
			return false
		}
	}
	if t.Status != 1 {
		return false
	}
	if t.ExpireAt != nil && time.Now().After(*t.ExpireAt) {
		return false
	}
	if t.MaxUses > 0 && t.UsedCount >= t.MaxUses {
		return false
	}
	return true
}

// CheckTempAccessSessionAlive 检查临时访问 token 对应的登录会话是否仍然存活：
// 仅校验 token 存在、已启用、未过期；不检查使用次数。
// 供 isLoggedInByCookie 判断临时登录态：登录时已消费 1 次使用次数，
// 之后刷新页面（cookie 校验）不能再因为次数耗尽而判定失效，否则刷新即 404。
func CheckTempAccessSessionAlive(token string) bool {
	if token == "" {
		return false
	}
	var t model.TempAccess
	hash := utils.SHA256Hex(token)
	err := model.DB.Where("token_hash = ?", hash).First(&t).Error
	if err != nil {
		if err2 := model.DB.Where("token = ?", token).First(&t).Error; err2 != nil {
			return false
		}
	}
	if t.Status != 1 {
		return false
	}
	if t.ExpireAt != nil && time.Now().After(*t.ExpireAt) {
		return false
	}
	return true
}

// VerifyTempAccess 校验临时访问 token（供鉴权中间件调用）。
// 返回 (tempAccessID, ok, reason)。tempAccessID 用于在 OperationLog 中关联"本次临时访问 session"。
// reason 用于日志记录失败原因。
//
// 去重窗口 24 小时：同 token 在窗口内的重复访问只算 1 次使用（覆盖整个登录会话，
// 避免前端每次 API 请求都扣 1 次导致链接快速耗尽）。
// 窗口内：UsedCount 不增加，不重复记录使用日志，但 last_used_at 仍会更新（用于"最后使用时间"展示）。
func VerifyTempAccess(token, ip, userAgent string) (uint, bool, string) {
	var t model.TempAccess
	// 优先按 token 指纹查询（新数据）；找不到再按明文匹配（兼容历史明文数据）
	hash := utils.SHA256Hex(token)
	err := model.DB.Where("token_hash = ?", hash).First(&t).Error
	if err != nil {
		if err2 := model.DB.Where("token = ?", token).First(&t).Error; err2 != nil {
			return 0, false, "临时链接不存在"
		}
	}
	if t.Status != 1 {
		return 0, false, "临时链接已禁用"
	}
	if t.ExpireAt != nil && time.Now().After(*t.ExpireAt) {
		return 0, false, "临时链接已过期"
	}
	if t.MaxUses > 0 && t.UsedCount >= t.MaxUses {
		return 0, false, "临时链接使用次数已用完"
	}

	// 归属地
	region := ""
	if reg, ok := SearchIp(ip); ok {
		region = fmt.Sprintf("%s|%s|%s|%s", reg.Country, reg.Province, reg.City, reg.ISP)
	}

	// 使用次数 +1，更新最后使用信息
	now := time.Now()
	// 去重窗口：24 小时内同 token 重复访问不算使用次数（仅刷新 last_used_at）。
	// 这是「临时登录会话」的语义：登录写入 cookie 后，前端后续 API 请求都带
	// 同一个 Bearer token，必须覆盖整个会话，否则 5 分钟去重窗口过期后
	// 每次请求都会扣 1 次，MaxUses=1 的链接登录后稍作操作就被 401 踢出。
	const dedupWindow = 24 * time.Hour
	if t.LastUsedAt != nil && now.Sub(*t.LastUsedAt) < dedupWindow {
		model.DB.Model(&t).Updates(map[string]interface{}{
			"last_ip":      ip,
			"last_used_at": now,
		})
		return t.ID, true, ""
	}
	model.DB.Model(&t).Updates(map[string]interface{}{
		"used_count":   t.UsedCount + 1,
		"last_ip":      ip,
		"last_used_at": now,
	})

	// 记录使用日志
	useLog := model.TempAccessUseLog{
		TempAccessID: t.ID,
		IP:           ip,
		Region:       region,
		UserAgent:    userAgent,
		CreatedAt:    now,
	}
	model.DB.Create(&useLog)

	// 记录操作日志（来源 temp）
	RecordOp(0, "temp.login", fmt.Sprintf("临时访问「%s」使用（%s）", t.Name, regionOr(region, ip)), ip, "success")

	return t.ID, true, ""
}

// regionOr 若 region 为空则返回 fallback
func regionOr(region, fallback string) string {
	if region == "" {
		return fallback
	}
	return region
}

// ListTempAccessUseLogs 列出临时访问使用日志（按时间倒序，可选按 temp_access_id 过滤）
func ListTempAccessUseLogs(tempAccessID uint, limit int) []model.TempAccessUseLog {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := model.DB.Order("id DESC").Limit(limit)
	if tempAccessID > 0 {
		q = q.Where("temp_access_id = ?", tempAccessID)
	}
	var logs []model.TempAccessUseLog
	q.Find(&logs)
	return logs
}
