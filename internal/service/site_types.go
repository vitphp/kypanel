package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 站点类型注册表。
//
// 背景：站点类型的差异原本散落在 isRuntimeSite / ensureRuntime / runtimeVersionOf /
// findRuntimeBinDir / siteRuntimePrelude / effectiveStartCommand / CreateSite 的 switch，
// 以及前端 Website.vue、SiteSettings.vue 里多处 includes([...]) 列表——新增一种类型要改
// 十几处，且极易漏改（漏一处就出现"能创建但起不来"或"列表不显示启停按钮"）。
//
// 这里把「类型的运行时特性」全部收敛成数据：进程型站点（由 systemd 守护 + 反代本地端口）
// 只需在 siteTypeSpecs 加一项、前端 SITE_TYPE_FORMS 加一项即可，主流程不用改。
type siteTypeSpec struct {
	Key   string // 站点类型标识
	Label string // 展示名，同时作为版本号前缀（如 "Java 21.0"）
	// BinName PATH 中的可执行文件名：未指定版本时的兜底校验对象
	BinName string
	// VersionCmd 版本探测命令（stdout+stderr 合并解析）
	VersionCmd string
	// VersionRe 从探测输出中提取版本的正则，需含 1 个捕获组
	VersionRe string
	// BinGlobs 按版本定位 bin 目录的 glob 模板，支持占位符：
	//   {v}=主.次版本（如 17.0 / 3.13）  {major}=主版本（如 17 / 3）
	//   {home}=家目录候选（$HOME、/root、/）
	// 顺序敏感：靠前的模板优先命中
	BinGlobs []string
	// EnvKey 需要导出的环境变量名，取值为 bin 的上一级目录（如 JAVA_HOME / GOROOT）
	EnvKey string
	// InstallHint 未安装时的引导文案（拼在"未检测到 xxx"之后）
	InstallHint string
	// DefaultRun 生成默认启动命令；返回空串表示必须由用户填写
	DefaultRun func(s *model.Site) string
	// NeedJar 该类型以「上传 jar」为主要部署形态（影响创建时的必填校验）
	NeedJar bool
}

// siteTypeSpecs 进程型站点注册表（静态/PHP/反向代理不在表内：它们不需要进程守护）。
var siteTypeSpecs = map[string]siteTypeSpec{
	model.SiteTypeNode: {
		Key:         model.SiteTypeNode,
		Label:       "Node",
		BinName:     "node",
		VersionCmd:  "node -v",
		VersionRe:   `v?(\d+\.\d+)`,
		BinGlobs:    []string{"{home}/.nvm/versions/node/v{v}.*/bin", "/usr/local/node{major}*/bin"},
		InstallHint: "请先在「应用商店」安装 Node.js 环境",
	},
	model.SiteTypePython: {
		Key:        model.SiteTypePython,
		Label:      "Python",
		BinName:    "python3",
		VersionCmd: "python3 --version",
		VersionRe:  `(\d+\.\d+)`,
		// pyenv 目录名是完整补丁号（3.13.15），必须用 glob 模糊匹配
		BinGlobs:    []string{"{home}/.pyenv/versions/{v}.*/bin", "/usr/local/python{major}*/bin"},
		InstallHint: "请先在「应用商店」安装 Python 环境",
	},
	model.SiteTypeGo: {
		Key:         model.SiteTypeGo,
		Label:       "Go",
		BinName:     "go",
		VersionCmd:  "go version",
		VersionRe:   `go(\d+\.\d+)`,
		BinGlobs:    []string{"/usr/local/go{v}*/bin"},
		EnvKey:      "GOROOT",
		InstallHint: "请先在「应用商店」安装 Golang 环境",
	},
	model.SiteTypeJava: {
		Key:        model.SiteTypeJava,
		Label:      "Java",
		BinName:    "java",
		VersionCmd: "java -version",
		// java -version 的版本在引号里且输出到 stderr（探测时会合并两路输出）
		VersionRe: `version\s+"([^"]+)"`,
		// JDK 安装位置因发行版/包名而异：Debian/Ubuntu 是 java-17-openjdk-amd64，
		// RHEL 系是 java-17-openjdk-17.0.x，手动安装常见 jdk-17*/opt/jdk17*
		BinGlobs: []string{
			"/usr/lib/jvm/java-{v}*/bin",
			"/usr/lib/jvm/java-{major}*/bin",
			"/usr/lib/jvm/*jdk*{major}*/bin",
			"/usr/lib/jvm/*-{v}*/bin",
			"/opt/jdk{major}*/bin",
			"/opt/java{major}*/bin",
		},
		EnvKey:      "JAVA_HOME",
		InstallHint: "请先在「应用商店」安装 Java (JDK) 环境",
		NeedJar:     true,
		DefaultRun:  javaDefaultRun,
	},
}

