package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupSiteSecurityRoutes 单站安全路由（/api/site/:id/security/*）
func setupSiteSecurityRoutes(g *gin.RouterGroup) {
	// 读取配置
	g.GET("/site/security/config", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.GetSiteSecurityConfig(siteID))
	})

	// 保存配置
	g.POST("/site/security/config", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var cfg model.SiteSecurityConfig
		if err := c.ShouldBindJSON(&cfg); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveSiteSecurityConfig(siteID, cfg); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.security.config", "更新单站安全配置："+siteName(siteID), "success")
		utils.Ok(c, nil)
	})

	// IP 规则
	g.GET("/site/security/ip", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.ListSiteSecIpRules(siteID))
	})
	g.POST("/site/security/ip/add", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			Action        string `json:"action" binding:"required,oneof=allow block"`
			MatchType     string `json:"match_type" binding:"required,oneof=ip cidr range country isp"`
			Content       string `json:"content" binding:"required"`
			ExpireSeconds int    `json:"expire_seconds"`
			Remark        string `json:"remark"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		id, err := service.AddSiteSecIpRule(siteID, req.Action, req.MatchType, req.Content, req.ExpireSeconds, req.Remark)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.security.ip_add", "新增单站 IP 规则 "+req.Content+"，网站："+siteName(siteID), "success")
		utils.Ok(c, gin.H{"id": id})
	})
	g.POST("/site/security/ip/delete", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteSiteSecIpRule(siteID, req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// UA 规则
	g.GET("/site/security/ua", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.ListSiteSecUaRules(siteID))
	})
	g.POST("/site/security/ua/add", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			Action  string `json:"action" binding:"required,oneof=allow block"`
			Content string `json:"content" binding:"required"`
			Remark  string `json:"remark"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		id, err := service.AddSiteSecUaRule(siteID, req.Action, req.Content, req.Remark)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})
	g.POST("/site/security/ua/delete", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteSiteSecUaRule(siteID, req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// Referer 规则
	g.GET("/site/security/referer", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.ListSiteSecRefererRules(siteID))
	})
	g.POST("/site/security/referer/add", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			Action  string `json:"action" binding:"required,oneof=allow block"`
			Content string `json:"content" binding:"required"`
			Remark  string `json:"remark"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		id, err := service.AddSiteSecRefererRule(siteID, req.Action, req.Content, req.Remark)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})
	g.POST("/site/security/referer/delete", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteSiteSecRefererRule(siteID, req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 自定义规则
	g.GET("/site/security/rule", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.ListSiteSecCustomRules(siteID))
	})
	g.POST("/site/security/rule/add", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req model.SiteSecCustomRule
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		id, err := service.AddSiteSecCustomRule(siteID, req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})
	g.POST("/site/security/rule/update", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			ID      uint   `json:"id" binding:"required"`
			Enabled *bool  `json:"enabled"`
			Action  string `json:"action"`
			Pattern string `json:"pattern"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateSiteSecCustomRule(siteID, req.ID, req.Enabled, req.Action, req.Pattern); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
	g.POST("/site/security/rule/delete", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteSiteSecCustomRule(siteID, req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 攻击日志
	g.GET("/site/security/logs", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		keyword := c.Query("keyword")
		logs, total, err := service.ListSiteSecLogs(siteID, page, pageSize, keyword)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"logs": logs, "total": total})
	})
	g.POST("/site/security/logs/clear", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		if err := service.ClearSiteSecLogs(siteID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 统计
	g.GET("/site/security/stats", func(c *gin.Context) {
		siteID := parseSiteID(c)
		if siteID == 0 {
			return
		}
		utils.Ok(c, service.GetSiteSecStats(siteID))
	})
}

// parseSiteID 从 query 参数解析 site_id（GET/POST 统一走 query 传递）
func parseSiteID(c *gin.Context) uint {
	id, err := strconv.ParseUint(c.Query("site_id"), 10, 32)
	if err != nil || id == 0 {
		utils.Fail(c, 400, "参数错误：缺少 site_id")
		return 0
	}
	return uint(id)
}
