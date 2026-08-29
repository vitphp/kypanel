package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============ Docker 基础能力 ============

// DockerInfo Docker 可用性信息
type DockerInfo struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

// GetDockerInfo 检测 docker 是否安装且守护进程在运行
func GetDockerInfo() DockerInfo {
	info := DockerInfo{}
	// 判定"是否安装"不能只看 docker CLI：卸载 docker.io 后 docker-cli 可能残留
	//（apt remove docker.io 只删元包，docker-cli/docker-buildx 等依赖子包不会自动删）。
	// 必须同时确认守护进程二进制 dockerd 存在，否则会把"引擎已卸载、仅剩 CLI"误判为已安装，
	// 前端会显示"守护进程未运行"的英文原始错误而非友好的"未安装"引导。
	if _, err := LookPathBin("docker"); err != nil {
		info.Installed = false
		info.Error = "未检测到 docker 命令，请先安装 Docker"
		return info
	}
	if _, err := LookPathBin("dockerd"); err != nil {
		info.Installed = false
		info.Error = "Docker 引擎（dockerd）未安装，请先安装 Docker"
		return info
	}
	info.Installed = true
	res, err := ExecCommand("docker version --format '{{.Server.Version}}'", 10*time.Second)
	if err != nil {
		info.Running = false
		info.Error = "Docker 守护进程未运行: " + err.Error()
		return info
	}
	if res.ExitCode != 0 {
		info.Running = false
		info.Error = strings.TrimSpace(res.Stderr)
		return info
	}
	info.Running = true
	info.Version = strings.TrimSpace(res.Stdout)
	return info
}

// ContainerInfo 容器信息
type ContainerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Ports   string `json:"ports"`
	Running bool   `json:"running"`
}

// ListContainers 列出所有容器（含停止）
func ListContainers() ([]ContainerInfo, error) {
	return listContainers("docker ps -a")
}

// ListRunningContainers 只列出运行中容器
func ListRunningContainers() ([]ContainerInfo, error) {
	return listContainers("docker ps")
}

func listContainers(base string) ([]ContainerInfo, error) {
	cmd := base + " --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}'"
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(res.Stderr))
	}
	var list []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		ci := ContainerInfo{
			ID:     parts[0],
			Name:   parts[1],
			Image:  parts[2],
			Status: parts[3],
			Ports:  "",
		}
		if len(parts) >= 5 {
			ci.Ports = parts[4]
		}
		ci.Running = strings.HasPrefix(ci.Status, "Up")
		list = append(list, ci)
	}
	return list, nil
}

// ImageInfo 镜像信息
type ImageInfo struct {
	ID      string `json:"id"`
	Tag     string `json:"tag"`
	Size    string `json:"size"`
	Created string `json:"created"`
}

// ListImages 列出镜像
func ListImages() ([]ImageInfo, error) {
	res, err := ExecCommand("docker images --format '{{.ID}}|{{.Repository}}:{{.Tag}}|{{.Size}}|{{.CreatedSince}}'", 15*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(res.Stderr))
	}
	var list []ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		img := ImageInfo{ID: parts[0], Tag: parts[1], Size: parts[2]}
		if len(parts) >= 4 {
			img.Created = parts[3]
		}
		list = append(list, img)
	}
	return list, nil
}

// CreateContainerReq 创建容器请求
type CreateContainerReq struct {
	Name    string   `json:"name" binding:"required"`
	Image   string   `json:"image" binding:"required"`
	Ports   []string `json:"ports"`   // 端口映射 ["8080:80"]
	Volumes []string `json:"volumes"` // 卷挂载 ["/data:/data"]
	Env     []string `json:"env"`     // 环境变量 ["MYSQL_ROOT_PASSWORD=123456"]
	Restart string   `json:"restart"` // no / always / unless-stopped
	Command string   `json:"command"` // 附加命令
}