// siteSpec 取站点类型规格。
func siteSpec(t string) (siteTypeSpec, bool) {
	spec, ok := siteTypeSpecs[t]
	return spec, ok
}

// runtimeSiteTypes 全部进程型站点类型（供前端/迁移等只读场景使用）。
func runtimeSiteTypes() []string {
	out := make([]string, 0, len(siteTypeSpecs))
	for k := range siteTypeSpecs {
		out = append(out, k)
	}
	return out
}

// ensureRuntime 校验运行环境已安装（支持多版本）。
// runtimeVersion 格式：Python 3.12 / Node 20 / Go 1.23 / Java 17.0
func ensureRuntime(t string, runtimeVersion string) error {
	if t == model.SiteTypePHP {
		return ensurePhpRuntime(runtimeVersion)
	}
	spec, ok := siteSpec(t)
	if !ok {
		return nil
	}
	if runtimeVersion != "" && findRuntimeBinDir(t, normalizeRuntimeVersion(runtimeVersion)) != "" {
		return nil
	}
	if _, err := exec.LookPath(spec.BinName); err != nil {
		return errors.New("未检测到 " + spec.BinName + "，" + spec.InstallHint)
	}
	return nil
}

// ensurePhpRuntime 校验 PHP 运行环境（多版本共存时按版本选择二进制名）
func ensurePhpRuntime(runtimeVersion string) error {
	bin := "php"
	if runtimeVersion != "" {
		// 尝试 php8.2 等版本特定二进制（兼容 "PHP 8.2.33" 完整版本号，归约为主.次版本）
		v := phpMinorVersion(runtimeVersion)
		if v == "" {
			v = strings.TrimPrefix(runtimeVersion, "PHP ")
		}
		if v != "" && v != runtimeVersion {
			bin = "php" + v
		}
	}
	if _, err := exec.LookPath(bin); err != nil {
		return errors.New("未检测到 " + bin + "，请先在「应用商店」安装对应 PHP 版本")
	}
	return nil
}

// runtimeVersionOf 探测本机已安装的运行时版本，输出统一归一化为「Label 主.次版本」
// （如 "Python 3.13"、"Node 20.19"、"Java 21.0"），保证与站点保存的版本串格式一致。
func runtimeVersionOf(t string) string {
	if t == model.SiteTypePHP {
		return phpVersionOf()
	}
	spec, ok := siteSpec(t)
	if !ok {
		return ""
	}
	res, err := ExecCommand(spec.VersionCmd, 20*time.Second)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	// java -version 只写 stderr、node -v 只写 stdout：统一合并两路，避免漏解析
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	raw := out
	if spec.VersionRe != "" {
		m := regexp.MustCompile(spec.VersionRe).FindStringSubmatch(out)
		if len(m) < 2 {
			return ""
		}
		raw = m[1]
	}
	if v := normalizeRuntimeVersion(raw); v != "" {
		return spec.Label + " " + v
	}
	return ""
}

// phpVersionOf 探测已安装的 PHP 版本（输出 "PHP 8.2"）
func phpVersionOf() string {
	res, err := ExecCommand("php -v", 15*time.Second)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) >= 2 && strings.HasPrefix(fields[0], "PHP") {
		if v := normalizeRuntimeVersion(fields[1]); v != "" {
			return "PHP " + v
		}
	}
	return ""
}

