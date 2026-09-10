package router

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

		// 域名对接状态（MX 是否指向本机）：列表页批量判定
		mail.POST("/domains/check-ready", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				DomainIDs []uint `json:"domain_ids"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if len(req.DomainIDs) == 0 {
				// 未传则默认检测全部
				for _, d := range service.ListMailDomains() {
					req.DomainIDs = append(req.DomainIDs, d.ID)
				}
			}
			result := map[uint]service.MailDomainStatus{}
			for _, id := range req.DomainIDs {
				result[id] = service.CheckMailDomainReady(id)
			}
			utils.Ok(c, result)
		})
	}

	// 添加域名向导：生成配置值（第2步）
	mail.GET("/domain-guide", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		d := c.Query("domain")
		if d == "" {
			utils.Fail(c, http.StatusBadRequest, "缺少域名参数")
			return
		}
		utils.Ok(c, service.BuildNewDomainGuide(d))
	})

	// 添加域名向导：检测该域名 MX 是否已指向本机（第3步通过判定）
	mail.GET("/domain-check", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		d := c.Query("domain")
		if d == "" {
			utils.Fail(c, http.StatusBadRequest, "缺少域名参数")
			return
		}
		ready, detail := service.CheckDomainMxReady(d)
		utils.Ok(c, gin.H{"ready": ready, "detail": detail})
	})

	// 邮箱账号
	accounts := mail.Group("/accounts")
	{
		// 某域名下账号列表
		accounts.GET("", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			did, _ := strconv.Atoi(c.Query("domain_id"))
			if did <= 0 {
				utils.Fail(c, http.StatusBadRequest, "缺少域名参数")
				return
			}
			utils.Ok(c, service.ListMailboxes(uint(did)))
		})

		// 单个添加账号
		accounts.POST("", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				DomainID uint   `json:"domain_id" binding:"required"`
				Name     string `json:"name" binding:"required"`
				Password string `json:"password" binding:"required"`
				Remark   string `json:"remark"`
				Quota    int64  `json:"quota"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			box, err := service.CreateMailbox(req.DomainID, req.Name, req.Password, req.Remark, req.Quota)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.account.add", "添加邮箱账号: "+box.Address(), "success")
			utils.Ok(c, box)
		})

		// 批量添加（多行文本：邮箱名 密码 / 邮箱名:密码）
		accounts.POST("/batch", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				DomainID uint   `json:"domain_id" binding:"required"`
				Lines    string `json:"lines"`
				Quota    int64  `json:"quota"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			created, failed, err := service.CreateMailboxesBatch(req.DomainID, req.Lines, req.Quota)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.account.batch", "批量添加邮箱账号", "success")
			utils.Ok(c, gin.H{"created": created, "failed": failed})
		})

		// 随机生成账号
		accounts.POST("/random", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				DomainID uint   `json:"domain_id" binding:"required"`
				Prefix   string `json:"prefix"`
				Length   int    `json:"length"`
				Digit    int    `json:"digit"`
				Count    int    `json:"count"`
				Password string `json:"password"`
				Quota    int64  `json:"quota"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			rule := service.RandomMailboxRule{
				Prefix: req.Prefix, Length: req.Length, Digit: req.Digit,
				Count: req.Count, Password: req.Password, Quota: req.Quota,
			}
			created, accounts, failed, err := service.GenerateRandomMailboxes(req.DomainID, rule)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.account.random", "随机生成邮箱账号", "success")
			utils.Ok(c, gin.H{"created": created, "accounts": accounts, "failed": failed})
		})

		// 删除账号
		accounts.DELETE("/:id", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if err := service.DeleteMailbox(uint(id)); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.account.delete", "删除邮箱账号 #"+strconv.Itoa(id), "success")
			utils.Ok(c, nil)
		})

		// 启停账号
		accounts.PATCH("/:id/enabled", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			var req struct {
				Enabled bool `json:"enabled"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if err := service.ToggleMailbox(uint(id), req.Enabled); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, nil)
		})

		// 重置密码
		accounts.PATCH("/:id/password", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil || id <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			var req struct {
				Password string `json:"password" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if err := service.UpdateMailboxPassword(uint(id), req.Password); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, nil)
		})
	}

	// 收件箱（消息）
	messages := mail.Group("/messages")
	{
		// 某账号收件箱消息列表（folder 默认 inbox）
		messages.GET("", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			mbID, _ := strconv.Atoi(c.Query("mailbox_id"))
			if mbID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "缺少账号参数")
				return
			}
			folder := c.Query("folder")
			list, err := service.ListMailboxMessagesAPI(uint(mbID), folder)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, list)
		})

		// 某账号收件箱未读数
		messages.GET("/unread-count", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			mbID, _ := strconv.Atoi(c.Query("mailbox_id"))
			if mbID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "缺少账号参数")
				return
			}
			utils.Ok(c, gin.H{"count": service.CountMailboxUnseenAPI(uint(mbID))})
		})

		// 一键已读：把某账号收件箱全部未读标为已读
		messages.POST("/read-all", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				MailboxID uint `json:"mailbox_id"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if req.MailboxID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "缺少账号参数")
				return
			}
			if err := service.MarkMailboxAllSeenAPI(req.MailboxID); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.message.readall", "一键标记全部已读", "success")
			utils.Ok(c, nil)
		})

		// 读某封信详情（并标记已读）
		messages.GET("/:id", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			msgID, _ := strconv.Atoi(c.Param("id"))
			mbID, _ := strconv.Atoi(c.Query("mailbox_id"))
			if msgID <= 0 || mbID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			detail, err := service.ReadMailboxMessageAPI(uint(mbID), uint(msgID))
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, detail)
		})

		// 下载附件
		messages.GET("/:id/attachment", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			msgID, _ := strconv.Atoi(c.Param("id"))
			mbID, _ := strconv.Atoi(c.Query("mailbox_id"))
			idx, _ := strconv.Atoi(c.Query("index"))
			if msgID <= 0 || mbID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			fname, ctype, data, err := service.GetMailAttachmentAPI(uint(mbID), uint(msgID), idx)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			if ctype == "" {
				ctype = "application/octet-stream"
			}
			c.Header("Content-Type", ctype)
			c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fname))
			c.Data(http.StatusOK, ctype, data)
		})

		// 删除消息
		messages.DELETE("/:id", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			msgID, _ := strconv.Atoi(c.Param("id"))
			mbID, _ := strconv.Atoi(c.Query("mailbox_id"))
			if msgID <= 0 || mbID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if err := service.DeleteMailboxMessageAPI(uint(mbID), uint(msgID)); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			recordOpForCtx(c, "mail.message.delete", "删除邮件 #"+strconv.Itoa(msgID), "success")
			utils.Ok(c, nil)
		})

		// 批量标记已读/未读
		messages.POST("/seen", func(c *gin.Context) {
			if !requireSuperAdmin(c) {
				return
			}
			var req struct {
				MailboxID uint   `json:"mailbox_id"`
				IDs       []uint `json:"ids"`
				Seen      bool   `json:"seen"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				utils.Fail(c, http.StatusBadRequest, "参数错误")
				return
			}
			if req.MailboxID <= 0 {
				utils.Fail(c, http.StatusBadRequest, "缺少账号参数")
				return
			}
			if err := service.SetMailboxMessagesSeenAPI(req.MailboxID, req.IDs, req.Seen); err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			utils.Ok(c, nil)
		})
	}

	// 全部邮箱账号未读总数（左侧菜单红点）
	mail.GET("/unread-count", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		utils.Ok(c, gin.H{"count": service.CountSystemUnseenAPI()})
	})

	// 发信
	mail.POST("/send", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		var req struct {
			MailboxID   uint     `json:"mailbox_id"`
			To          []string `json:"to"`
			Cc          []string `json:"cc"`
			Subject     string   `json:"subject"`
			Text        string   `json:"text"`
			Html        string   `json:"html"`
			Attachments []struct {
				Filename string `json:"filename"`
				Type     string `json:"type"`
				Data     string `json:"data"` // base64
				Inline   bool   `json:"inline"`
				CID      string `json:"cid"`
			} `json:"attachments"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
			return
		}
		if req.MailboxID == 0 {
			utils.Fail(c, http.StatusBadRequest, "缺少发件账号")
			return
		}
		attach, ok := parseAttachments(c, req.Attachments)
		if !ok {
			return
		}
		result, err := service.SendMail(service.SendMailRequest{
			MailboxID: req.MailboxID,
			To:        req.To,
			Cc:        req.Cc,
			Subject:   req.Subject,
			TextBody:  req.Text,
			HtmlBody:  req.Html,
			Attach:    attach,
		})
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		recordOpForCtx(c, "mail.send", "发送邮件: "+strings.Join(req.To, ","), "success")
		utils.Ok(c, result)
	})

	// 保存草稿
	mail.POST("/drafts", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		var req struct {
			MailboxID   uint     `json:"mailbox_id"`
			DraftID     uint     `json:"draft_id"`
			To          []string `json:"to"`
			Subject     string   `json:"subject"`
			Text        string   `json:"text"`
			Html        string   `json:"html"`
			Attachments []struct {
				Filename string `json:"filename"`
				Type     string `json:"type"`
				Data     string `json:"data"`
				Inline   bool   `json:"inline"`
				CID      string `json:"cid"`
			} `json:"attachments"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
			return
		}
		if req.MailboxID == 0 {
			utils.Fail(c, http.StatusBadRequest, "缺少账号")
			return
		}
		attach, ok := parseAttachments(c, req.Attachments)
		if !ok {
			return
		}
		rec, err := service.SaveDraft(service.SaveDraftRequest{
			MailboxID: req.MailboxID, DraftID: req.DraftID, To: req.To,
			Subject: req.Subject, TextBody: req.Text, HtmlBody: req.Html, Attach: attach,
		})
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": rec.ID})
	})
}

// parseAttachments 解析请求里的附件（base64 → bytes），总大小限 25MB。
func parseAttachments(c *gin.Context, in []struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Data     string `json:"data"`
	Inline   bool   `json:"inline"`
	CID      string `json:"cid"`
}) ([]service.MailAttachment, bool) {
	var out []service.MailAttachment
	var total int
	for _, a := range in {
		raw, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, "附件数据无效: "+a.Filename)
			return nil, false
		}
		total += len(raw)
		if total > 25*1024*1024 {
			utils.Fail(c, http.StatusBadRequest, "附件总大小超过 25MB")
			return nil, false
		}
		out = append(out, service.MailAttachment{
			Filename: a.Filename, ContentType: a.Type, Data: raw, Inline: a.Inline, ContentID: a.CID,
		})
	}
	return out, true
}
