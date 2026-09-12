package service

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kypanel/internal/model"
)

// TestEffectiveStartCommandJava Java 站点启动命令：留空时按 jar + JVM 参数 + 端口自动拼装。
func TestEffectiveStartCommandJava(t *testing.T) {
	// 1) 自动拼装
	s := &model.Site{
		Type:      model.SiteTypeJava,
		JarFile:   "app.jar",
		JvmArgs:   "-Xmx512m -Duser.timezone=GMT+08",
		ProxyPort: 18090,
	}
	got := effectiveStartCommand(s)
	want := "java -Xmx512m -Duser.timezone=GMT+08 -jar 'app.jar' --server.port=18090"
	if got != want {
		t.Fatalf("自动拼装命令 = %q, want %q", got, want)
	}

	// 2) 没有 JVM 参数时也要能拼装
	s2 := &model.Site{Type: model.SiteTypeJava, JarFile: "a.jar", ProxyPort: 8081}
	if got := effectiveStartCommand(s2); got != "java -jar 'a.jar' --server.port=8081" {
		t.Fatalf("无 JVM 参数拼装 = %q", got)
	}

	// 3) 自定义启动命令优先（非 Spring Boot 项目场景）
	s3 := &model.Site{Type: model.SiteTypeJava, JarFile: "a.jar", StartCommand: "java -cp . Main"}
	if got := effectiveStartCommand(s3); got != "java -cp . Main" {
		t.Fatalf("自定义命令应优先，实际 %q", got)
	}

	// 4) 缺 jar 且无自定义命令 → 空（调用方据此判定"未就绪"）
	s4 := &model.Site{Type: model.SiteTypeJava, ProxyPort: 8080}
	if got := effectiveStartCommand(s4); got != "" {
		t.Fatalf("缺 jar 应返回空，实际 %q", got)
	}

	// 5) 非 Java 类型不会被自动拼装
	s5 := &model.Site{Type: model.SiteTypeNode, JarFile: "a.jar", ProxyPort: 3000}
	if got := effectiveStartCommand(s5); got != "" {
		t.Fatalf("非 Java 类型应为空，实际 %q", got)
	}
}

// TestIsRuntimeSiteIncludesJava Java 必须被当作进程型站点（才会生成 systemd 守护）。
func TestIsRuntimeSiteIncludesJava(t *testing.T) {
	for _, ty := range []string{model.SiteTypeNode, model.SiteTypePython, model.SiteTypeGo, model.SiteTypeJava} {
		if !isRuntimeSite(ty) {
			t.Fatalf("%s 应为进程型站点", ty)
		}
	}
	for _, ty := range []string{model.SiteTypeStatic, model.SiteTypePHP, model.SiteTypeProxy} {
		if isRuntimeSite(ty) {
			t.Fatalf("%s 不应为进程型站点", ty)
		}
	}
}

// makeZip 生成一个 zip 包（内含指定文件），用于测试 DeployJavaArtifact 的解压分支。
func makeZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, "pkg.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

