package router

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupSiteRoutes 网站管理路由
func setupSiteRoutes(g *gin.RouterGroup) {
	// 网站列表
	g.GET("/site/list", func(c *gin.Context) {
		utils.Ok(c, service.ListSites())
	})

	// 全局默认页面（4 个页面内容 + 当前默认站点）
	g.GET("/site/default-pages", func(c *gin.Context) {
		utils.Ok(c, service.GetDefaultPages())
	})

	// 保存某个全局默认页面 {kind, content}
	g.POST("/site/default-pages/save", func(c *gin.Context) {
		var req struct {
			Kind    string `json:"kind" binding:"required"`
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveDefaultPage(req.Kind, req.Content); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.default_page", "保存默认页面: "+req.Kind, "success")
		utils.Ok(c, nil)
	})

	// 设置/清除默认站点 {id}（id=0 清除）
	g.POST("/site/default-site", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SetDefaultSite(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.default_site", "设置默认站点："+siteName(req.ID), "success")
		utils.Ok(c, nil)
	})

	// 创建网站
	g.POST("/site/create", func(c *gin.Context) {
		var req service.CreateSiteReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		site, err := service.CreateSite(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.create", "创建网站: "+req.Name, "success")
		utils.Ok(c, site)
	})

	// 站点启停
	g.POST("/site/action", func(c *gin.Context) {
		var req service.SiteActionReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SiteAction(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.action", req.Action+" 网站："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 删除网站
	g.POST("/site/delete", func(c *gin.Context) {
		var req service.DeleteSiteReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		result, err := service.DeleteSite(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.delete", "删除网站："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, result)
	})

	// 站点详情（设置面板）
	g.GET("/site/detail", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		detail, err := service.GetSiteDetail(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, detail)
	})

	// 保存网站设置
	g.POST("/site/settings", func(c *gin.Context) {
		var req service.SiteSettingsReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveSiteSettings(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.settings", "修改网站设置："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// HTTPS 设置
	g.POST("/site/settings/ssl", func(c *gin.Context) {
		var req service.SiteSSLReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.SaveSiteSSL(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.ssl", "修改 HTTPS 设置："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 检测 certbot
	g.GET("/site/ssl/certbot", func(c *gin.Context) {
		result, err := service.CheckCertbot()
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, result)
	})

	// 已申请证书列表
	g.GET("/site/ssl/certs", func(c *gin.Context) {
		utils.Ok(c, service.ListSSLCerts())
	})

	// 删除证书记录
	g.POST("/site/ssl/cert/delete", func(c *gin.Context) {
		var req service.DeleteSSLCertReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.DeleteSSLCert(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.ssl.cert_delete", "删除证书记录 ID="+strconv.FormatUint(uint64(req.ID), 10), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 下载证书（PEM + KEY）
	g.GET("/site/ssl/cert/download", func(c *gin.Context) {
		// 优先按站点 ID 读取当前站点已部署的证书
		if siteID := c.Query("site_id"); siteID != "" {
			id, err := strconv.ParseUint(siteID, 10, 32)
			if err != nil {
				utils.Fail(c, 400, "参数错误")
				return
			}
			cert, key, err := service.DownloadSiteSSL(uint(id))
			if err != nil {
				utils.Fail(c, 500, err.Error())
				return
			}
			utils.Ok(c, gin.H{"cert": cert, "key": key})
			return
		}
		// 按证书记录 ID 下载
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		cert, key, err := service.DownloadSSLCert(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"cert": cert, "key": key})
	})

	// 部署已有证书到站点
	g.POST("/site/ssl/apply-cert", func(c *gin.Context) {
		var req service.ApplyCertReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		result, err := service.ApplySSLCertToSite(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.ssl.apply_cert", "部署证书到网站："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, result)
	})

	// 一键申请 Let's Encrypt 证书（旧接口兼容）
	g.POST("/site/ssl/letsencrypt", func(c *gin.Context) {
		var req service.LetSEncryptReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		result, err := service.RequestLetsEncrypt(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.ssl.letsencrypt", "申请 Let's Encrypt 证书 网站："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, result)
	})

	// 一键申请 SSL 证书（支持 Let's Encrypt / LiteSSL）
	g.POST("/site/ssl/apply", func(c *gin.Context) {
		var req service.SSLApplyReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		result, err := service.ApplySSLCert(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.ssl.apply", "申请 "+req.Brand+" 证书 网站："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, result)
	})

	// DNS 验证申请（通配符证书）：创建订单并返回需要添加的 TXT 记录
	g.POST("/site/ssl/dns-start", func(c *gin.Context) {
		var req service.SSLApplyReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		resp, err := service.StartDNSApply(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, resp)
	})

	// 检查 DNS TXT 记录是否已生效
	g.POST("/site/ssl/dns-check", func(c *gin.Context) {
		var req struct {
			TaskID string `json:"task_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		resp, err := service.CheckDNSPropagation(req.TaskID)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, resp)
	})

	// 确认解析生效后完成签发
	g.POST("/site/ssl/dns-complete", func(c *gin.Context) {
		var req struct {
			TaskID string `json:"task_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		msg, err := service.CompleteDNSApply(req.TaskID)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "site.ssl.apply_dns", "DNS 验证申请证书 TaskID="+req.TaskID, "success")
		utils.Ok(c, gin.H{"message": msg})
	})

	// 续期全部 Let's Encrypt 证书
	g.POST("/site/ssl/renew", func(c *gin.Context) {
		output, err := service.RenewLetsEncrypt()
		if err != nil {
			utils.Fail(c, 500, output+err.Error())
			return
		}
		utils.Ok(c, map[string]string{"output": output})
	})

	// 读取站点 nginx 配置
	g.GET("/site/config", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		content, err := service.GetSiteConfig(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"content": content})
	})

	// 保存站点 nginx 配置
	g.POST("/site/config", func(c *gin.Context) {
		var req struct {
			ID      uint   `json:"id" binding:"required"`
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SaveSiteConfig(req.ID, req.Content); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.config", "修改 Nginx 配置："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 已安装 PHP-FPM 版本列表
	g.GET("/site/php-fpms", func(c *gin.Context) {
		utils.Ok(c, service.ListPhpFpms())
	})

	// 站点运行目录候选列表（root 下的一级子目录）
	g.GET("/site/subdirs", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		list, err := service.ListSiteSubdirs(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// 站点目录文件夹列表（用于重定向「按目录」多选）
	g.GET("/site/redirect/dirs", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		dirs, err := service.ListSiteDirs(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, dirs)
	})

	// 站点级 IP 黑名单（仅对该站点生效；与 WAF 全局 IP 拉黑独立）
	g.GET("/site/blockip", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		ips, err := service.ListSiteBlockIPs(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"ips": ips})
	})
	g.POST("/site/blockip/add", func(c *gin.Context) {
		var req service.SiteBlockIPReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		id, err := service.AddSiteBlockIP(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})
	g.POST("/site/blockip/delete", func(c *gin.Context) {
		var req struct {
			SiteID uint `json:"site_id" binding:"required"`
			ID     uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.RemoveSiteBlockIP(req.SiteID, req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 目录树浏览（用于弹窗选择目录）
	g.GET("/site/dir-tree", func(c *gin.Context) {
		path := c.Query("path")
		if path == "" {
			path = "/"
		}
		tree, err := service.DirTree(path)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, tree)
	})

	// 站点访问日志（结构化解析）
	g.GET("/site/logs", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		maxLines, _ := strconv.Atoi(c.Query("lines"))
		result, err := service.SiteLogs(uint(id), maxLines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, result)
	})

	// 站点访问日志分析（统计 + 风险 + IP 归属）
	g.GET("/site/logs/analyze", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Query("id"), 10, 32)
		if err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		maxLines, _ := strconv.Atoi(c.Query("lines"))
		result, err := service.SiteLogsAnalyze(uint(id), maxLines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, result)
	})

	// 更新站点备注
	g.POST("/site/remark", func(c *gin.Context) {
		var req service.UpdateSiteRemarkReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.UpdateSiteRemark(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "site.remark", "修改站点备注："+siteName(req.ID), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 站点访问统计查询：range 枚举值 yesterday / before_yesterday / 7d / 30d / custom
	// 自定义时带 start / end (YYYY-MM-DD)
	g.GET("/site/stat", func(c *gin.Context) {
		idStr := c.Query("id")
		rangeKind := c.DefaultQuery("range", "7d")
		start := c.Query("start")
		end := c.Query("end")
		id64, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id64 <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		// 限制只能查 90 天，避免恶意请求
		if rangeKind == "custom" {
			s, e := service.ParseStatDate(start), service.ParseStatDate(end)
			if s.IsZero() || e.IsZero() {
				utils.Fail(c, 400, "自定义时间格式错误")
				return
			}
			if e.Before(s) {
				utils.Fail(c, 400, "结束时间需晚于开始时间")
				return
			}
			if e.Sub(s) > 91*24*time.Hour {
				utils.Fail(c, 400, "时间范围最长 90 天")
				return
			}
		}
		stat, err := service.GetSiteStat(uint(id64), rangeKind, start, end)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, stat)
	})

	// importer 状态
	g.GET("/site/stat/status", func(c *gin.Context) {
		idStr := c.Query("id")
		id64, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id64 <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		ok, path, lerr := service.GetSiteStatImporterStatus(uint(id64))
		utils.Ok(c, gin.H{
			"running":     ok,
			"log_path":    path,
			"last_error":  lerr,
			"ip_region":   service.IpRegionEnabled(),
		})
	})
}

// siteName 返回站点名称（带 ID），用于操作日志描述。
func siteName(id uint) string {
	if id == 0 {
		return "无"
	}
	s, err := service.GetSiteDetail(id)
	if err != nil || s == nil {
		return "ID=" + strconv.FormatUint(uint64(id), 10)
	}
	if s.Name != "" {
		return s.Name + " (ID=" + strconv.FormatUint(uint64(id), 10) + ")"
	}
	return "ID=" + strconv.FormatUint(uint64(id), 10)
}
