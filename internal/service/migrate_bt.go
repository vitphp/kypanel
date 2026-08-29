package service

// ==================== 迁出到对端面板 ====================
// 本面板（kypanel）作为源端，把网站/数据库/FTP 迁移到目标对端面板：
//   1. 本机打包生成迁移包（复用 ExportMigration）
//   2. 调对端面板 API 创建网站（AddSite）
//   3. 上传网站文件包并解压到站点目录
//   4. 调对端面板 API 创建数据库（AddDatabase）+ 导入 SQL
//   5. 调对端面板 API 创建 FTP 账号（AddUser）

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kypanel/internal/model"
)

// BTExportRequest 迁出到对端面板的请求
type BTExportRequest struct {
	BTURL       string   `json:"bt_url"`       // 对端面板地址，如 http://1.2.3.4:8888
	BTSK        string   `json:"bt_sk"`        // 对端面板 API 接口密钥
	Sites       []string `json:"sites"`        // 选中的网站
	Databases   []string `json:"databases"`    // 选中的数据库
	FTPs        []string `json:"ftps"`         // 选中的 FTP
	DBPassword  string   `json:"db_password"`  // 数据库新密码（空则沿用源端）
	FTPPassword string   `json:"ftp_password"` // FTP 新密码（空则随机生成）

	// 冲突处理决策（key 为网站/数据库/FTP 名称，true=覆盖，false=跳过）。
	// 由前端在迁移前检测后弹窗确认生成。
	SiteOverwrite    map[string]bool `json:"site_overwrite"`
	DatabaseOverwrite map[string]bool `json:"database_overwrite"`
	FtpOverwrite      map[string]bool `json:"ftp_overwrite"`
}

// StartExportToBT 开始迁出到对端面板（异步，返回任务 ID）
func StartExportToBT(req BTExportRequest) (string, error) {
	if req.BTURL == "" || req.BTSK == "" {
		return "", errors.New("请填写对端面板地址和 API 密钥")
	}
	if len(req.Sites) == 0 && len(req.Databases) == 0 && len(req.FTPs) == 0 {
		return "", errors.New("请至少选择一个迁移对象")
	}
	id := "btexport-" + time.Now().Format("20060102150405")
	task := newImportTask(id)
	go runExportToBT(task, req)
	return id, nil
}

