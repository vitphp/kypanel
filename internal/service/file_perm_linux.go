//go:build linux

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"kypanel/internal/model"
)

// filePermInfo 返回文件的八进制权限(如 755)、属主、属组
func filePermInfo(info os.FileInfo) (perm, user, group string) {
	perm = fmt.Sprintf("%o", info.Mode().Perm())
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		user = lookupName("/etc/passwd", st.Uid)
		group = lookupName("/etc/group", st.Gid)
	}
	return
}

// lookupName 在 passwd/group 文件中按 id 查找名称，找不到则返回 id 字符串
func lookupName(file string, id uint32) string {
	idStr := strconv.FormatUint(uint64(id), 10)
	data, err := os.ReadFile(file)
	if err != nil {
		return idStr
	}
	return lookupNameInText(string(data), idStr)
}

func lookupNameInText(text, idStr string) string {
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[2] == idStr {
			return parts[0]
		}
	}
	return idStr
}

// lookupId 在 passwd/group 文件中按名称查找 id
func lookupId(file, name string) (uint32, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	return lookupIdInText(string(data), name)
}

func lookupIdInText(text, name string) (uint32, error) {
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[0] == name {
			id, err := strconv.ParseUint(parts[2], 10, 32)
			if err != nil {
				continue
			}
			return uint32(id), nil
		}
	}
	return 0, fmt.Errorf("%s not found", name)
}

// SystemUser 系统用户简要信息
type SystemUser struct {
	Name      string `json:"name"`
	Uid       uint32 `json:"uid"`
	Gid       uint32 `json:"gid"`
	GroupName string `json:"group_name"` // 主组名
	Home      string `json:"home"`
	Shell     string `json:"shell"`
}

// ListSystemUsers 读取 /etc/passwd，返回面板风格的用户列表：
// 仅包含 root 和普通用户（uid>=1000），自动去重
func ListSystemUsers() ([]SystemUser, error) {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}
	groupData, _ := os.ReadFile("/etc/group")

	var users []SystemUser
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 7 || parts[0] == "" {
			continue
		}
		uid, err1 := strconv.ParseUint(parts[2], 10, 32)
		gid, err2 := strconv.ParseUint(parts[3], 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		// 只保留 root、普通用户，以及常见 Web 运行用户（www-data、nginx 等）
		webUsers := map[string]bool{
			"www-data": true, "www": true, "nginx": true,
			"apache":   true, "http": true, "php-fpm": true,
			"php-fcgi": true,
		}
		if uid != 0 && uid < 1000 && !webUsers[parts[0]] {
			continue
		}
		if seen[parts[0]] {
			continue
		}
		seen[parts[0]] = true
		users = append(users, SystemUser{
			Name:      parts[0],
			Uid:       uint32(uid),
			Gid:       uint32(gid),
			GroupName: groupNameOf(string(groupData), uint32(gid)),
			Home:      parts[5],
			Shell:     parts[6],
		})
	}
	return users, nil
}

// ListSystemGroups 读取 /etc/group 返回组名列表（去重）
func ListSystemGroups() ([]string, error) {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}
	var groups []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		name := strings.SplitN(line, ":", 2)[0]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		groups = append(groups, name)
	}
	return groups, nil
}

// groupNameOf 按 gid 在 /etc/group 文本中查找组名
func groupNameOf(text string, gid uint32) string {
	gidStr := strconv.FormatUint(uint64(gid), 10)
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[2] == gidStr {
			return parts[0]
		}
	}
	return gidStr
}

// SetPerm 修改文件/目录的权限与属主
// owner 或 group 为空时不修改；recursive 为 true 时递归应用到子目录
func SetPerm(path, mode, owner, group string, recursive bool) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}

	// 解析权限
	var perm os.FileMode
	if strings.TrimSpace(mode) != "" {
		m, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
		if err != nil || m > 0o7777 {
			return errors.New("权限格式错误，请使用如 755 的八进制数字")
		}
		perm = os.FileMode(m)
	}

	// 解析属主/属组
	var uid, gid int = -1, -1
	if strings.TrimSpace(owner) != "" {
		u, err := lookupId("/etc/passwd", strings.TrimSpace(owner))
		if err != nil {
			return fmt.Errorf("找不到用户 %s", owner)
		}
		uid = int(u)
		// 未指定组时，默认使用用户的主组
		if strings.TrimSpace(group) == "" {
			if g, err := lookupUserGid(owner); err == nil {
				gid = int(g)
			}
		}
	}
	if strings.TrimSpace(group) != "" {
		g, err := lookupId("/etc/group", strings.TrimSpace(group))
		if err != nil {
			return fmt.Errorf("找不到用户组 %s", group)
		}
		gid = int(g)
	}

	apply := func(p string, info os.FileInfo) error {
		if perm != 0 {
			if err := os.Chmod(p, perm); err != nil {
				return err
			}
		}
		if uid != -1 || gid != -1 {
			// 对符号链接使用 Lchown，避免意外修改指向目标
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Lchown(p, uid, gid); err != nil {
					return err
				}
			} else {
				if err := os.Chown(p, uid, gid); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if !recursive {
		info, err := os.Lstat(clean)
		if err != nil {
			return err
		}
		return apply(clean, info)
	}

	// 递归处理
	return filepath.Walk(clean, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无权限访问的文件
		}
		return apply(p, info)
	})
}

