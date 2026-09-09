package router

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupMailRoutes 注册邮箱系统管理接口。邮箱管理属于敏感操作（涉及域名/DNS/后续私钥），
// 仅允许超级管理员操作（与 API 令牌管理同级），避免子账号越权管理邮箱域名。
func setupMailRoutes(g *gin.RouterGroup) {
	mail := g.Group("/mail")
	{
		// 邮箱域名列表
		mail.GET("/domains", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			utils.Ok(c, service.ListMailDomains())
		})

		// 添加域名
		mail.POST("/domains", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				Domain string `json:"domain" binding:"required"`
				Remark string `json:"remark"`
				Quota  int64  `json:"quota"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			rec, err := service.CreateMailDomain(req.Domain, req.Remark, req.Quota)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.domain.add", "添加邮箱域名: "+rec.Domain, "success")
			utils.Ok(c, rec)
		})

		// 更新域名（启停 / 备注 / 配额 / DNS 勾选状态）
		mail.PATCH("/domains/:id", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			var req struct {
				Enabled         *bool   `json:"enabled"`
				Remark          *string `json:"remark"`
				QuotaPerBox     *int64  `json:"quota_per_box"`
				MxConfigured    *bool   `json:"mx_configured"`
				SpfConfigured   *bool   `json:"spf_configured"`
				DkimConfigured  *bool   `json:"dkim_configured"`
				DmarcConfigured *bool   `json:"dmarc_configured"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			patch := map[string]interface{}{}
			if req.Enabled != nil {
				patch["enabled"] = *req.Enabled
			}
			if req.Remark != nil {
				patch["remark"] = *req.Remark
			}
			if req.QuotaPerBox != nil && *req.QuotaPerBox > 0 {
				patch["quota_per_box"] = *req.QuotaPerBox
			}
			if req.MxConfigured != nil {
				patch["mx_configured"] = *req.MxConfigured
			}
			if req.SpfConfigured != nil {
				patch["spf_configured"] = *req.SpfConfigured
			}
			if req.DkimConfigured != nil {
				patch["dkim_configured"] = *req.DkimConfigured
			}
			if req.DmarcConfigured != nil {
				patch["dmarc_configured"] = *req.DmarcConfigured
			}
			if err := service.UpdateMailDomain(uint(id), patch); err != nil {
				utils.Fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			recordOpForCtx(c, "mail.domain.update", "更新邮箱域名 #"+strconv.Itoa(id), "success")
			utils.Ok(c, nil)
		})

		// 删除域名
		mail.DELETE("/domains/:id", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if err := service.DeleteMailDomain(uint(id)); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.domain.delete", "删除邮箱域名 #"+strconv.Itoa(id), "success")
			utils.Ok(c, nil)
		})

		// DNS 绑定引导（告诉用户怎么解析 MX/SPF/DKIM/DMARC）
		mail.GET("/domains/:id/dns-guide", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			guide, err := service.MailDomainDnsGuideByID(uint(id))
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, guide)
		})

		// DNS 自检：查该域名 MX 是否已生效
		mail.GET("/domains/:id/dns-check", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			domain, err := service.MailDomainNameByID(uint(id))
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			mx, err := service.CheckMailDnsRecord(domain)
			if err != nil {
				// 返回 200 但带 ready=false，由前端展示"暂未生效"而非弹错误
				utils.Ok(c, gin.H{"ready": false, "message": err.Error()})
				return
			}
			utils.Ok(c, gin.H{"mx": mx, "ready": true})
		})
	}
}