func runExportToBT(t *ImportTask, req BTExportRequest) {
	defer func() {
		if r := recover(); r != nil {
			t.mu.Lock()
			t.Status = "failed"
			t.Error = fmt.Sprintf("%v", r)
			t.mu.Unlock()
			t.logf("迁移失败: %v", r)
		}
	}()

	// 1. 本机打包
	t.logf("正在打包本机网站/数据库...")
	exp, err := ExportMigration(req.Sites, req.Databases, req.FTPs)
	if err != nil {
		t.fail("打包失败: " + err.Error())
		return
	}
	pkgPath := migrateExportFile(exp.ID)
	defer os.Remove(pkgPath)
	t.logf("打包完成：网站 %d 个、数据库 %d 个、FTP %d 个",
		len(exp.Manifest.Sites), len(exp.Manifest.Databases), len(exp.Manifest.FTPs))

	// 解压迁移包到工作目录
	workDir := filepath.Join(migrateRoot(), "bt-work-"+exp.ID)
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)
	if err := ungzTar(pkgPath, workDir); err != nil {
		t.fail("解压迁移包失败: " + err.Error())
		return
	}

	// 2. 连接对端面板
	t.logf("正在连接对端面板 %s ...", req.BTURL)
	bt := NewBTClient(req.BTURL, req.BTSK)
	existing, err := bt.SiteList()
	if err != nil {
		t.fail("连接对端面板失败: " + err.Error())
		return
	}
	// name → {id, webname}，用于覆盖时删除
	type btSiteEntry struct{ id, webname string }
	btSiteMap := map[string]btSiteEntry{}
	for _, s := range existing {
		n, _ := s["name"].(string)
		if n == "" {
			continue
		}
		id := fmt.Sprintf("%v", s["id"])
		btSiteMap[n] = btSiteEntry{id: id, webname: btParseWebname(s)}
	}
	t.logf("对端面板连接成功（当前 %d 个网站）", len(existing))

	// 3. 创建网站 + 上传文件
	for _, ms := range exp.Manifest.Sites {
		if info, exists := btSiteMap[ms.Name]; exists {
			if !req.SiteOverwrite[ms.Name] {
				t.logf("网站 %s 在对端面板已存在，按选择跳过", ms.Name)
				continue
			}
			t.logf("网站 %s 在对端面板已存在，先删除后重建（覆盖）...", ms.Name)
			if _, err := bt.DeleteSite(info.id, info.webname); err != nil {
				t.fail("覆盖网站 " + ms.Name + " 失败（请到对端面板手动删除该网站后重试）: " + err.Error())
				return
			}
		}
		domains := ms.Domain
		if ms.Domains != "" {
			domains += "," + ms.Domains
		}
		phpVer := ms.PhpVersion
		if phpVer == "" {
			phpVer = "74"
		}
		// 目标建站目录沿用源站根目录，保证迁移前后网站文件夹路径一致
		path := ms.Root
		if path == "" {
			path = filepath.Join("/www/wwwroot", ms.Name)
		}
		t.logf("在对端面板创建网站 %s（域名 %s，PHP %s，目录 %s）...", ms.Name, domains, phpVer, path)
		if _, err := bt.AddSite(ms.Name, domains, path, phpVer); err != nil {
			t.fail("创建网站 " + ms.Name + " 失败: " + err.Error())
			return
		}
		t.logf("网站 %s 创建成功", ms.Name)

		// 上传网站文件
		src := filepath.Join(workDir, "sites", ms.Name, "wwwroot.tar.gz")
		if !fileExists(src) {
			t.logf("网站 %s 无文件包，跳过文件上传", ms.Name)
			continue
		}
		remote := "/www/backup/migrate-" + ms.Name + ".tar.gz"
		t.logf("上传网站 %s 文件包...", ms.Name)
		lastProg := time.Now()
		if err := bt.Upload(src, remote, func(done, total int64) {
			if time.Since(lastProg) < 2*time.Second {
				return
			}
			lastProg = time.Now()
			pct := float64(0)
			if total > 0 {
				pct = float64(done) / float64(total) * 100
			}
			t.logf("上传网站 %s 文件包中... %.0f%%（%d/%d MB）", ms.Name, pct, done/1024/1024, total/1024/1024)
		}); err != nil {
			t.fail("上传网站 " + ms.Name + " 文件失败: " + err.Error())
			return
		}
		t.logf("解压网站 %s 文件到站点目录...", ms.Name)
		if err := bt.Unzip(remote, path); err != nil {
			t.fail("解压网站 " + ms.Name + " 文件失败: " + err.Error())
			return
		}
		btDeleteRemoteFile(bt, remote)
		t.logf("网站 %s 文件恢复完成", ms.Name)
	}

	// 4. 数据库
	dbsExist := map[string]string{}
	if dbs, err := bt.DatabaseList(); err == nil {
		for _, d := range dbs {
			if id, _ := d["id"].(string); id != "" {
				if n, _ := d["name"].(string); n != "" {
					dbsExist[n] = id
				}
			}
		}
	}
	for _, db := range exp.Manifest.Databases {
		pass := req.DBPassword
		if pass == "" {
			pass = db.Password
		}
		if pass == "" {
			pass = randomPassword(16)
		}
		if dbID, exists := dbsExist[db.Name]; exists {
			if !req.DatabaseOverwrite[db.Name] {
				t.logf("数据库 %s 在对端面板已存在，按选择跳过（含 SQL 导入）", db.Name)
				continue
			}
			t.logf("数据库 %s 在对端面板已存在，先删除后重建（覆盖）...", db.Name)
			if _, err := bt.DeleteDatabase(dbID, db.Name); err != nil {
				t.fail("覆盖数据库 " + db.Name + " 失败（请到对端面板手动删除该数据库后重试）: " + err.Error())
				return
			}
			delete(dbsExist, db.Name)
		}
		t.logf("在对端面板创建数据库 %s ...", db.Name)
		if _, err := bt.AddDatabase(db.Name, db.User, pass); err != nil {
			t.fail("创建数据库 " + db.Name + " 失败: " + err.Error())
			return
		}
		t.logf("数据库 %s 创建成功", db.Name)

		// 导入 SQL
		sqlLocal := filepath.Join(workDir, "databases", db.Name+".sql.gz")
		if !fileExists(sqlLocal) {
			t.logf("数据库 %s 无备份文件，跳过导入", db.Name)
			continue
		}
		// 解压为明文 SQL 再上传（对端面板导入接口通常要求明文）
		plain := filepath.Join(workDir, "databases", db.Name+".sql")
		if err := gunzipFile(sqlLocal, plain); err != nil {
			t.logf("数据库 %s 备份解压失败: %v", db.Name, err)
			continue
		}
		remoteSQL := "/www/backup/migrate-" + db.Name + ".sql"
		t.logf("上传数据库 %s 备份...", db.Name)
		if err := bt.Upload(plain, remoteSQL, nil); err != nil {
			t.fail("上传数据库 " + db.Name + " 备份失败: " + err.Error())
			return
		}
		dbID := dbsExist[db.Name]
		if dbID == "" {
			if dbs, err := bt.DatabaseList(); err == nil {
				for _, d := range dbs {
					if n, _ := d["name"].(string); n == db.Name {
						dbID, _ = d["id"].(string)
					}
				}
			}
		}
		t.logf("导入数据库 %s ...", db.Name)
		if _, err := bt.ImportDatabase(dbID, remoteSQL); err != nil {
			t.logf("数据库 %s 自动导入失败（可到对端面板手动导入 %s）：%v", db.Name, remoteSQL, err)
		} else {
			t.logf("数据库 %s 导入完成", db.Name)
		}
		btDeleteRemoteFile(bt, remoteSQL)
	}

	// 5. FTP
	btFtpExist := map[string]bool{}
	if ftpList, err := bt.FtpUserList(); err == nil {
		for _, u := range ftpList {
			if n, _ := u["username"].(string); n != "" {
				btFtpExist[n] = true
			}
		}
	}
	for _, f := range exp.Manifest.FTPs {
		if btFtpExist[f.Username] {
			if !req.FtpOverwrite[f.Username] {
				t.logf("FTP 账号 %s 在对端面板已存在，按选择跳过", f.Username)
				continue
			}
			t.logf("FTP 账号 %s 在对端面板已存在，先删除后重建（覆盖）...", f.Username)
			if _, err := bt.DeleteFtpUser(f.Username); err != nil {
				t.fail("覆盖 FTP 账号 " + f.Username + " 失败: " + err.Error())
				return
			}
		}
		pass := req.FTPPassword
		if pass == "" {
			pass = randomPassword(12)
		}
		t.logf("在对端面板创建 FTP 账号 %s（密码 %s）...", f.Username, pass)
		home := f.HomeDir
		if home == "" {
			home = "/www/wwwroot/" + f.Username
		}
		if _, err := bt.AddFtpUser(f.Username, pass, home); err != nil {
			t.fail("创建 FTP 账号 " + f.Username + " 失败: " + err.Error())
			return
		}
		t.logf("FTP 账号 %s 创建完成", f.Username)
	}

	t.mu.Lock()
	t.Status = "success"
	t.mu.Unlock()
	t.logf("迁出到对端面板完成！请到对端面板确认站点/数据库/FTP 状态。")
}

