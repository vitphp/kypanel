package router

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupMailPortalRoutes 注册「邮箱门户」公开接口（无需登录）。
//
// 这些接口由门户站点（mail.<domain>）的 nginx 片段把 /api/mail-portal/ 反代到面板，
// 供访客自助注册 / 登录 / 收发邮件。会话鉴权用独立的邮箱会话 token（Authorization: Bearer mb.xxx），
// 与面板管理员登录体系隔离。
func setupMailPortalRoutes(r *gin.Engine) {
	g := r.Group("/api/mail-portal")

	// 门户 Logo（公开，供官网/webmail 页面引用；按邮箱域名 ID 定位）
	g.GET("/logo/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		if id <= 0 {
			c.Status(http.StatusNotFound)
			return
		}
		ctype, data, err := service.MailPortalLogoFile(uint(id))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, ctype, data)
	})

	// 注册（受门户「开放注册」开关约束）
	g.POST("/register", func(c *gin.Context) {
		var req struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		box, err := service.MailPortalRegister(c.Request.Host, req.Name, req.Password)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, gin.H{"address": box.Address()})
	})

	// 登录 → 返回会话 token
	g.POST("/login", func(c *gin.Context) {
		var req struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		token, box, err := service.MailPortalLogin(c.Request.Host, req.Name, req.Password)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, gin.H{
			"token": token,
			"mailbox": gin.H{
				"id":      box.ID,
				"name":    box.Name,
				"address": box.Address(),
			},
		})
	})

	// 退出（无状态 token，前端清除即可，这里仅作占位）
	g.POST("/logout", func(c *gin.Context) { utils.Ok(c, nil) })

	// 当前登录账号
	g.GET("/me", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		utils.Ok(c, gin.H{"id": box.ID, "name": box.Name, "address": box.Address()})
	})

	// 邮件列表
	g.GET("/messages", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		list, err := service.ListMailboxMessagesAPI(box.ID, c.Query("folder"))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// 邮件详情（并标记已读）
	g.GET("/messages/:id", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		msgID, _ := strconv.Atoi(c.Param("id"))
		if msgID <= 0 {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		detail, err := service.ReadMailboxMessageAPI(box.ID, uint(msgID))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, detail)
	})

	// 附件下载
	g.GET("/messages/:id/attachments/:idx", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		msgID, _ := strconv.Atoi(c.Param("id"))
		idx, _ := strconv.Atoi(c.Param("idx"))
		if msgID <= 0 {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		fname, ctype, data, err := service.GetMailAttachmentAPI(box.ID, uint(msgID), idx)
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

	// 删除邮件
	g.DELETE("/messages/:id", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		msgID, _ := strconv.Atoi(c.Param("id"))
		if msgID <= 0 {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		if err := service.DeleteMailboxMessageAPI(box.ID, uint(msgID)); err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 发信
	g.POST("/send", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		var req struct {
			To          interface{} `json:"to"`
			Subject     string      `json:"subject"`
			Text        string      `json:"text"`
			Html        string      `json:"html"`
			Attachments []string    `json:"attachments"` // 文件库中的文件名列表
			// 正文内嵌图片（富文本编辑器插入的图片，以 cid 引用）
			InlineImages []struct {
				Filename string `json:"filename"`
				Type     string `json:"type"`
				CID      string `json:"cid"`
				Data     string `json:"data"` // base64（不含 data: 前缀）
			} `json:"inline_images"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		to := normalizeMailRecipients(req.To)
		if len(to) == 0 {
			utils.Fail(c, http.StatusBadRequest, "请填写收件人")
			return
		}
		// 从「我的文件」加载附件（可选）
		var attach []service.MailAttachment
		if len(req.Attachments) > 0 {
			loaded, err := service.LoadMailPortalAttachments(box, req.Attachments)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, err.Error())
				return
			}
			attach = loaded
		}
		// 正文内嵌图片（可选）
		var inlineSize int
		for _, im := range req.InlineImages {
			if im.CID == "" || im.Data == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(im.Data)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, "正文内嵌图片格式不正确")
				return
			}
			inlineSize += len(data)
			if inlineSize > service.MailPortalMaxFileSize {
				utils.Fail(c, http.StatusBadRequest, "正文内嵌图片总大小不能超过 25MB")
				return
			}
			ctype := im.Type
			if ctype == "" {
				ctype = "image/png"
			}
			name := im.Filename
			if name == "" {
				name = im.CID + ".png"
			}
			attach = append(attach, service.MailAttachment{
				Filename:    name,
				ContentType: ctype,
				Data:        data,
				Inline:      true,
				ContentID:   im.CID,
			})
		}
		result, err := service.SendMail(service.SendMailRequest{
			MailboxID: box.ID,
			To:        to,
			Subject:   req.Subject,
			TextBody:  req.Text,
			HtmlBody:  req.Html,
			Attach:    attach,
		})
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, result)
	})

	// ===== 我的文件（附件库）=====

	// 列出账号文件目录（含目录路径，展示给用户）
	g.GET("/files", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		view, err := service.ListMailPortalFiles(box)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, view)
	})

	// 上传文件（multipart，字段名 file）
	g.POST("/files", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		fileHeader, err := c.FormFile("file")
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, "缺少上传文件")
			return
		}
		if fileHeader.Size > int64(service.MailPortalMaxFileSize) {
			utils.Fail(c, http.StatusBadRequest, "单个文件不能超过 25MB")
			return
		}
		f, err := fileHeader.Open()
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, "读取上传文件失败")
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, int64(service.MailPortalMaxFileSize)+1))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, "读取上传文件失败")
			return
		}
		file, err := service.SaveMailPortalFile(box, fileHeader.Filename, data)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, file)
	})

	// 下载文件
	g.GET("/files/:name", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		fname, ctype, data, err := service.GetMailPortalFile(box, c.Param("name"))
		if err != nil {
			utils.Fail(c, http.StatusNotFound, err.Error())
			return
		}
		c.Header("Content-Type", ctype)
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fname))
		c.Data(http.StatusOK, ctype, data)
	})

	// 删除文件
	g.DELETE("/files/:name", func(c *gin.Context) {
		box, ok := mailPortalAuth(c)
		if !ok {
			return
		}
		if err := service.DeleteMailPortalFile(box, c.Param("name")); err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
}

// mailPortalAuth 从 Authorization: Bearer <token> 解析门户登录账号。
func mailPortalAuth(c *gin.Context) (*model.Mailbox, bool) {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未登录")
		return nil, false
	}
	box, err := service.MailboxByPortalToken(token)
	if err != nil {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, err.Error())
		return nil, false
	}
	return box, true
}

// normalizeMailRecipients 兼容收件人为字符串（逗号分隔）或数组两种入参。
func normalizeMailRecipients(v interface{}) []string {
	var out []string
	switch t := v.(type) {
	case string:
		for _, s := range strings.Split(t, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case []interface{}:
		for _, item := range t {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
