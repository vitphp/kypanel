package service

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"kypanel/internal/model"
)

// FtpAvailable 检测 vsftpd 是否可用
func FtpAvailable() (bool, string) {
	// vsftpd 二进制位于 /usr/sbin/vsftpd；systemd 服务进程的 PATH 通常不含 /usr/sbin，
	// 用 LookPathBin（依赖 PATH）会误判为未安装，改为绝对路径检测。
	if _, err := os.Stat("/usr/sbin/vsftpd"); err != nil {
		return false, "未检测到 vsftpd，请先安装：apt-get install -y vsftpd 或 yum install -y vsftpd"
	}
	return true, ""
}

// FtpVersion 探测 vsftpd 版本号。
// 只要二进制存在就保证返回非空值：优先走内置 VersionCmd（包管理器）探测真实版本，
// 探测失败时退回占位值，避免「已安装但版本探测失败」被上层误判为未安装。
func FtpVersion() string {
	if _, err := os.Stat("/usr/sbin/vsftpd"); err != nil {
		return ""
	}
	if meta, ok := findApp("ftp"); ok && meta.VersionCmd != "" {
		if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
			return v
		}
	}
	return "unknown"
}

// FtpUserItem FTP 用户条目（含密码占位与系统状态）
type FtpUserItem struct {
	model.FtpUser
	SystemExists bool   `json:"system_exists"` // 系统用户是否存在
	PasswordSet  bool   `json:"password_set"`  // 是否设置了密码
	Locked       bool   `json:"locked"`        // 系统用户是否被锁定
}

// ListFtpUsers 列出 FTP 用户
func ListFtpUsers() []FtpUserItem {
	var users []model.FtpUser
	model.DB.Order("id DESC").Find(&users)
	items := make([]FtpUserItem, 0, len(users))
	for _, u := range users {
		item := FtpUserItem{FtpUser: u}
		res, _ := ExecCommand(fmt.Sprintf("id %s", shellQuote(u.Username)), 10*time.Second)
		item.SystemExists = res != nil && res.ExitCode == 0
		if item.SystemExists {
			// 判断是否锁定：passwd -S 输出第二列为 L 表示锁定
			ps, _ := ExecCommand(fmt.Sprintf("passwd -S %s 2>/dev/null", shellQuote(u.Username)), 10*time.Second)
			if ps != nil && ps.ExitCode == 0 {
				fields := strings.Fields(ps.Stdout)
				if len(fields) >= 2 && strings.ToUpper(fields[1]) == "L" {
					item.Locked = true
				} else {
					item.PasswordSet = true
				}
			}
		}
		items = append(items, item)
	}
	return items
}

// CreateFtpUserReq 创建 FTP 用户请求
type CreateFtpUserReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	HomeDir  string `json:"home_dir" binding:"required"`
	Remark   string `json:"remark"`
}

var ftpUserRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// CreateFtpUser 创建 FTP 用户
func CreateFtpUser(req CreateFtpUserReq) error {
	if !ftpUserRe.MatchString(req.Username) {
		return errors.New("用户名只能包含字母、数字、下划线")
	}
	if len(req.Username) < 2 || len(req.Username) > 32 {
		return errors.New("用户名长度需在 2~32 之间")
	}
	if len(req.Password) < 6 {
		return errors.New("密码长度至少 6 位")
	}
	if !strings.HasPrefix(req.HomeDir, "/") {
		return errors.New("家目录必须是绝对路径")
	}

	// 用户名不能与现有系统用户冲突
	if res, _ := ExecCommand(fmt.Sprintf("id %s", shellQuote(req.Username)), 10*time.Second); res != nil && res.ExitCode == 0 {
		// 检查是否已由面板管理
		var cnt int64
		model.DB.Model(&model.FtpUser{}).Where("username = ?", req.Username).Count(&cnt)
		if cnt == 0 {
			return errors.New("系统已存在同名用户，请换一个用户名")
		}
	}

	// 创建家目录
	if err := os.MkdirAll(req.HomeDir, 0o755); err != nil {
		return errors.New("创建家目录失败: " + err.Error())
	}

	// 创建系统用户（nologin，仅 FTP）
	cmd := fmt.Sprintf("useradd -d %s -s /sbin/nologin %s", shellQuote(req.HomeDir), shellQuote(req.Username))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return errors.New("创建系统用户失败: " + err.Error())
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}

	// 设置密码
	chpwd := fmt.Sprintf("echo %s | chpasswd", shellQuote(req.Username+":"+req.Password))
	res2, err := ExecCommand(chpwd, 15*time.Second)
	if err != nil || res2.ExitCode != 0 {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = strings.TrimSpace(res2.Stderr)
		}
		_, _ = ExecCommand(fmt.Sprintf("userdel -r %s", shellQuote(req.Username)), 15*time.Second)
		return errors.New("设置密码失败: " + msg)
	}

	// 记录到面板数据库
	u := model.FtpUser{Username: req.Username, HomeDir: req.HomeDir, Remark: req.Remark, Status: "enabled"}
	if err := model.DB.Create(&u).Error; err != nil {
		_, _ = ExecCommand(fmt.Sprintf("userdel -r %s", shellQuote(req.Username)), 15*time.Second)
		return errors.New("保存记录失败: " + err.Error())
	}
	return nil
}

// DeleteFtpUser 删除 FTP 用户
func DeleteFtpUser(id uint) error {
	var u model.FtpUser
	if err := model.DB.First(&u, id).Error; err != nil {
		return errors.New("用户不存在")
	}
	// 删除系统用户（不删家目录，避免误删网站数据）
	res, err := ExecCommand(fmt.Sprintf("userdel %s", shellQuote(u.Username)), 15*time.Second)
	if err != nil {
		return errors.New("删除系统用户失败: " + err.Error())
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stderr, "does not exist") {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return model.DB.Delete(&model.FtpUser{}, id).Error
}

// ToggleFtpUser 启用/禁用 FTP 用户
func ToggleFtpUser(id uint, enable bool) error {
	var u model.FtpUser
	if err := model.DB.First(&u, id).Error; err != nil {
		return errors.New("用户不存在")
	}
	action := "-L"
	if enable {
		action = "-U"
	}
	res, err := ExecCommand(fmt.Sprintf("usermod %s %s", action, shellQuote(u.Username)), 15*time.Second)
	if err != nil {
		return errors.New("操作失败: " + err.Error())
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	status := "disabled"
	if enable {
		status = "enabled"
	}
	return model.DB.Model(&u).Update("status", status).Error
}