// btDeleteRemoteFile 删除对端面板服务器上的临时文件（失败不报错）
func btDeleteRemoteFile(bt *BTClient, path string) {
	params := url.Values{}
	params.Set("name", path)
	if _, err := bt.btRequest("files", "DeleteFile", params); err != nil {
		_ = err
	}
}

// gunzipFile 将 .sql.gz 解压为明文 .sql
func gunzipFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gr.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, gr)
	return err
}

// BTEnvCompare 预检：把本机选中网站需要的环境与对端面板已装 PHP 版本对比
func BTEnvCompare(btURL, btSK string, sites []string) (map[string]any, error) {
	bt := NewBTClient(btURL, btSK)
	phpList, err := bt.PHPVersionList()
	if err != nil {
		return nil, err
	}
	// 对端面板返回版本号可能带点（如 "7.4"）或纯数字（"74"），统一转 Remi 风格
	normalized := map[string]bool{}
	for _, v := range phpList {
		v = strings.TrimSpace(v)
		normalized[v] = true
		key := strings.ReplaceAll(v, ".", "")
		normalized[key] = true
	}
	need := map[string]bool{}
	for _, name := range sites {
		var s model.Site
		if err := model.DB.Where("name = ?", name).First(&s).Error; err != nil {
			continue
		}
		if s.Type == model.SiteTypePHP {
			v := phpVersionFromFpm(s.PhpFpm)
			if v == "" {
				v = "74"
			}
			need[v] = true
		}
	}
	var missing []string
	for v := range need {
		if !normalized[v] {
			missing = append(missing, v)
		}
	}
	return map[string]any{
		"php_installed": normalized,
		"php_required":  need,
		"php_missing":   missing,
		"all_ready":     len(missing) == 0,
	}, nil
}