// normalizeRuntimeVersion 从任意格式的版本描述中提取主.次版本号（如 "20.19"）。
// 兼容 "Node v20.19.2"、"go1.24.4"、"Python 3.13.5"、"PHP 8.2.33"、"Java 1.8.0_371" 等写法。
func normalizeRuntimeVersion(raw string) string {
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) >= 3 {
		return m[1] + "." + m[2]
	}
	return strings.TrimSpace(raw)
}

// findRuntimeBinDir 定位指定类型、主.次版本运行时的 bin 目录（供 PATH 注入 / 环境校验）。
// 各类型的安装位置差异全部写在注册表的 BinGlobs 里，这里只做占位符展开与命中校验。
//
// 家目录候选包含 $HOME 与 /root：面板以 systemd 服务运行时曾存在未注入 HOME 的历史部署，
// 导致 pyenv 装到了 /.pyenv，这里一并兜底兼容。
func findRuntimeBinDir(t, v string) string {
	spec, ok := siteSpec(t)
	if !ok || v == "" {
		return ""
	}
	major := strings.SplitN(v, ".", 2)[0]
	homes := make([]string, 0, 3)
	if h := os.Getenv("HOME"); h != "" {
		homes = append(homes, h)
	}
	homes = append(homes, "/root", "/")
	for _, tpl := range spec.BinGlobs {
		if strings.Contains(tpl, "{home}") {
			for _, h := range homes {
				if dir := matchBinGlob(tpl, spec.BinName, h, v, major); dir != "" {
					return dir
				}
			}
			continue
		}
		if dir := matchBinGlob(tpl, spec.BinName, "", v, major); dir != "" {
			return dir
		}
	}
	return ""
}

// matchBinGlob 展开单个 glob 模板并返回首个命中且含目标可执行文件的目录。
func matchBinGlob(tpl, binName, home, v, major string) string {
	p := strings.ReplaceAll(tpl, "{home}", home)
	p = strings.ReplaceAll(p, "{major}", major)
	p = strings.ReplaceAll(p, "{v}", v)
	matches, _ := filepath.Glob(p)
	for _, m := range matches {
		if binName == "" {
			return m
		}
		if _, err := os.Stat(filepath.Join(m, binName)); err == nil {
			return m
		}
	}
	return ""
}

// ===== Java 站点默认启动命令 =====

// javaDefaultRun 生成 Java 站点默认启动命令：java <JVM参数> -jar <jar> --server.port=<端口>。
// 非 Spring Boot 项目请自行填写启动命令（会原样优先使用）。
func javaDefaultRun(s *model.Site) string {
	jar := strings.TrimSpace(s.JarFile)
	if jar == "" {
		return ""
	}
	parts := []string{"java"}
	if args := strings.TrimSpace(s.JvmArgs); args != "" {
		parts = append(parts, args)
	}
	// -jar 必须紧跟 jar 路径；应用参数放最后
	parts = append(parts, "-jar", shellQuote(jar))
	if s.ProxyPort > 0 {
		parts = append(parts, fmt.Sprintf("--server.port=%d", s.ProxyPort))
	}
	return strings.Join(parts, " ")
}

// ===== 端口自动分配（用户不必理解"应用端口"概念）=====

// sitePortStart 自动分配的起始端口（避开 8080/3000 等常用端口，减少与外部服务冲突）
const sitePortStart = 18090

// AllocateSitePort 为进程型站点自动分配一个空闲端口：
// 跳过其它站点已占用的监听端口/应用端口，并实际探测本机是否可绑定。
func AllocateSitePort() int {
	used := map[int]bool{}
	if sites, err := model.ListSites(); err == nil {
		for _, s := range sites {
			if s.Port > 0 {
				used[s.Port] = true
			}
			if s.ProxyPort > 0 {
				used[s.ProxyPort] = true
			}
		}
	}
	for p := sitePortStart; p < sitePortStart+2000; p++ {
		if used[p] {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return p
	}
	return 0
}
