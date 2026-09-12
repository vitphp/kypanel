package service

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"kypanel/internal/model"
)

// Go 站点源码上传与部署：把上传的压缩包/二进制文件落到项目目录。

const siteUploadDir = "/www/.lp-upload"

// SaveUploadedSource 把上传的源码流式写入临时目录，返回临时文件路径。
func SaveUploadedSource(r io.Reader, filename string) (string, error) {
	if err := os.MkdirAll(siteUploadDir, 0o755); err != nil {
		return "", err
	}
	base := sanitizeUploadName(filename)
	if base == "" {
		base = "source"
	}
	tmp := filepath.Join(siteUploadDir, time.Now().Format("20060102150405")+"_"+randHex(4)+"_"+base)
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// sanitizeUploadName 只保留文件名部分，去掉路径分隔符与危险字符。
func sanitizeUploadName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return -1
		}
		return r
	}, name)
	return strings.TrimSpace(name)
}

// IsArchive 判断是否为支持的压缩包（zip）。
func IsArchive(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".zip")
}

// DeployJavaArtifact 把上传的 Java 构建产物部署到项目目录，返回要运行的 jar 文件名（相对项目目录）。
// 支持两种上传方式：
//  1. 直接上传 .jar（Spring Boot fat jar 最常见）；
//  2. 上传 .zip（内含 jar，常见于「jar + 配置文件/依赖目录」一起打包的场景）。
//
// war 包不在此处理：war 需要 Servlet 容器（Tomcat），应使用「反向代理」站点指向 Tomcat。
func DeployJavaArtifact(tmpPath, filename, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", errors.New("创建项目目录失败: " + err.Error())
	}
	cleanDest, err := SanitizePath(destDir)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(lower, ".war"):
		return "", errors.New("Java 站点运行的是可执行 jar（如 Spring Boot fat jar）；war 包请改用「反向代理」站点指向 Tomcat")
	case strings.HasSuffix(lower, ".jar"):
		name := sanitizeUploadName(filename)
		if name == "" {
			name = "app.jar"
		}
		if err := copyFile(tmpPath, filepath.Join(cleanDest, name)); err != nil {
			return "", errors.New("保存 jar 失败: " + err.Error())
		}
		return name, nil
	case IsArchive(filename):
		if err := UnzipFile(tmpPath, cleanDest); err != nil {
			return "", errors.New("解压失败: " + err.Error())
		}
		jars := ScanJarFiles(cleanDest)
		if len(jars) == 0 {
			return "", errors.New("压缩包中没有找到 .jar 文件")
		}
		if len(jars) == 1 {
			return jars[0], nil
		}
		// 多个 jar：排除 sources/javadoc，取体积最大的（通常就是主程序包）
		best := ""
		var bestSize int64 = -1
		for _, rel := range jars {
			base := strings.ToLower(filepath.Base(rel))
			if strings.Contains(base, "sources") || strings.Contains(base, "javadoc") {
				continue
			}
			if fi, statErr := os.Stat(filepath.Join(cleanDest, rel)); statErr == nil && fi.Size() > bestSize {
				bestSize = fi.Size()
				best = rel
			}
		}
		if best == "" {
			return "", errors.New("压缩包中的 jar 均为 sources/javadoc，未找到可运行的主程序 jar")
		}
		return best, nil
	}
	return "", errors.New("仅支持上传 .jar，或内含 jar 的 .zip 文件")
}

