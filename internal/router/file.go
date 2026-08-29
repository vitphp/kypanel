package router

import (
	"io"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// pathParam 兼容两种调用方式读取 path 参数：
// GET：/file/list?path=/root；POST：body {"path":"/root"}（长路径更安全）
func pathParam(c *gin.Context) string {
	if p := c.Query("path"); p != "" {
		return p
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err == nil {
		return req.Path
	}
	return ""
}

// setupFileRoutes 文件管理路由
func setupFileRoutes(g *gin.RouterGroup) {
	// 目录列表（兼容 GET query / POST body）
	fileListHandler := func(c *gin.Context) {
		items, err := service.ListDir(pathParam(c))
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, items)
	}
	g.GET("/file/list", fileListHandler)
	g.POST("/file/list", fileListHandler)

	// 读取文件内容（兼容 GET query / POST body）
	fileReadHandler := func(c *gin.Context) {
		content, err := service.ReadFile(pathParam(c))
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"content": content})
	}
	g.GET("/file/read", fileReadHandler)
	g.POST("/file/read", fileReadHandler)

	// 写入文件
	g.POST("/file/write", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.WriteFile(req.Path, req.Content); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.write", req.Path, "success")
		utils.Ok(c, nil)
	})

	// 创建目录
	g.POST("/file/mkdir", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.CreateDir(req.Path); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.mkdir", req.Path, "success")
		utils.Ok(c, nil)
	})

	// 创建文件
	g.POST("/file/create", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.CreateFile(req.Path); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.create", req.Path, "success")
		utils.Ok(c, nil)
	})

	// 重命名/移动
	g.POST("/file/rename", func(c *gin.Context) {
		var req struct {
			OldPath string `json:"old_path" binding:"required"`
			NewPath string `json:"new_path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.RenameFile(req.OldPath, req.NewPath); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.rename", req.OldPath+" -> "+req.NewPath, "success")
		utils.Ok(c, nil)
	})

	// 删除（先进回收站）
	g.POST("/file/delete", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteToTrash(req.Path); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.delete", req.Path, "success")
		utils.Ok(c, nil)
	})

	// 复制（src -> dst 目录）
	g.POST("/file/copy", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			DestDir string `json:"dest_dir" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.CopyPath(req.Path, req.DestDir); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.copy", req.Path+" -> "+req.DestDir, "success")
		utils.Ok(c, nil)
	})

	// 移动（src -> dst 目录）
	g.POST("/file/mv", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			DestDir string `json:"dest_dir" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.MovePath(req.Path, req.DestDir); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.mv", req.Path+" -> "+req.DestDir, "success")
		utils.Ok(c, nil)
	})

	// 远程下载（URL 到指定目录）：异步执行，返回任务 ID
	g.POST("/file/remote_download", func(c *gin.Context) {
		var req struct {
			URL  string `json:"url" binding:"required"`
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		id, err := service.StartRemoteDownload(req.URL, req.Path)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.remote_download", req.URL+" -> "+req.Path, "success")
		utils.Ok(c, gin.H{"id": id})
	})

	// 远程下载任务列表（含实时进度）：供右下角传输面板轮询
	g.GET("/file/remote_download/tasks", func(c *gin.Context) {
		utils.Ok(c, service.GetRemoteDownloadTasks())
	})

	// 上传文件（统一接口，字节级断点续传）。
	// - sub_path 可选：文件夹上传时携带相对子路径，保持目录结构
	// - sub_path 可选：文件夹上传时携带相对子路径，保持目录结构
	// - file_id 可选：携带时启用字节级续传，服务端写入临时文件 <dst>.<总大小>.<file_id>.upload.tmp
	// - offset 可选：续传起点字节数（服务端已写字节），缺省 0
	// - total_size 可选：文件总字节数；缺省时按"本次请求即整个文件"处理（脚本/直传兼容）
	// 关键：大文件上传必须流式处理——不能用 c.FormFile（它会先把整个 multipart body 读进内存/临时盘，
	// GB 级文件会卡到传完才执行 handler，导致"传 2 秒中断时一个临时文件也没生成"）。
	// 这里用 MultipartReader 逐 part 流式读，元数据 part 在前、file part 最后，读到 file 即边写临时文件。
	g.POST("/file/upload", func(c *gin.Context) {
		var (
			path        string
			subPath     string
			filename    string
			fileID      string
			offsetStr   string
			totalSizeStr string
			filePart    *multipart.Part
		)

		mr, err := c.Request.MultipartReader()
		if err != nil {
			utils.Fail(c, 400, "请求不是合法的 multipart 格式")
			return
		}

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				utils.Fail(c, 400, "读取上传数据失败")
				return
			}
			name := p.FormName()
			if name == "file" {
				// file part：稍后流式写入，先不在这里读，跳出循环
				filePart = p
				break
			}
			// 其余字段是文本元数据，直接读完整
			buf, _ := io.ReadAll(io.LimitReader(p, 1<<20)) // 单个元数据字段最多 1MB
			val := string(buf)
			switch name {
			case "path":
				path = val
			case "sub_path":
				subPath = strings.Trim(val, "/")
			case "filename":
				filename = strings.TrimSpace(val)
			case "file_id":
				fileID = strings.Trim(val, " ")
			case "offset":
				offsetStr = strings.TrimSpace(val)
			case "total_size":
				totalSizeStr = strings.TrimSpace(val)
			}
		}

		if filePart == nil {
			utils.Fail(c, 400, "缺少上传文件")
			return
		}
		defer filePart.Close()

		// filename 未显式传入时回退到 multipart 里的原始文件名
		if filename == "" {
			filename = filePart.FileName()
		}
		if path == "" || filename == "" {
			utils.Fail(c, 400, "缺少上传目标路径或文件名")
			return
		}

		cleanSub, err := service.CleanUploadSubPath(subPath)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}

		// 净化 file_id，避免被恶意利用做路径穿越
		if fileID != "" {
			fileID = strings.Map(func(r rune) rune {
				if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
					return r
				}
				return '_'
			}, fileID)
			if len(fileID) > 64 {
				fileID = fileID[:64]
			}
		}

		var offset int64 = 0
		if offsetStr != "" {
			offset, err = strconv.ParseInt(offsetStr, 10, 64)
			if err != nil || offset < 0 {
				utils.Fail(c, 400, "offset 非法")
				return
			}
		}
		var totalSize int64 = -1
		if totalSizeStr != "" {
			totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
			if err != nil || totalSize < 0 {
				utils.Fail(c, 400, "total_size 非法")
				return
			}
		}

		dst := service.JoinUploadPath(path, cleanSub, filename)

		// 流式写入临时文件：直接读 file part 的字节，边读边写（实时变大，类似迅雷/网盘的上传进度效果）
		written, complete, err := service.SaveUploadAppend(dst, fileID, offset, totalSize, filePart)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.upload", dst, "success")
		utils.Ok(c, gin.H{"offset": written, "complete": complete})
	})

	// 上传偏移探针：返回已写入临时文件的字节数（续传起点），及目标文件是否已存在
	g.GET("/file/upload/offset", func(c *gin.Context) {
		path := c.Query("path")
		subPath := strings.Trim(c.Query("sub_path"), "/")
		filename := c.Query("filename")
		fileID := strings.Trim(c.Query("file_id"), " ")
		totalSizeStr := strings.TrimSpace(c.Query("total_size"))
		if path == "" || filename == "" || fileID == "" {
			utils.Fail(c, 400, "缺少上传元数据")
			return
		}
		fileID = strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, fileID)
		if len(fileID) > 64 {
			fileID = fileID[:64]
		}
		var totalSize int64 = -1
		if totalSizeStr != "" {
			var err error
			totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
			if err != nil || totalSize < 0 {
				utils.Fail(c, 400, "total_size 非法")
				return
			}
		}
		cleanSub, err := service.CleanUploadSubPath(subPath)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		dst := service.JoinUploadPath(path, cleanSub, filename)
		offset, err := service.ProbeUploadOffset(dst, fileID, totalSize)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"offset": offset, "exists": service.FileExists(dst)})
	})

	// 覆盖上传前清理：删除目标文件及残留临时文件
	g.POST("/file/upload/reset", func(c *gin.Context) {
		path := c.PostForm("path")
		subPath := strings.Trim(c.PostForm("sub_path"), "/")
		filename := c.PostForm("filename")
		fileID := strings.Trim(c.PostForm("file_id"), " ")
		totalSizeStr := strings.TrimSpace(c.PostForm("total_size"))
		if path == "" || filename == "" || fileID == "" {
			utils.Fail(c, 400, "缺少上传元数据")
			return
		}
		fileID = strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, fileID)
		if len(fileID) > 64 {
			fileID = fileID[:64]
		}
		var totalSize int64 = -1
		if totalSizeStr != "" {
			var err error
			totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
			if err != nil || totalSize < 0 {
				utils.Fail(c, 400, "total_size 非法")
				return
			}
		}
		cleanSub, err := service.CleanUploadSubPath(subPath)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		dst := service.JoinUploadPath(path, cleanSub, filename)
		if err := service.ResetUploadParts(dst, fileID, totalSize); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 下载文件
	g.GET("/file/download", func(c *gin.Context) {
		path := c.Query("path")
		clean, err := service.SanitizePath(path)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		c.Header("Content-Disposition", "attachment")
		c.File(clean)
	})

	// 媒体/图片内联预览（浏览器直接渲染）
	g.GET("/file/preview", func(c *gin.Context) {
		path := c.Query("path")
		clean, err := service.SanitizePath(path)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		c.File(clean)
	})

	// 申请预览 token（需登录态；实际预览在 setupDownloadRoutes 的免 JWT 组）
	g.POST("/file/preview-token", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		clean, err := service.SanitizePath(req.Path)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		token := service.NewPreviewToken("file", clean)
		utils.Ok(c, gin.H{"token": token})
	})

	// 磁盘占用
	g.GET("/file/du", func(c *gin.Context) {
		path := c.Query("path")
		res, err := service.GetDiskUsage(path)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// 修改权限/所有者
	g.POST("/file/chmod", func(c *gin.Context) {
		var req struct {
			Path      string `json:"path" binding:"required"`
			Mode      string `json:"mode"`
			Owner     string `json:"owner"`
			Group     string `json:"group"`
			Recursive bool   `json:"recursive"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SetPerm(req.Path, req.Mode, req.Owner, req.Group, req.Recursive); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.chmod", req.Path+" -> mode="+req.Mode+" owner="+req.Owner+":"+req.Group+" recursive="+strconv.FormatBool(req.Recursive), "success")
		utils.Ok(c, nil)
	})

	// 系统用户列表
	g.GET("/file/users", func(c *gin.Context) {
		users, err := service.ListSystemUsers()
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, users)
	})

	// 推荐属主（按路径匹配站点运行用户，供权限弹窗预填）
	g.GET("/file/recommend_owner", func(c *gin.Context) {
		path := strings.TrimSpace(c.Query("path"))
		if path == "" {
			utils.Fail(c, 400, "参数错误")
			return
		}
		owner, group, site := service.RecommendOwner(path)
		utils.Ok(c, gin.H{"owner": owner, "group": group, "site": site})
	})

	// 系统用户组列表
	g.GET("/file/groups", func(c *gin.Context) {
		groups, err := service.ListSystemGroups()
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, groups)
	})

	// 压缩（支持多选：paths 数组）
	g.POST("/file/zip", func(c *gin.Context) {
		var req struct {
			Paths   []string `json:"paths"`
			Path    string   `json:"path"` // 兼容单源
			ZipPath string   `json:"zip_path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		paths := req.Paths
		if len(paths) == 0 && req.Path != "" {
			paths = []string{req.Path}
		}
		if len(paths) == 0 {
			utils.Fail(c, 400, "请选择要压缩的文件")
			return
		}
		if err := service.ZipFiles(paths, req.ZipPath); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.zip", req.ZipPath, "success")
		utils.Ok(c, nil)
	})

	// 解压
	g.POST("/file/unzip", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			DestDir string `json:"dest_dir" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UnzipFile(req.Path, req.DestDir); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.unzip", req.Path+" -> "+req.DestDir, "success")
		utils.Ok(c, nil)
	})

	// 文件名搜索（递归）
	g.POST("/file/search", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			Keyword string `json:"keyword" binding:"required"`
			Max     int    `json:"max"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		items, err := service.Search(req.Path, req.Keyword, req.Max)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, items)
	})

	// 语法检查（对当前编辑内容做语法校验）
	g.POST("/file/syntax_check", func(c *gin.Context) {
		var req struct {
			Path    string `json:"path" binding:"required"`
			Content string `json:"content"`
			Lang    string `json:"lang"` // 显式指定语言；空则按扩展名推断
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		lang := req.Lang
		if lang == "" {
			lang = service.SyntaxCheckLang(req.Path)
		}
		if lang == "" {
			utils.Fail(c, 400, "无法识别该文件的语法类型")
			return
		}
		utils.Ok(c, service.SyntaxCheck(lang, req.Content))
	})

	// ===== 回收站 =====

	// 回收站列表
	g.GET("/file/trash/list", func(c *gin.Context) {
		items, err := service.ListTrash()
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, items)
	})

	// 还原
	g.POST("/file/trash/restore", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.RestoreTrash(req.ID); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "file.trash.restore", strconv.FormatUint(uint64(req.ID), 10), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 彻底删除单条
	g.POST("/file/trash/purge", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.PurgeTrash(req.ID); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "file.trash.purge", strconv.FormatUint(uint64(req.ID), 10), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 清空回收站
	g.POST("/file/trash/empty", func(c *gin.Context) {
		if err := service.EmptyTrash(); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "file.trash.empty", "", "success")
		utils.Ok(c, nil)
	})
}
