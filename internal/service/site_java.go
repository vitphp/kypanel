package service

import (
	"archive/zip"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// JavaArtifactInfo 从 jar 包中读出的部署元信息（用于「少填字段」的自动推断）。
type JavaArtifactInfo struct {
	MainClass  string // MANIFEST: Main-Class
	StartClass string // MANIFEST: Start-Class（Spring Boot）
	Version    string // MANIFEST: Spring-Boot-Version
	SpringBoot bool   // 是否为 Spring Boot 可执行包
	Port       int    // 内嵌配置声明的服务端口（application.properties/yml），0=未声明
}

// Runnable 判断该 jar 是否带入口（可 java -jar 直接运行）。
func (i JavaArtifactInfo) Runnable() bool { return strings.TrimSpace(i.MainClass) != "" }

// InspectJavaArtifact 读取 jar 的清单与内嵌配置，推断部署所需信息。
// 全部「尽力而为」：读不到信息不算失败，由默认值/用户填写兜底，绝不阻断创建。
func InspectJavaArtifact(path string) JavaArtifactInfo {
	info := JavaArtifactInfo{}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return info
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != "META-INF/MANIFEST.MF" {
			continue
		}
		data := readZipEntry(f)
		info.MainClass = manifestValue(data, "Main-Class")
		info.StartClass = manifestValue(data, "Start-Class")
		info.Version = manifestValue(data, "Spring-Boot-Version")
		// Spring Boot fat jar 的 Main-Class 是 JarLauncher，真正的入口在 Start-Class
		if info.StartClass != "" || strings.Contains(info.MainClass, "springframework.boot.loader") {
			info.SpringBoot = true
		}
		break
	}

	// 端口推断：Spring Boot fat jar 把配置放在 BOOT-INF/classes/ 下
	for _, f := range zr.File {
		switch f.Name {
		case "BOOT-INF/classes/application.properties", "application.properties":
			if p := parseServerPortProperties(readZipEntry(f)); p > 0 {
				info.Port = p
			}
		case "BOOT-INF/classes/application.yml", "BOOT-INF/classes/application.yaml",
			"application.yml", "application.yaml":
			if p := parseServerPortYAML(readZipEntry(f)); p > 0 {
				info.Port = p
			}
		}
	}
	return info
}

// readZipEntry 读取 zip 内单个文件的文本内容（限制大小，避免解压炸弹）。
func readZipEntry(f *zip.File) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return ""
	}
	return string(b)
}

// manifestValue 读取 MANIFEST.MF 中某个头的值（支持 72 字节折叠续行）。
func manifestValue(manifest, key string) string {
	lines := strings.Split(strings.ReplaceAll(manifest, "\r\n", "\n"), "\n")
	prefix := key + ":"
	var val strings.Builder
	found := false
	for _, ln := range lines {
		if !found {
			if strings.HasPrefix(ln, prefix) {
				val.WriteString(strings.TrimSpace(strings.TrimPrefix(ln, prefix)))
				found = true
			}
			continue
		}
		// 续行以单个空格开头
		if strings.HasPrefix(ln, " ") {
			val.WriteString(strings.TrimSpace(ln))
			continue
		}
		break
	}
	return strings.TrimSpace(val.String())
}

// parseServerPortProperties 从 application.properties 提取 server.port。
func parseServerPortProperties(content string) int {
	for _, ln := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "!") {
			continue
		}
		kv := strings.SplitN(ln, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) != "server.port" {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(kv[1])); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return 0
}

// parseServerPortYAML 从 application.yml 提取 server.port（兼容 `server:\n  port: 8080`
// 与 `server.port: 8080` 两种写法；不引入 YAML 依赖，只做缩进扫描）。
func parseServerPortYAML(content string) int {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inServer := false
	serverIndent := -1
	for _, raw := range lines {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		ln := strings.TrimSpace(raw)

		// 单行写法：server.port: 8080
		if strings.HasPrefix(ln, "server.port:") {
			if p := yamlPort(ln); p > 0 {
				return p
			}
		}
		// 块写法：server: 之后紧跟缩进的 port:
		if ln == "server:" || strings.HasPrefix(ln, "server:") {
			if strings.HasPrefix(ln, "server:") && strings.Contains(ln, "port:") {
				if p := yamlPort(ln); p > 0 {
					return p
				}
			}
			inServer = true
			serverIndent = indent
			continue
		}
		if !inServer {
			continue
		}
		// 离开 server 块（同级或更外层的新键）
		if indent <= serverIndent {
			inServer = false
			continue
		}
		if strings.HasPrefix(ln, "port:") {
			if p := yamlPort(ln); p > 0 {
				return p
			}
		}
	}
	return 0
}

// yamlPort 解析 `...port: 8080` 末尾的端口号。
func yamlPort(line string) int {
	idx := strings.LastIndex(line, "port:")
	if idx < 0 {
		return 0
	}
	v := strings.TrimSpace(line[idx+len("port:"):])
	v = strings.Trim(v, `"'`)
	if sp := strings.IndexAny(v, " #"); sp > 0 {
		v = v[:sp]
	}
	if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
		return p
	}
	return 0
}

// isPortBindable 探测端口在 127.0.0.1 上是否可绑定（用于判断端口是否空闲）。
func isPortBindable(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// JarMainHint 生成「启动命令是否需要用户补全」的提示，供创建时返回告警。
func JarMainHint(root, jarFile string) string {
	if jarFile == "" {
		return ""
	}
	info := InspectJavaArtifact(filepath.Join(root, jarFile))
	if info.Runnable() {
		return ""
	}
	return "未在该 jar 的清单里找到主类（Main-Class/Start-Class），可能不是可执行包；请确认启动命令是否正确"
}

// jarFileExists 判断项目目录下是否存在该 jar（相对路径，含子目录）。
func jarFileExists(root, jarFile string) bool {
	if jarFile == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, jarFile))
	return err == nil && !info.IsDir()
}