// lookupUserGid 按用户名查找主组 id
func lookupUserGid(name string) (uint32, error) {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 4 && parts[0] == name {
			gid, err := strconv.ParseUint(parts[3], 10, 32)
			if err != nil {
				continue
			}
			return uint32(gid), nil
		}
	}
	return 0, fmt.Errorf("user %s not found", name)
}

// RecommendOwner 根据路径推荐安全的属主/属组：
//   - 站点根目录 → Web 运行用户（如 www/www-data，探测实际进程属主）
//   - 数据库/缓存目录 → 对应运行用户（mysql/postgres/redis/mongodb）
//   - 其它 → 返回空，前端沿用文件当前属主
//
// 返回 site 为命中的站点名（可能为空）。
func RecommendOwner(path string) (owner, group, site string) {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." || clean == "/" {
		return "", "", ""
	}

	// 1) 匹配站点根目录（最长前缀优先）
	var sites []model.Site
	if err := model.DB.Where("root <> ''").Find(&sites).Error; err == nil {
		bestLen := 0
		for i := range sites {
			r := filepath.Clean(sites[i].Root)
			if r == "/" || r == "." {
				continue
			}
			if (clean == r || strings.HasPrefix(clean, r+string(filepath.Separator))) && len(r) > bestLen {
				bestLen = len(r)
				site = sites[i].Name
			}
		}
	}

	// 2) Web 站点或通用站点目录 → Web 运行用户
	if site != "" || strings.HasPrefix(clean, webRootBase+string(filepath.Separator)) {
		u := webRunUser()
		if u == "" {
			return "", "", site
		}
		return u, userPrimaryGroupName(u), site
	}

	// 3) 数据库/缓存数据目录特例
	for _, db := range []struct {
		prefix string
		user   string
	}{
		{"/var/lib/mysql", "mysql"},
		{"/var/lib/postgresql", "postgres"},
		{"/var/lib/redis", "redis"},
		{"/var/lib/mongodb", "mongodb"},
	} {
		if strings.HasPrefix(clean, db.prefix) && userExists(db.user) {
			return db.user, userPrimaryGroupName(db.user), ""
		}
	}

	return "", "", ""
}

// webRunUser 探测实际 Web 运行用户：优先 php-fpm / nginx / apache2 进程属主，兜底常见用户名
func webRunUser() string {
	for _, proc := range []string{"php-fpm", "nginx", "apache2", "httpd"} {
		out, err := exec.Command("ps", "-o", "user=", "-C", proc).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			u := strings.TrimSpace(line)
			if u == "" || u == "root" {
				continue
			}
			if userExists(u) {
				return u
			}
		}
	}
	for _, u := range []string{"www-data", "www", "nginx", "apache"} {
		if userExists(u) {
			return u
		}
	}
	return ""
}

// userExists 检查系统用户是否存在
func userExists(name string) bool {
	if name == "" {
		return false
	}
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 1 && parts[0] == name {
			return true
		}
	}
	return false
}

// ChownToWebUser 把路径的属主/属组改为当前 Web 运行用户。
// 仅对 /www/wwwroot 下的路径生效，避免误改系统文件。
func ChownToWebUser(path string, recursive bool) error {
	clean, err := SanitizePath(path)
	if err != nil {
		return err
	}
	if clean != webRootBase && !strings.HasPrefix(clean, webRootBase+string(filepath.Separator)) {
		return nil
	}
	user := webRunUser()
	if user == "" {
		return nil
	}
	group := userPrimaryGroupName(user)
	if group == "" {
		group = user
	}
	return SetPerm(clean, "", user, group, recursive)
}

// userPrimaryGroupName 返回用户主组名（读取 /etc/passwd + /etc/group）
func userPrimaryGroupName(name string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	var gid string
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 4 && parts[0] == name {
			gid = parts[3]
			break
		}
	}
	if gid == "" {
		return ""
	}
	g, err := strconv.ParseUint(gid, 10, 32)
	if err != nil {
		return ""
	}
	gdata, _ := os.ReadFile("/etc/group")
	return groupNameOf(string(gdata), uint32(g))
}
