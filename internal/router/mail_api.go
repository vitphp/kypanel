package router

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupMailApiRoutes 注册「对外邮件 REST API」（第三方系统用 API 令牌调用，无需面板登录）。
//
// 令牌通过请求头传递，二选一：
//   - X-API-Key: mk_xxxxx
//   - Authorization: Bearer mk_xxxxx
//
// 令牌绑定单一邮箱域名，只能操作本域账号，实现多租户隔离。
func setupMailApiRoutes(r *gin.Engine) {
	g := r.Group("/api/mail-api/v1")

	// 令牌自检：确认令牌有效并返回绑定域名
	g.GET("/me", func(c *gin.Context) {
		key, ok := mailApiAuth(c)
		if !ok {
			return
		}
		utils.Ok(c, gin.H{
			"domain":     key.Domain,
			"name":       key.Name,
			"key_prefix": key.KeyPrefix,
		})
	})

	// 发信
	g.POST("/send", func(c *gin.Context) {
		key, ok := mailApiAuth(c)
		if !ok {
			return
		}
		var req struct {
			From        string          `json:"from"`
			To          jsonStrings     `json:"to"`
			Cc          jsonStrings     `json:"cc"`
			Subject     string          `json:"subject"`
			Text        string          `json:"text"`
			Html        string          `json:"html"`
			Attachments []apiAttachment `json:"attachments"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
			return
		}
		box, err := service.MailApiResolveMailbox(key, req.From)
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		to := req.To.slice()
		if len(to) == 0 {
			utils.Fail(c, http.StatusBadRequest, "请填写收件人")
			return
		}
		// 附件解码（总大小 25MB 上限）
		var attach []service.MailAttachment
		var total int
		for _, a := range req.Attachments {
			data, err := base64.StdEncoding.DecodeString(a.Data)
			if err != nil {
				utils.Fail(c, http.StatusBadRequest, "附件数据无效: "+a.Filename)
				return
			}
			total += len(data)
			if total > 25*1024*1024 {
				utils.Fail(c, http.StatusBadRequest, "附件总大小超过 25MB")
				return
			}
			attach = append(attach, service.MailAttachment{
				Filename: a.Filename, ContentType: a.Type, Data: data, Inline: a.Inline, ContentID: a.CID,
			})
		}
		result, err := service.SendMail(service.SendMailRequest{
			MailboxID: box.ID,
			To:        to,
			Cc:        req.Cc.slice(),
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

	// 收件箱列表（供第三方轮询）
	g.GET("/messages", func(c *gin.Context) {
		key, ok := mailApiAuth(c)
		if !ok {
			return
		}
		box, err := service.MailApiResolveMailbox(key, c.Query("address"))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		list, err := service.ListMailboxMessagesAPI(box.ID, c.Query("folder"))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		utils.Ok(c, gin.H{"address": box.Address(), "messages": list})
	})

	// 读信详情（会标记已读）
	g.GET("/messages/:id", func(c *gin.Context) {
		key, ok := mailApiAuth(c)
		if !ok {
			return
		}
		box, err := service.MailApiResolveMailbox(key, c.Query("address"))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
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
		key, ok := mailApiAuth(c)
		if !ok {
			return
		}
		box, err := service.MailApiResolveMailbox(key, c.Query("address"))
		if err != nil {
			utils.Fail(c, http.StatusBadRequest, err.Error())
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
}

// mailApiAuth 解析并校验 API 令牌（X-API-Key 或 Authorization: Bearer）。
func mailApiAuth(c *gin.Context) (*model.MailApiKey, bool) {
	token := strings.TrimSpace(c.GetHeader("X-API-Key"))
	if token == "" {
		token = strings.TrimSpace(c.GetHeader("Authorization"))
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	}
	if token == "" {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, "缺少 API 令牌")
		return nil, false
	}
	key, err := service.AuthenticateMailApiKey(token)
	if err != nil {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, err.Error())
		return nil, false
	}
	return key, true
}

// jsonStrings 兼容 JSON 中的字符串（逗号分隔）或字符串数组两种写法。
type jsonStrings []string

func (j *jsonStrings) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*j = arr
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*j = splitComma(str)
	return nil
}

func (j jsonStrings) slice() []string {
	out := make([]string, 0, len(j))
	for _, s := range j {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// apiAttachment 对外 API 的附件结构
type apiAttachment struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Data     string `json:"data"` // base64
	Inline   bool   `json:"inline"`
	CID      string `json:"cid"`
}
