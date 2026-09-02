package router

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupSecurityRoutes 安全中心路由（统一规则模型：port / ip / country / isp）
func setupSecurityRoutes(g *gin.RouterGroup) {
	// 防火墙与离线库状态
	g.GET("/security/status", func(c *gin.Context) {
		utils.Ok(c, service.GetFirewallStatus())
	})

	// 规则列表，type=port|ip|geo|all（geo=country+isp）
	g.GET("/security/rules", func(c *gin.Context) {
		typ := c.Query("type")
		if typ == "" {
			typ = "all"
		}
		utils.Ok(c, gin.H{"rules": service.ListSecurityRulesByType(typ)})
	})

	// 新增规则（端口/IP/国家/运营商统一入口）
	g.POST("/security/rule/add", func(c *gin.Context) {
		var req service.AddSecurityRuleReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		rule, err := service.AddSecurityRule(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		detail := "添加 "
		switch rule.Type {
		case "port":
			detail += fmt.Sprintf("%s 端口 %s/%s", rule.Action, rule.Port, rule.Proto)
		case "ip":
			detail += fmt.Sprintf("%s IP %s", rule.Action, strings.Join(rule.Content, ","))
		default:
			detail += fmt.Sprintf("%s %s %s", rule.Action, rule.Type, strings.Join(rule.Content, ","))
		}
		recordOpForCtx(c, "security.rule.add", detail, "success")
		utils.Ok(c, rule)
	})

	// 修改规则（端口/IP/国家/运营商统一入口；系统默认规则不可编辑）
	g.POST("/security/rule/update", func(c *gin.Context) {
		var req service.AddSecurityRuleReq
		// 用 query 拿 id，避免在请求体里嵌入同名字段导致 binding 校验问题
		id := c.Query("id")
		if id == "" {
			utils.Fail(c, 400, "id 不能为空（请用 ?id=xxx 传 id）")
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		rule, err := service.UpdateSecurityRule(id, req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		detail := "修改 "
		switch rule.Type {
		case "port":
			detail += fmt.Sprintf("%s 端口 %s/%s", rule.Action, rule.Port, rule.Proto)
		case "ip":
			detail += fmt.Sprintf("%s IP %s", rule.Action, strings.Join(rule.Content, ","))
		default:
			detail += fmt.Sprintf("%s %s %s", rule.Action, rule.Type, strings.Join(rule.Content, ","))
		}
		recordOpForCtx(c, "security.rule.update", detail, "success")
		utils.Ok(c, rule)
	})

	// 删除规则
	g.POST("/security/rule/delete", func(c *gin.Context) {
		var req struct {
			ID string `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteSecurityRule(req.ID); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "security.rule.delete", "删除规则 #"+req.ID, "success")
		utils.Ok(c, nil)
	})

	// 修改规则备注
	g.POST("/security/rule/remark", func(c *gin.Context) {
		var req struct {
			ID     string `json:"id" binding:"required"`
			Remark string `json:"remark"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateSecurityRuleRemark(req.ID, req.Remark); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "security.rule.remark", "修改规则 #"+req.ID+" 备注为 "+req.Remark, "success")
		utils.Ok(c, nil)
	})

	// 手动应用规则到底层防火墙
	g.POST("/security/rules/apply", func(c *gin.Context) {
		if err := service.ApplySecurityRulesWithTimeout(30 * time.Second); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// IP 归属查询
	g.GET("/security/ip/lookup", func(c *gin.Context) {
		ip := c.Query("ip")
		if ip == "" {
			utils.Fail(c, 400, "请填写要查询的 IP")
			return
		}
		region, ok := service.SearchIp(ip)
		if !ok || region == nil {
			utils.Fail(c, 400, "查询失败，请确认离线库已加载")
			return
		}
		utils.Ok(c, gin.H{
			"ip":      ip,
			"country": region.Country,
			"province": region.Province,
			"city":    region.City,
			"isp":     region.ISP,
		})
	})

	// 重新加载离线 IP 库
	g.POST("/security/ip/reload", func(c *gin.Context) {
		service.InitIpRegion()
		ok, size, errMsg := service.IpRegionStatus()
		utils.Ok(c, gin.H{"ok": ok, "size": size, "err": errMsg})
	})
}