// ScanJarFiles 扫描目录（根 + 一层子目录）中的 .jar 文件，返回相对路径（排序后）。
func ScanJarFiles(dir string) []string {
	var out []string
	scanOne := func(d, prefix string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
				out = append(out, filepath.Join(prefix, e.Name()))
			}
		}
	}
	scanOne(dir, "")
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				scanOne(filepath.Join(dir, e.Name()), e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// ScanExecutables 扫描目录（根 + 一层子目录）中的可执行文件。
func ScanExecutables(dir string) []model.ExecFile {
	out := []model.ExecFile{}
	clean, err := SanitizePath(dir)
	if err != nil {
		return out
	}
	scanOne := func(d string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if !isExecutable(info) {
				continue
			}
			rel, _ := filepath.Rel(clean, filepath.Join(d, e.Name()))
			out = append(out, model.ExecFile{Path: rel, Name: e.Name(), Size: info.Size()})
		}
	}
	scanOne(clean)
	// 一层子目录
	if entries, err := os.ReadDir(clean); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				scanOne(filepath.Join(clean, e.Name()))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// isExecutable 判断文件是否具有可执行权限（且非目录）。
func isExecutable(info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// DeploySiteSource 把临时源码部署到项目目录：
//   - zip → 解压到 destDir；解压后若无二进制但检测到 Go 源码(go.mod)，自动编译出二进制
//   - 单文件 → 复制到 destDir 并加执行权限
//
// outputName 为源码编译时的输出二进制名（一般为网站名称）；buildName 为构建产物默认名。
// runtimeVersion 为站点所选运行环境版本（Go 1.x），用于定位对应 Go 工具链。
//
// 返回部署后项目目录中的可执行文件列表。
func DeploySiteSource(tmpPath, filename, destDir, runtimeVersion, outputName string) ([]model.ExecFile, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, errors.New("创建项目目录失败: " + err.Error())
	}
	cleanDest, err := SanitizePath(destDir)
	if err != nil {
		return nil, err
	}
	if IsArchive(filename) {
		if err := UnzipFile(tmpPath, cleanDest); err != nil {
			return nil, errors.New("解压失败: " + err.Error())
		}
		// 解压后如果没有任何可执行文件，但存在 Go 源码（go.mod），则自动编译
		if len(ScanExecutables(cleanDest)) == 0 && findGoModuleRoot(cleanDest) != "" {
			if err := goBuildSite(cleanDest, runtimeVersion, outputName); err != nil {
				return nil, err
			}
		}
	} else {
		base := sanitizeUploadName(filename)
		if base == "" {
			return nil, errors.New("文件名无效")
		}
		target := filepath.Join(cleanDest, base)
		if err := copyFile(tmpPath, target); err != nil {
			return nil, errors.New("写入文件失败: " + err.Error())
		}
		_ = os.Chmod(target, 0o755)
	}
	_ = ChownToWebUser(cleanDest, true)
	return ScanExecutables(cleanDest), nil
}

// findGoModuleRoot 在目录（根 + 一层子目录）中查找 go.mod，返回其所在目录；找不到返回空。
func findGoModuleRoot(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return dir
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(sub, "go.mod")); err == nil {
				return sub
			}
		}
	}
	return ""
}

// goBuildSite 用指定 Go 版本把 Go 源码编译成二进制，输出到 moduleRoot 目录。
//   - 编译目标平台 = 当前服务器系统（runtime.GOOS/GOARCH），保证产物与服务器一致
//   - 输出名默认 outputName（网站名），为空时回退 "app"
//   - 编译失败返回带 stderr 的错误，供前端展示
func goBuildSite(moduleRoot, runtimeVersion, outputName string) error {
	bin := findRuntimeBinDir(model.SiteTypeGo, normalizeRuntimeVersion(runtimeVersion))
	goBin := "go"
	env := os.Environ()
	if bin != "" {
		goBin = filepath.Join(bin, "go")
		// GOROOT = bin 目录的上一级（如 /usr/local/go1.24）
		env = append(env, "GOROOT="+filepath.Dir(bin))
	}
	if _, err := exec.LookPath(goBin); err != nil {
		if _, err2 := exec.LookPath("go"); err2 != nil {
			return errors.New("未检测到 go，请先在「应用商店」安装 Golang 环境")
		}
		goBin = "go"
	}

	out := sanitizeUploadName(outputName)
	if out == "" {
		out = "app"
	}
	// 目标平台固定为当前服务器系统架构
	env = append(env,
		"CGO_ENABLED=0",
		"GOOS="+runtime.GOOS,
		"GOARCH="+runtime.GOARCH,
		"GOPROXY=https://goproxy.cn,direct",
		"GOFLAGS=-mod=mod",
	)
	cmd := exec.Command(goBin, "build", "-o", out, ".")
	cmd.Dir = moduleRoot
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return errors.New("源码编译失败：" + msg)
	}
	target := filepath.Join(moduleRoot, out)
	if _, err := os.Stat(target); err != nil {
		return errors.New("源码编译完成但未找到产物 " + out)
	}
	_ = os.Chmod(target, 0o755)
	return nil
}

// CleanupUpload 删除上传的临时文件。
func CleanupUpload(tmpPath string) {
	if tmpPath == "" {
		return
	}
	if strings.HasPrefix(tmpPath, siteUploadDir) {
		_ = os.Remove(tmpPath)
	}
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
