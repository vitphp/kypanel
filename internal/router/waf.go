package router

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupWAFRoutes WAF 应用防护路由
func setupWAFRoutes(g *gin.RouterGroup) {
	// 总览
	g.GET("/waf/overview", func(c *gin.Context) {
		utils.Ok(c, service.GetWAFOverview())
	})

	// 开关与模式
	g.POST("/waf/setting", func(c *gin.Context) {
		var req service.WAFSettingReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveWAFSetting(req); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		detail := "修改 WAF 防护设置"
		if req.Mode != "" {
			detail += " 模式：" + req.Mode
		}
		if req.Enabled != nil {
			if *req.Enabled {
				detail += " 开启"
			} else {
				detail += " 关闭"
			}
		}
		recordOpForCtx(c, "waf.setting", detail, "success")
		utils.Ok(c, nil)
	})

	// 规则库
	g.GET("/waf/rules", func(c *gin.Context) {
		utils.Ok(c, gin.H{"rules": service.ListWAFRules()})
	})

	// 更新规则
	g.POST("/waf/rule/update", func(c *gin.Context) {
		var req struct {
			ID     uint                     `json:"id" binding:"required"`
			Update service.UpdateWAFRuleReq `json:"update"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.UpdateWAFRule(req.ID, req.Update); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.rule.update", fmt.Sprintf("规则 #%d（动作：%s）", req.ID, req.Update.Action), "success")
		utils.Ok(c, nil)
	})

	// 新增自定义规则
	g.POST("/waf/rule/add", func(c *gin.Context) {
		var req service.CreateWAFRuleReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateWAFRule(req); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		ruleName := req.Name
		if ruleName == "" {
			ruleName = req.Pattern
		}
		recordOpForCtx(c, "waf.rule.add", "新增 WAF 规则："+ruleName, "success")
		utils.Ok(c, nil)
	})

	// 删除规则
	g.POST("/waf/rule/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteWAFRule(req.ID); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.rule.delete", fmt.Sprintf("删除 WAF 规则 #%d", req.ID), "success")
		utils.Ok(c, nil)
	})

	// 恢复内置规则
	g.POST("/waf/rules/reset", func(c *gin.Context) {
		if err := service.ResetWAFRules(); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.rules.reset", "恢复 WAF 内置规则", "success")
		utils.Ok(c, nil)
	})

	// 黑白名单
	g.GET("/waf/iprules", func(c *gin.Context) {
		utils.Ok(c, gin.H{"rules": service.ListWAFIpRules()})
	})

	g.POST("/waf/iprule/add", func(c *gin.Context) {
		var req service.WAFIpRuleReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateWAFIpRule(req); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.iprule.add", fmt.Sprintf("%s %s %s", req.Type, req.Action, req.Content), "success")
		utils.Ok(c, nil)
	})

	g.POST("/waf/iprule/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteWAFIpRule(req.ID); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.iprule.delete", fmt.Sprintf("删除 WAF 黑白名单 #%d", req.ID), "success")
		utils.Ok(c, nil)
	})

	// CC 防护配置
	g.GET("/waf/cc", func(c *gin.Context) {
		utils.Ok(c, service.GetWAFCcConfig())
	})

	g.POST("/waf/cc/save", func(c *gin.Context) {
		var req service.WAFCcConfigReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveWAFCcConfig(req); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		if err := service.ApplyWAFConfig(); err != nil {
			utils.Fail(c, 400, "配置应用失败: "+err.Error())
			return
		}
		recordOpForCtx(c, "waf.cc.save", fmt.Sprintf("保存 CC 防护配置（限速 %d/%d秒）", req.MaxRequests, req.WindowSec), "success")
		utils.Ok(c, nil)
	})

	// 攻击日志
	g.GET("/waf/logs", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		keyword := c.Query("keyword")
		logs, total, err := service.ListWAFAttackLogs(page, pageSize, keyword)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"logs": logs, "total": total})
	})

	g.POST("/waf/logs/clear", func(c *gin.Context) {
		if err := service.ClearWAFAttackLogs(); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "waf.logs.clear", "清空 WAF 攻击日志", "success")
		utils.Ok(c, nil)
	})

	// 统计
	g.GET("/waf/stats/top_ip", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		utils.Ok(c, gin.H{"rows": service.WAFStatsTopIp(limit)})
	})

	g.GET("/waf/stats/top_category", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		utils.Ok(c, gin.H{"rows": service.WAFStatsTopCategory(limit)})
	})
}
