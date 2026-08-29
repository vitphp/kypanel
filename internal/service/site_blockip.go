package service

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"kypanel/internal/model"
)

// SiteBlockIPReq 站点级 IP 拉黑请求
type SiteBlockIPReq struct {
	ID            uint   `json:"id"`
	SiteID        uint   `json:"site_id" binding:"required"`
	IP            string `json:"ip" binding:"required"`
	ExpireSeconds int    `json:"expire_seconds"` // 0 表示永久
	Remark        string `json:"remark"`
}

// ListSiteBlockIPs 列出某站点的 IP 黑名单（含已过期条目，按创建时间倒序）
func ListSiteBlockIPs(siteID uint) ([]model.SiteBlockIP, error) {
	var ips []model.SiteBlockIP
	if err := model.DB.Where("site_id = ?", siteID).
		Order("created_at DESC").
		Find(&ips).Error; err != nil {
		return nil, err
	}
	return ips, nil
}

// AddSiteBlockIP 拉黑一个 IP（站点级）。返回新记录的 ID。
func AddSiteBlockIP(req SiteBlockIPReq) (uint, error) {
	ip := strings.TrimSpace(req.IP)
	if ip == "" {
		return 0, errors.New("IP 不能为空")
	}
	if _, _, err := net.ParseCIDR(ip); err != nil {
		if net.ParseIP(ip) == nil {
			return 0, errors.New("IP 格式不合法")
		}
	}
	// 去重：同 IP 已存在则更新 expire_at 和 remark
	var existing model.SiteBlockIP
	if err := model.DB.Where("site_id = ? AND ip = ?", req.SiteID, ip).First(&existing).Error; err == nil {
		var expireAt *time.Time
		if req.ExpireSeconds > 0 {
			t := time.Now().Add(time.Duration(req.ExpireSeconds) * time.Second)
			expireAt = &t
		}
		existing.ExpireAt = expireAt
		if strings.TrimSpace(req.Remark) != "" {
			existing.Remark = req.Remark
		}
		if err := model.DB.Save(&existing).Error; err != nil {
			return 0, err
		}
		if err := rebuildSiteConfig(req.SiteID); err != nil {
			return existing.ID, err
		}
		return existing.ID, nil
	}
	var expireAt *time.Time
	if req.ExpireSeconds > 0 {
		t := time.Now().Add(time.Duration(req.ExpireSeconds) * time.Second)
		expireAt = &t
	}
	row := model.SiteBlockIP{
		SiteID:   req.SiteID,
		IP:       ip,
		ExpireAt: expireAt,
		Remark:   req.Remark,
	}
	if err := model.DB.Create(&row).Error; err != nil {
		return 0, err
	}
	if err := rebuildSiteConfig(req.SiteID); err != nil {
		return row.ID, err
	}
	return row.ID, nil
}

// RemoveSiteBlockIP 解禁一条站点级 IP 黑名单
func RemoveSiteBlockIP(siteID uint, id uint) error {
	res := model.DB.Where("site_id = ? AND id = ?", siteID, id).Delete(&model.SiteBlockIP{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("记录不存在")
	}
	return rebuildSiteConfig(siteID)
}

// PurgeExpiredSiteBlockIPs 清理过期的站点级 IP 拉黑（每小时跑一次）
func PurgeExpiredSiteBlockIPs() int64 {
	res := model.DB.Where("expire_at IS NOT NULL AND expire_at < ?", time.Now()).
		Delete(&model.SiteBlockIP{})
	return res.RowsAffected
}

// effectiveSiteBlockIPs 返回某站点当前有效的（未过期）IP 列表，按创建时间正序
func effectiveSiteBlockIPs(siteID uint) []string {
	var rows []model.SiteBlockIP
	now := time.Now()
	model.DB.Where("site_id = ? AND (expire_at IS NULL OR expire_at > ?)", siteID, now).
		Order("created_at ASC").
		Find(&rows)
	ips := make([]string, 0, len(rows))
	for _, r := range rows {
		ips = append(ips, r.IP)
	}
	return ips
}

// rebuildSiteConfig 触发该站点的 nginx 配置重写
func rebuildSiteConfig(siteID uint) error {
	var s model.Site
	if err := model.DB.First(&s, siteID).Error; err != nil {
		return errors.New("站点不存在")
	}
	s.ConfigOverride = "" // 走自动生成模式，siteBlockIPDirectives 自然注入
	return writeSiteConf(&s)
}

// siteBlockIPDirectives 生成该站点有效 IP 黑名单的 nginx 指令片段
// 形如：
//
//	# site block ips (3)
//	deny 1.2.3.4;
//	deny 5.6.7.8;
//	deny 9.10.11.0/24;
//
// 放在 server 块头部。allow all 在 server 块尾由原有逻辑保证（fallback）。
func siteBlockIPDirectives(siteID uint) string {
	ips := effectiveSiteBlockIPs(siteID)
	if len(ips) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "    # site block ips (%d)\n", len(ips))
	for _, ip := range ips {
		fmt.Fprintf(&sb, "    deny %s;\n", ip)
	}
	return sb.String()
}

// StartSiteBlockIPJanitor 启动每小时清理过期 IP 的协程
func StartSiteBlockIPJanitor() {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			if n := PurgeExpiredSiteBlockIPs(); n > 0 {
				// 过期清理后需要重写所有受影响的站点配置
				var siteIDs []uint
				model.DB.Model(&model.Site{}).Pluck("id", &siteIDs)
				for _, id := range siteIDs {
					_ = rebuildSiteConfig(id)
				}
			}
		}
	}()
}