// CreateContainer 创建并启动容器
func CreateContainer(req CreateContainerReq) error {
	args := []string{"docker", "run", "-d", "--name", req.Name}
	if req.Restart == "" {
		req.Restart = "unless-stopped"
	}
	args = append(args, "--restart", req.Restart)
	for _, p := range req.Ports {
		args = append(args, "-p", p)
	}
	for _, v := range req.Volumes {
		args = append(args, "-v", v)
	}
	for _, e := range req.Env {
		args = append(args, "-e", e)
	}
	args = append(args, req.Image)
	if req.Command != "" {
		args = append(args, req.Command)
	}
	res, err := ExecCommand(shellJoin(args), 120*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// ContainerAction 启停容器
func ContainerAction(id, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("不支持的操作: " + action)
	}
	res, err := ExecCommand(fmt.Sprintf("docker %s %s", action, shellQuote(id)), 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// RemoveContainer 删除容器
func RemoveContainer(id string, force bool) error {
	flag := ""
	if force {
		flag = "-f "
	}
	res, err := ExecCommand(fmt.Sprintf("docker rm %s%s", flag, shellQuote(id)), 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// ContainerLogs 读取容器日志
func ContainerLogs(id string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	res, err := ExecCommand(fmt.Sprintf("docker logs --tail %d %s", lines, shellQuote(id)), 20*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout + res.Stderr), nil
}

// ============ Docker 网络管理 ============

// NetworkInfo 网络信息
type NetworkInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Subnet     string `json:"subnet"`
	Gateway    string `json:"gateway"`
	Containers string `json:"containers"`
}

// ListNetworks 列出 Docker 网络（补充子网/网关/容器数）
func ListNetworks() ([]NetworkInfo, error) {
	res, err := ExecCommand("docker network ls --format '{{.ID}}|{{.Name}}|{{.Driver}}|{{.Scope}}'", 15*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(res.Stderr))
	}
	var list []NetworkInfo
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		ni := NetworkInfo{ID: parts[0], Name: parts[1], Driver: parts[2], Scope: parts[3]}
		ni.fillNetworkDetail()
		list = append(list, ni)
	}
	return list, nil
}

// fillNetworkDetail 通过 inspect 补充子网、网关与容器数
func (ni *NetworkInfo) fillNetworkDetail() {
	cmd := fmt.Sprintf("docker network inspect %s --format '{{range .IPAM.Config}}{{.Subnet}}|{{.Gateway}}{{end}}|{{len .Containers}}'", shellQuote(ni.ID))
	res, err := ExecCommand(cmd, 10*time.Second)
	if err != nil || res.ExitCode != 0 {
		return
	}
	parts := strings.Split(strings.TrimSpace(res.Stdout), "|")
	if len(parts) >= 3 {
		ni.Subnet = parts[0]
		ni.Gateway = parts[1]
		ni.Containers = parts[2]
	}
}

// CreateNetworkReq 创建网络请求
type CreateNetworkReq struct {
	Name     string `json:"name" binding:"required"`
	Driver   string `json:"driver"`    // bridge / overlay / macvlan / host
	Subnet   string `json:"subnet"`    // 如 172.20.0.0/16
	Gateway  string `json:"gateway"`   // 如 172.20.0.1
	IPRange  string `json:"ip_range"`  // 如 172.20.0.2/24
	Internal bool   `json:"internal"`  // 内部网络（不提供外部访问）
}

// CreateNetwork 创建 Docker 网络
func CreateNetwork(req CreateNetworkReq) error {
	if req.Driver == "" {
		req.Driver = "bridge"
	}
	args := []string{"docker", "network", "create", "--driver", req.Driver}
	if req.Subnet != "" {
		args = append(args, "--subnet", req.Subnet)
	}
	if req.Gateway != "" {
		args = append(args, "--gateway", req.Gateway)
	}
	if req.IPRange != "" {
		args = append(args, "--ip-range", req.IPRange)
	}
	if req.Internal {
		args = append(args, "--internal")
	}
	args = append(args, req.Name)
	res, err := ExecCommand(shellJoin(args), 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// RemoveNetwork 删除 Docker 网络
func RemoveNetwork(id string) error {
	res, err := ExecCommand(fmt.Sprintf("docker network rm %s", shellQuote(id)), 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// ============ Docker 应用商店（compose 模板） ============

// DockerAppMeta 容器应用元数据
type DockerAppMeta struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	DefaultPort int      `json:"default_port"`
	Image       string   `json:"image"`
	DependsOn   []string `json:"depends_on"` // 依赖的其它应用 key（如 wordpress 依赖 mysql）
	NeedPwd     bool     `json:"need_pwd"`   // 是否需要设置密码
	EnvTips     string   `json:"env_tips"`   // 环境变量提示
}

var dockerAppMetas = []DockerAppMeta{
	{
		Key: "nginx", Name: "Nginx", Icon: "Cpu",
		Description: "Nginx 官方镜像，独立的 HTTP 服务容器", DefaultPort: 8080,
		Image: "nginx:alpine",
	},
	{
		Key: "mysql", Name: "MySQL", Icon: "Coin",
		Description: "MySQL 8 容器数据库", DefaultPort: 3306,
		Image: "mysql:8", NeedPwd: true, EnvTips: "默认用户 root，密码为设置值",
	},
	{
		Key: "redis", Name: "Redis", Icon: "DataLine",
		Description: "Redis 7 内存缓存容器", DefaultPort: 6379,
		Image: "redis:7", NeedPwd: true, EnvTips: "密码为设置值，默认无密码模式可留空",
	},
	{
		Key: "wordpress", Name: "WordPress", Icon: "EditPen",
		Description: "WordPress 博客 + MySQL 组合（自动建库）", DefaultPort: 8081,
		Image: "wordpress:php8.3-apache", DependsOn: []string{"mysql"}, NeedPwd: true,
		EnvTips: "会同时安装 MySQL 容器并创建 wordpress 库",
	},
	{
		Key: "portainer", Name: "Portainer", Icon: "Platform",
		Description: "Docker 可视化管理面板（Web UI）", DefaultPort: 9443,
		Image: "portainer/portainer-ce:latest",
		EnvTips: "首次访问 https://服务器IP:端口 设置管理员",
	},
	{
		Key: "nextcloud", Name: "Nextcloud", Icon: "Cloudy",
		Description: "自建网盘 / 私有云存储", DefaultPort: 8082,
		Image: "nextcloud:latest",
	},
	{
		Key: "code-server", Name: "Code Server", Icon: "Monitor",
		Description: "浏览器版 VS Code，随时随地写代码", DefaultPort: 8083,
		Image: "codercom/code-server:latest", NeedPwd: true,
		EnvTips: "访问需密码，请设置访问密码",
	},
	{
		Key: "phpmyadmin", Name: "phpMyAdmin", Icon: "Coin",
		Description: "MySQL 网页管理工具（与 MySQL 容器配套）", DefaultPort: 8084,
		Image: "phpmyadmin/phpmyadmin:latest", DependsOn: []string{"mysql"},
		EnvTips: "自动连接同机 MySQL 容器，登录用 mysql 容器的 root 密码",
	},
}

// DockerAppItem 应用条目（元数据 + 安装状态）
type DockerAppItem struct {
	DockerAppMeta
	Status     string  `json:"status"`
	Port       int     `json:"port"`
	Running    bool    `json:"running"`
	Error      string  `json:"error"`
	InstalledAt *time.Time `json:"installed_at"`
}

// ListDockerApps 返回容器应用商店列表
func ListDockerApps() []DockerAppItem {
	items := make([]DockerAppItem, 0, len(dockerAppMetas))
	for _, meta := range dockerAppMetas {
		item := DockerAppItem{DockerAppMeta: meta}
		rec, err := model.GetDockerApp(meta.Key)
		if err == nil {
			item.Status = rec.Status
			item.Port = rec.Port
			item.Error = rec.Error
			item.InstalledAt = rec.InstalledAt
		} else {
			item.Status = "not_installed"
		}
		// 运行状态实时探测
		if item.Status == "installed" {
			item.Running = dockerAppRunning(meta.Key)
		}
		items = append(items, item)
	}
	return items
}

// dockerAppDir 返回应用 compose 目录
func dockerAppDir(key string) string {
	return filepath.Join(config.Get().DataDir, "docker", "apps", key)
}

// dockerComposePath 返回应用 compose 文件路径
func dockerComposePath(key string) string {
	return filepath.Join(dockerAppDir(key), "docker-compose.yml")
}

// dockerAppRunning 探测应用容器是否运行
func dockerAppRunning(key string) bool {
	res, err := ExecCommand(fmt.Sprintf("docker ps --filter name=lp-%s --format '{{.Status}}'", shellQuote(key)), 10*time.Second)
	if err != nil || res.ExitCode != 0 {
		return false
	}
	out := strings.TrimSpace(res.Stdout)
	return out != "" && strings.HasPrefix(out, "Up")
}

var dockerTaskMu sync.Mutex

// InstallDockerAppReq 安装请求
type InstallDockerAppReq struct {
	Key      string `json:"key" binding:"required"`
	Port     int    `json:"port"`     // 对外端口，0 表示用默认
	Password string `json:"password"` // 需要密码的应用
}

// InstallDockerApp 安装容器应用
func InstallDockerApp(req InstallDockerAppReq) error {
	meta, ok := findDockerApp(req.Key)
	if !ok {
		return errors.New("未知应用: " + req.Key)
	}

	dockerTaskMu.Lock()
	defer dockerTaskMu.Unlock()

	rec, _ := model.GetDockerApp(meta.Key)
	// 已安装则跳过（允许带新参数重装时先卸载旧的）
	if rec != nil && rec.Status == "installed" && dockerAppRunning(meta.Key) {
		return errors.New("应用已安装且正在运行，如需调整请先卸载")
	}

	port := req.Port
	if port == 0 {
		port = meta.DefaultPort
	}
	pwd := req.Password

	// 创建 compose 目录与文件
	dir := dockerAppDir(meta.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("创建应用目录失败: " + err.Error())
	}

	compose := renderDockerCompose(meta, port, pwd)
	if err := os.WriteFile(dockerComposePath(meta.Key), []byte(compose), 0o644); err != nil {
		return errors.New("写入 compose 文件失败: " + err.Error())
	}

	// 记录安装中
	if rec == nil {
		rec = &model.DockerApp{Key: meta.Key, Name: meta.Name, Status: "installing", Port: port}
	} else {
		rec.Status = "installing"
		rec.Port = port
		rec.Error = ""
	}
	_ = model.SaveDockerApp(rec)

	go func() {
		res, err := ExecCommand(fmt.Sprintf("docker compose -f %s up -d --remove-orphans", shellQuote(dockerComposePath(meta.Key))), 10*time.Minute)
		rec2, _ := model.GetDockerApp(meta.Key)
		if err != nil || res.ExitCode != 0 {
			msg := ""
			if err != nil {
				msg = err.Error()
			} else {
				msg = strings.TrimSpace(res.Stderr)
			}
			rec2.Status = "failed"
			rec2.Error = msg
		} else {
			rec2.Status = "installed"
			rec2.Error = ""
			now := time.Now()
			rec2.InstalledAt = &now
		}
		_ = model.SaveDockerApp(rec2)
	}()
	return nil
}

// UninstallDockerApp 卸载容器应用
func UninstallDockerApp(key string) error {
	meta, ok := findDockerApp(key)
	if !ok {
		return errors.New("未知应用: " + key)
	}
	rec, err := model.GetDockerApp(meta.Key)
	if err != nil || rec.Status == "not_installed" {
		return errors.New("应用尚未安装")
	}

	dockerTaskMu.Lock()
	defer dockerTaskMu.Unlock()

	// 卸载依赖应用：如果其它应用依赖本应用（如 wordpress 依赖 mysql），一并处理
	for _, m := range dockerAppMetas {
		for _, d := range m.DependsOn {
			if d == meta.Key {
				_ = dockerComposeDown(m.Key)
				r2, _ := model.GetDockerApp(m.Key)
				if r2 != nil {
					r2.Status = "not_installed"
					r2.InstalledAt = nil
					_ = model.SaveDockerApp(r2)
				}
			}
		}
	}

	if err := dockerComposeDown(meta.Key); err != nil {
		return err
	}
	rec.Status = "not_installed"
	rec.InstalledAt = nil
	rec.Error = ""
	_ = model.SaveDockerApp(rec)
	return nil
}

func dockerComposeDown(key string) error {
	path := dockerComposePath(key)
	if _, err := os.Stat(path); err != nil {
		return nil // 没有 compose 文件，直接跳过
	}
	res, err := ExecCommand(fmt.Sprintf("docker compose -f %s down -v", shellQuote(path)), 5*time.Minute)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	_ = os.RemoveAll(dockerAppDir(key))
	return nil
}

func findDockerApp(key string) (DockerAppMeta, bool) {
	for _, m := range dockerAppMetas {
		if m.Key == key {
			return m, true
		}
	}
	return DockerAppMeta{}, false
}

// renderDockerCompose 渲染 compose 文件
func renderDockerCompose(meta DockerAppMeta, port int, pwd string) string {
	containerName := "lp-" + meta.Key
	host := "127.0.0.1"
	_ = host

	var sb strings.Builder
	sb.WriteString("services:\n")
	switch meta.Key {
	case "mysql":
		sb.WriteString("  mysql:\n")
		sb.WriteString("    image: mysql:8\n")
		sb.WriteString("    container_name: lp-mysql\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      MYSQL_ROOT_PASSWORD: %q\n", pwd))
		sb.WriteString("      MYSQL_DATABASE: wordpress\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - mysql_data:/var/lib/mysql\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:3306\"\n", port))
		sb.WriteString("volumes:\n  mysql_data:\n")
	case "wordpress":
		sb.WriteString("  mysql:\n")
		sb.WriteString("    image: mysql:8\n")
		sb.WriteString("    container_name: lp-mysql\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      MYSQL_ROOT_PASSWORD: %q\n", pwd))
		sb.WriteString("      MYSQL_DATABASE: wordpress\n")
		sb.WriteString("      MYSQL_USER: wpuser\n")
		sb.WriteString(fmt.Sprintf("      MYSQL_PASSWORD: %q\n", pwd))
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - mysql_data:/var/lib/mysql\n")
		sb.WriteString("    healthcheck:\n")
		sb.WriteString("      test: [\"CMD\", \"mysqladmin\", \"ping\", \"-h\", \"localhost\"]\n")
		sb.WriteString("      interval: 5s\n")
		sb.WriteString("      retries: 20\n")
		sb.WriteString("  wordpress:\n")
		sb.WriteString("    image: wordpress:php8.3-apache\n")
		sb.WriteString("    container_name: lp-wordpress\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    depends_on:\n")
		sb.WriteString("      mysql:\n")
		sb.WriteString("        condition: service_healthy\n")
		sb.WriteString("    environment:\n")
		sb.WriteString("      WORDPRESS_DB_HOST: mysql:3306\n")
		sb.WriteString("      WORDPRESS_DB_USER: wpuser\n")
		sb.WriteString(fmt.Sprintf("      WORDPRESS_DB_PASSWORD: %q\n", pwd))
		sb.WriteString("      WORDPRESS_DB_NAME: wordpress\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - wp_data:/var/www/html\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:80\"\n", port))
		sb.WriteString("volumes:\n  mysql_data:\n  wp_data:\n")
	case "redis":
		sb.WriteString("  redis:\n")
		sb.WriteString("    image: redis:7\n")
		sb.WriteString("    container_name: lp-redis\n")
		sb.WriteString("    restart: unless-stopped\n")
		if pwd != "" {
			sb.WriteString("    command: redis-server --requirepass " + shellQuote(pwd) + "\n")
		}
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - redis_data:/data\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:6379\"\n", port))
		sb.WriteString("volumes:\n  redis_data:\n")
	case "portainer":
		sb.WriteString("  portainer:\n")
		sb.WriteString("    image: portainer/portainer-ce:latest\n")
		sb.WriteString("    container_name: lp-portainer\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    command: -H unix:///var/run/docker.sock\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - /var/run/docker.sock:/var/run/docker.sock\n")
		sb.WriteString("      - portainer_data:/data\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:9443\"\n", port))
		sb.WriteString("volumes:\n  portainer_data:\n")
	case "nextcloud":
		sb.WriteString("  nextcloud:\n")
		sb.WriteString("    image: nextcloud:latest\n")
		sb.WriteString("    container_name: lp-nextcloud\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - nc_data:/var/www/html\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:80\"\n", port))
		sb.WriteString("volumes:\n  nc_data:\n")
	case "code-server":
		sb.WriteString("  code-server:\n")
		sb.WriteString("    image: codercom/code-server:latest\n")
		sb.WriteString("    container_name: lp-codeserver\n")
		sb.WriteString("    restart: unless-stopped\n")
		if pwd != "" {
			sb.WriteString(fmt.Sprintf("    command: --auth password --bind-addr 0.0.0.0:8080\n"))
			sb.WriteString("    environment:\n")
			sb.WriteString(fmt.Sprintf("      PASSWORD: %q\n", pwd))
		}
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - code_data:/home/coder\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:8080\"\n", port))
		sb.WriteString("volumes:\n  code_data:\n")
	case "phpmyadmin":
		sb.WriteString("  phpmyadmin:\n")
		sb.WriteString("    image: phpmyadmin/phpmyadmin:latest\n")
		sb.WriteString("    container_name: lp-phpmyadmin\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    environment:\n")
		sb.WriteString("      PMA_HOST: mysql\n")
		sb.WriteString("      PMA_PORT: 3306\n")
		sb.WriteString("      UPLOAD_LIMIT: 128M\n")
		sb.WriteString("    depends_on:\n")
		sb.WriteString("      - mysql\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:80\"\n", port))
		sb.WriteString("volumes:\n")
	default: // nginx 及其它：通用模板
		sb.WriteString(fmt.Sprintf("  %s:\n", meta.Key))
		sb.WriteString(fmt.Sprintf("    image: %s\n", meta.Image))
		sb.WriteString(fmt.Sprintf("    container_name: %s\n", containerName))
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - ./html:/usr/share/nginx/html:ro\n")
		sb.WriteString(fmt.Sprintf("    ports:\n      - \"%d:80\"\n", port))
	}
	return sb.String()
}