// TestDeployJavaArtifact 覆盖：直接上传 jar、zip 内含 jar、war 拒绝、非法文件拒绝、多 jar 取主程序。
func TestDeployJavaArtifact(t *testing.T) {
	base := t.TempDir()

	// 1) 直接上传 jar
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	jarPath := filepath.Join(src, "myapp.jar")
	if err := os.WriteFile(jarPath, []byte("fake-jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest1 := filepath.Join(base, "site1")
	name, err := DeployJavaArtifact(jarPath, "myapp.jar", dest1)
	if err != nil {
		t.Fatalf("上传 jar 失败: %v", err)
	}
	if name != "myapp.jar" {
		t.Fatalf("jar 文件名 = %q", name)
	}
	if _, err := os.Stat(filepath.Join(dest1, "myapp.jar")); err != nil {
		t.Fatalf("jar 未落盘: %v", err)
	}

	// 2) zip 内含单个 jar
	z1 := makeZip(t, src, map[string]string{"app.jar": "fake", "application.yml": "port: 1"})
	dest2 := filepath.Join(base, "site2")
	name, err = DeployJavaArtifact(z1, "pkg.zip", dest2)
	if err != nil {
		t.Fatalf("zip 部署失败: %v", err)
	}
	if name != "app.jar" {
		t.Fatalf("zip 内 jar 文件名 = %q", name)
	}

	// 3) zip 内含多个 jar：应跳过 sources/javadoc，取主程序包
	z2 := makeZip(t, src, map[string]string{
		"lib/app-1.0.jar":        strings.Repeat("x", 4000),
		"lib/app-1.0-sources.jar": "small",
		"lib/app-1.0-javadoc.jar": "small",
	})
	dest3 := filepath.Join(base, "site3")
	name, err = DeployJavaArtifact(z2, "multi.zip", dest3)
	if err != nil {
		t.Fatalf("多 jar 部署失败: %v", err)
	}
	if name != filepath.Join("lib", "app-1.0.jar") {
		t.Fatalf("多 jar 应选主程序包，实际 %q", name)
	}

	// 4) war 包明确拒绝并给出 Tomcat 引导
	if _, err := DeployJavaArtifact(jarPath, "app.war", filepath.Join(base, "site4")); err == nil {
		t.Fatal("war 应被拒绝")
	} else if !strings.Contains(err.Error(), "Tomcat") {
		t.Fatalf("war 报错应提示 Tomcat，实际 %q", err.Error())
	}

	// 5) 其它扩展名拒绝
	if _, err := DeployJavaArtifact(jarPath, "notes.txt", filepath.Join(base, "site5")); err == nil {
		t.Fatal("非 jar/zip 应被拒绝")
	}

	// 6) zip 内没有 jar
	z3 := makeZip(t, src, map[string]string{"readme.txt": "no jar here"})
	if _, err := DeployJavaArtifact(z3, "empty.zip", filepath.Join(base, "site6")); err == nil {
		t.Fatal("zip 内无 jar 应报错")
	}
}

// makeJar 生成一个带 MANIFEST.MF 的 jar 包（用于测试清单解析）。
func makeJar(t *testing.T, dir, name string, manifest string, extra map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// MANIFEST 需保证以空行结尾（zip 写入时自动处理）
	w, err := zw.Create("META-INF/MANIFEST.MF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(manifest + "\n\n")); err != nil {
		t.Fatal(err)
	}
	for n, c := range extra {
		w2, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w2.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

// TestInspectJavaArtifact 覆盖 jar 清单解析：Spring Boot 识别、Main-Class/Start-Class、
// 内嵌 server.port（properties 与 yml）。
func TestInspectJavaArtifact(t *testing.T) {
	dir := t.TempDir()

	// 1) Spring Boot fat jar：Main-Class 是 loader，真正的入口在 Start-Class
	sbManifest := "Manifest-Version: 1.0\n" +
		"Main-Class: org.springframework.boot.loader.JarLauncher\n" +
		"Start-Class: com.example.MyApp\n" +
		"Spring-Boot-Version: 2.7.0\n"
	sbJar := makeJar(t, dir, "sb.jar", sbManifest, map[string]string{
		"BOOT-INF/classes/application.properties": "server.port=8080\nspring.name=demo\n",
	})
	info := InspectJavaArtifact(sbJar)
	if !info.SpringBoot {
		t.Fatal("Spring Boot 包应被识别为 SpringBoot")
	}
	if info.StartClass != "com.example.MyApp" {
		t.Fatalf("Start-Class = %q", info.StartClass)
	}
	if info.Version != "2.7.0" {
		t.Fatalf("Spring-Boot-Version = %q", info.Version)
	}
	if info.Port != 8080 {
		t.Fatalf("properties 里的 server.port 应为 8080，实际 %d", info.Port)
	}
	if !info.Runnable() {
		t.Fatal("Spring Boot 包应可运行")
	}

	// 2) 普通可执行 jar：只有 Main-Class
	plain := makeJar(t, dir, "plain.jar", "Manifest-Version: 1.0\nMain-Class: com.example.Main\n", nil)
	info2 := InspectJavaArtifact(plain)
	if info2.SpringBoot || info2.StartClass != "" {
		t.Fatalf("普通 jar 不应被识别为 Spring Boot，实际 %+v", info2)
	}
	if info2.MainClass != "com.example.Main" || !info2.Runnable() {
		t.Fatalf("普通 jar 应有 Main-Class，实际 %+v", info2)
	}

	// 3) 无清单的 jar：不可运行，不报错
	noManifest := makeJar(t, dir, "no.jar", "", map[string]string{"a.class": "x"})
	info3 := InspectJavaArtifact(noManifest)
	if info3.Runnable() {
		t.Fatal("无清单 jar 不应可运行")
	}

	// 4) yml 里的端口（块写法）
	ymlManifest := "Main-Class: org.springframework.boot.loader.JarLauncher\nStart-Class: c.MyApp\n"
	ymlJar := makeJar(t, dir, "yml.jar", ymlManifest, map[string]string{
		"BOOT-INF/classes/application.yml": "server:\n  port: 9090\nspring:\n  name: x\n",
	})
	if got := InspectJavaArtifact(ymlJar).Port; got != 9090 {
		t.Fatalf("yml 块写法 server.port 应为 9090，实际 %d", got)
	}

	// 5) yml 单行写法
	ymlSingle := makeJar(t, dir, "yml2.jar", ymlManifest, map[string]string{
		"BOOT-INF/classes/application.yml": "server.port: 7070\n",
	})
	if got := InspectJavaArtifact(ymlSingle).Port; got != 7070 {
		t.Fatalf("yml 单行 server.port 应为 7070，实际 %d", got)
	}
}

// TestManifestValueFold 覆盖 MANIFEST 72 字节折叠续行（RFC 规定行宽上限）。
func TestManifestValueFold(t *testing.T) {
	// Main-Class 超长，被折叠成多行（续行以空格开头）
	manifest := "Manifest-Version: 1.0\n" +
		"Main-Class: com.example.very.long.package.name.that.will.be.folded.across.mult\n" +
		" iple.lines.Main\n"
	if got := manifestValue(manifest, "Main-Class"); got != "com.example.very.long.package.name.that.will.be.folded.across.multiple.lines.Main" {
		t.Fatalf("折叠续行解析错误，实际 %q", got)
	}
}

// TestParseServerPort 覆盖 properties / yml 端口解析的边界情况。
func TestParseServerPort(t *testing.T) {
	// properties
	if got := parseServerPortProperties("# comment\nserver.port=1234\nother=1\n"); got != 1234 {
		t.Fatalf("properties 端口 = %d", got)
	}
	if got := parseServerPortProperties("server.port=abc\n"); got != 0 {
		t.Fatalf("非法端口应返回 0，实际 %d", got)
	}
	if got := parseServerPortProperties("server.port = 8080 \n"); got != 8080 {
		t.Fatalf("带空格的 properties 端口 = %d", got)
	}
	// yml 块写法缩进变化
	if got := parseServerPortYAML("server:\n    port: 8000\nfoo: 1\n"); got != 8000 {
		t.Fatalf("yml 端口 = %d", got)
	}
	// yml 单行
	if got := parseServerPortYAML("server.port: 8001\n"); got != 8001 {
		t.Fatalf("yml 单行端口 = %d", got)
	}
}

// TestMapBTProjectTypeJava 宝塔 Java / Spring Boot 项目应映射为本站 Java 站点。
func TestMapBTProjectTypeJava(t *testing.T) {
	cases := map[string]string{
		"java":       model.SiteTypeJava,
		"Java":       model.SiteTypeJava,
		"springboot": model.SiteTypeJava,
		"node":       model.SiteTypeNode,
		"go":         model.SiteTypeGo,
	}
	for in, want := range cases {
		if got := mapBTProjectType(in); got != want {
			t.Fatalf("mapBTProjectType(%q) = %q, want %q", in, got, want)
		}
	}
}
