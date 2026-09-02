package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/config"
)

// DatabaseBackupItem 数据库备份项
type DatabaseBackupItem struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SizeText  string `json:"sizeText"`
	Time      string `json:"time"`
	Storage   string `json:"storage"`
	Remark    string `json:"remark"`
}

// databaseBackupDir 备份文件统一存放目录
func databaseBackupDir() string {
	return filepath.Join(config.Get().DataDir, "backup", "database")
}

// DatabaseBackupList 列出某个数据库的备份文件
func DatabaseBackupList(dbType, dbName string) ([]DatabaseBackupItem, error) {
	if dbName == "" {
		return nil, errors.New("数据库名不能为空")
	}
	dir := databaseBackupDir()
	_ = os.MkdirAll(dir, 0755)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var items []DatabaseBackupItem
	prefix := dbName + "_"
	ext := backupExtension(dbType)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, DatabaseBackupItem{
			Name:     name,
			Size:     info.Size(),
			SizeText: FormatBytes(info.Size()),
			Time:     info.ModTime().Format("2006-01-02 15:04:05"),
			Storage:  "本地",
			Remark:   "",
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Time > items[j].Time
	})
	return items, nil
}

// DatabaseBackupCreate 为数据库创建一次备份
func DatabaseBackupCreate(dbType, dbName string) error {
	if dbName == "" {
		return errors.New("数据库名不能为空")
	}
	dir := databaseBackupDir()
	_ = os.MkdirAll(dir, 0755)

	ts := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("%s_%s%s", dbName, ts, backupExtension(dbType))
	dest := filepath.Join(dir, fileName)

	switch strings.ToLower(dbType) {
	case "mysql":
		return backupMysqlDatabase(dbName, dest)
	case "sqlite":
		return backupSqliteDatabase(dbName, dest)
	default:
		return fmt.Errorf("暂不支持 %s 数据库的备份", dbType)
	}
}

// DatabaseBackupRestore 从备份文件恢复数据库
func DatabaseBackupRestore(dbType, dbName, fileName string) error {
	if dbName == "" || fileName == "" {
		return errors.New("数据库名或文件名不能为空")
	}
	path, err := validateBackupPath(dbType, dbName, fileName)
	if err != nil {
		return err
	}
	switch strings.ToLower(dbType) {
	case "mysql":
		return restoreMysqlBackup(dbName, path)
	case "sqlite":
		return restoreSqliteBackup(dbName, path)
	default:
		return fmt.Errorf("暂不支持 %s 数据库的恢复", dbType)
	}
}

// DatabaseBackupDelete 删除某个备份文件
func DatabaseBackupDelete(dbType, dbName, fileName string) error {
	path, err := validateBackupPath(dbType, dbName, fileName)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// DatabaseBackupPath 返回备份文件完整路径（用于下载）
func DatabaseBackupPath(dbType, dbName, fileName string) (string, error) {
	return validateBackupPath(dbType, dbName, fileName)
}

// DatabaseBackupCount 返回某个数据库的备份数量
func DatabaseBackupCount(dbType, dbName string) int {
	items, _ := DatabaseBackupList(dbType, dbName)
	return len(items)
}

func backupExtension(dbType string) string {
	switch strings.ToLower(dbType) {
	case "mysql":
		return ".sql.gz"
	case "sqlite":
		return ".db"
	default:
		return ".bak"
	}
}

func validateBackupPath(dbType, dbName, fileName string) (string, error) {
	if strings.ContainsAny(fileName, "/\\") {
		return "", errors.New("非法的文件名")
	}
	dir := databaseBackupDir()
	path := filepath.Join(dir, fileName)
	// 校验文件确实属于目标数据库
	prefix := dbName + "_"
	ext := backupExtension(dbType)
	if !strings.HasPrefix(fileName, prefix) || !strings.HasSuffix(fileName, ext) {
		return "", errors.New("备份文件与数据库不匹配")
	}
	if _, err := os.Stat(path); err != nil {
		return "", errors.New("备份文件不存在")
	}
	return path, nil
}

// mysqlDumpSupportsGTIDPurged 检查当前 mysqldump 是否支持 --set-gtid-purged 参数
// MariaDB 及部分老版本 MySQL 不支持该参数，使用时会报错
func mysqlDumpSupportsGTIDPurged() bool {
	mysqldump, err := LookPathBin("mysqldump")
	if err != nil {
		return false
	}
	res, _ := ExecCommand(fmt.Sprintf("%s --version", shellQuote(mysqldump)), 10*time.Second)
	if res == nil {
		return false
	}
	ver := strings.ToLower(res.Stdout + res.Stderr)
	if strings.Contains(ver, "mariadb") {
		return false
	}
	re := regexp.MustCompile(`(\d+)\.(\d+)`)
	if m := re.FindStringSubmatch(ver); len(m) >= 3 {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		return major > 5 || (major == 5 && minor >= 6)
	}
	return false
}

func backupMysqlDatabase(dbName, dest string) error {
	mysqldump, err := LookPathBin("mysqldump")
	if err != nil {
		return errors.New("未找到 mysqldump 命令，请确认 MySQL 已安装")
	}
	// 确保目标目录存在
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)

	args := strings.ReplaceAll(mysqlBaseArgs(), "-N -s", "")
	gtid := ""
	if mysqlDumpSupportsGTIDPurged() {
		gtid = " --set-gtid-purged=OFF"
	}
	cmd := fmt.Sprintf("%s %s --default-character-set=utf8mb4 --single-transaction%s --quick %s | gzip > %s",
		shellQuote(mysqldump), args, gtid, shellQuote(dbName), shellQuote(dest))
	res, err := ExecCommand(cmd, 300*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func restoreMysqlBackup(dbName, path string) error {
	mysql, err := LookPathBin("mysql")
	if err != nil {
		return errors.New("未找到 mysql 命令，请确认 MySQL 已安装")
	}
	// 先确保数据库存在
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4", dbName)
	cmd1 := fmt.Sprintf("%s %s -e %s", shellQuote(mysql), mysqlBaseArgs(), shellQuote(createSQL))
	res1, err := ExecCommand(cmd1, 60*time.Second)
	if err != nil {
		return err
	}
	if res1.ExitCode != 0 {
		return errors.New(res1.Stderr)
	}
	cmd2 := fmt.Sprintf("gunzip -c %s | %s %s %s", shellQuote(path), shellQuote(mysql), mysqlBaseArgs(), shellQuote(dbName))
	res2, err := ExecCommand(cmd2, 300*time.Second)
	if err != nil {
		return err
	}
	if res2.ExitCode != 0 {
		return errors.New(res2.Stderr)
	}
	return nil
}

func backupSqliteDatabase(dbName, dest string) error {
	e := sqliteEngine{}
	src := filepath.Join(e.dataDir(), dbName)
	if _, err := os.Stat(src); err != nil {
		return errors.New("数据库文件不存在")
	}
	cmd := fmt.Sprintf("cp -f %s %s", shellQuote(src), shellQuote(dest))
	res, err := ExecCommand(cmd, 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func restoreSqliteBackup(dbName, path string) error {
	e := sqliteEngine{}
	src := filepath.Join(e.dataDir(), dbName)
	cmd := fmt.Sprintf("cp -f %s %s", shellQuote(path), shellQuote(src))
	res, err := ExecCommand(cmd, 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

// DatabaseImport 从用户上传的 SQL/DB 文件导入到指定数据库
func DatabaseImport(dbType, dbName, filePath string) error {
	if dbName == "" {
		return errors.New("数据库名不能为空")
	}
	if filePath == "" {
		return errors.New("导入文件路径不能为空")
	}
	if _, err := os.Stat(filePath); err != nil {
		return errors.New("导入文件不存在")
	}
	switch strings.ToLower(dbType) {
	case "mysql":
		return importMysqlFile(dbName, filePath)
	case "sqlite":
		return importSqliteFile(dbName, filePath)
	default:
		return fmt.Errorf("暂不支持 %s 数据库的导入", dbType)
	}
}

// sqlImportCompatArgs 导入前的流式 SQL 兼容降级（sed 参数，直接拼进 shell 命令）。
// 用 sed 流式处理而不是落盘改写，避免大库解压后占用双倍磁盘：
//   - ROW_FORMAT=COMPACT/REDUNDANT → DYNAMIC：COMPACT/REDUNDANT 会把 BLOB 前缀 768 字节
//     内联存储，宽表（大量 varchar/TEXT）累加后极易触发
//     "ERROR 1118 Row size too large (> 8126)"；DYNAMIC 只存 20 字节指针，可根治。
//     源库能存下通常是因为其默认 ROW_FORMAT=DYNAMIC（MySQL 5.7.9+ 起为默认）。
//   - utf8mb3 → utf8mb4、0900_/uca1400_ 系列 collation → unicode_ci：
//     目标库版本偏低时不识别，会报 "ERROR 1273 Unknown collation"，
//     进而 CREATE TABLE 全失败、后续 INSERT 报 "Table doesn't exist"。
const sqlImportCompatArgs = `-e 's/ROW_FORMAT=COMPACT/ROW_FORMAT=DYNAMIC/gI' -e 's/ROW_FORMAT=REDUNDANT/ROW_FORMAT=DYNAMIC/gI' -e 's/utf8mb3/utf8mb4/gI' -e 's/uca1400_ai_ci/unicode_ci/gI' -e 's/uca1400_as_ci/unicode_ci/gI' -e 's/uca1400_ai_cs/unicode_ci/gI' -e 's/uca1400_as_cs/unicode_ci/gI' -e 's/0900_ai_ci/unicode_ci/gI' -e 's/0900_as_ci/unicode_ci/gI' -e 's/0900_ai_cs/unicode_ci/gI' -e 's/0900_as_cs/unicode_ci/gI'`

// sqlImportSessionSQL 导入会话前置语句：兜底关闭 InnoDB 严格模式（仍有极宽行时降级为警告
// 而非报错），并放大 max_allowed_packet 以容纳大 INSERT。
const sqlImportSessionSQL = `SET SESSION innodb_strict_mode=OFF;`

// sqlImportMysqlClientArgs mysql 客户端导入参数。
// 注意：新版本 MySQL 中 SESSION 级 max_allowed_packet 是只读的（ERROR 1621），
// 必须用客户端参数 --max_allowed_packet 设置；服务端上限另行尝试 SET GLOBAL。
const sqlImportMysqlClientArgs = "--max-allowed-packet=1G"

// raiseGlobalMaxAllowedPacket 尝试把服务端 GLOBAL max_allowed_packet 提到 1G，
// 保证大 SQL 导入不被服务端 64M/16M 默认限制截断。失败（权限/版本不支持）仅记日志不阻断导入。
func raiseGlobalMaxAllowedPacket() {
	mysql, err := LookPathBin("mysql")
	if err != nil {
		return
	}
	cmd := fmt.Sprintf("%s %s -e %s", shellQuote(mysql), mysqlBaseArgs(),
		shellQuote("SET GLOBAL max_allowed_packet=1073741824"))
	_, _ = ExecCommand(cmd, 15*time.Second)
}

// importMysqlFile 把 SQL 文件导入 MySQL。
// 兼容 .sql / .sql.gz / .sql.zip（第三方面板新版数据库手动备份为 .sql.zip，zip 内是单个 .sql）。
func importMysqlFile(dbName, path string) error {
	// zip 备份先用 Go archive/zip 解出 .sql 再导入（不依赖系统 unzip）。
	// 按魔数识别而非文件后缀：迁移场景下载的对端备份统一落地为 .sql.gz 命名，
	// 但第三方面板新版数据库备份实际是 zip 格式，按后缀会错误走 gunzip 分支。
	if isZipFile(path) {
		sqlPath, cleanup, err := extractSQLFromZip(path)
		if err != nil {
			return err
		}
		defer cleanup()
		path = sqlPath
	}
	mysql, err := LookPathBin("mysql")
	if err != nil {
		return errors.New("未找到 mysql 命令，请确认 MySQL 已安装")
	}
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4", dbName)
	cmd1 := fmt.Sprintf("%s %s -e %s", shellQuote(mysql), mysqlBaseArgs(), shellQuote(createSQL))
	res1, err := ExecCommand(cmd1, 60*time.Second)
	if err != nil {
		return err
	}
	if res1.ExitCode != 0 {
		return errors.New(res1.Stderr)
	}
	// 导入：先 SET 会话变量，再流式做兼容降级后灌入 mysql
	raiseGlobalMaxAllowedPacket()
	var cmd2 string
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		cmd2 = fmt.Sprintf("{ echo %s; gunzip -c %s | sed %s; } | %s %s %s %s",
			shellQuote(sqlImportSessionSQL), shellQuote(path), sqlImportCompatArgs,
			shellQuote(mysql), mysqlBaseArgs(), sqlImportMysqlClientArgs, shellQuote(dbName))
	} else {
		cmd2 = fmt.Sprintf("{ echo %s; sed %s %s; } | %s %s %s %s",
			shellQuote(sqlImportSessionSQL), sqlImportCompatArgs, shellQuote(path),
			shellQuote(mysql), mysqlBaseArgs(), sqlImportMysqlClientArgs, shellQuote(dbName))
	}
	res2, err := ExecCommand(cmd2, 300*time.Second)
	if err != nil {
		return err
	}
	if res2.ExitCode != 0 {
		return errors.New(res2.Stderr)
	}
	return nil
}

// extractSQLFromZip 从 .sql.zip 中解出 .sql 文件到临时目录，返回临时文件路径与清理函数。
// 第三方面板数据库 zip 备份内通常只有一个 .sql；若出现多个，优先取名字带 .sql 后缀的。
func extractSQLFromZip(zipPath string) (string, func(), error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", nil, fmt.Errorf("打开数据库 zip 备份失败: %w", err)
	}
	defer zr.Close()
	var target *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".sql") {
			target = f
			break
		}
	}
	if target == nil && len(zr.File) > 0 {
		target = zr.File[0]
	}
	if target == nil {
		return "", nil, errors.New("数据库 zip 备份内没有文件")
	}
	rc, err := target.Open()
	if err != nil {
		return "", nil, fmt.Errorf("读取 zip 内文件失败: %w", err)
	}
	defer rc.Close()
	tmp := filepath.Join(os.TempDir(), "kypanel-sql-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql")
	out, err := os.Create(tmp)
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", nil, fmt.Errorf("解压数据库 zip 失败: %w", err)
	}
	out.Close()
	return tmp, func() { os.Remove(tmp) }, nil
}

func importSqliteFile(dbName, path string) error {
	e := sqliteEngine{}
	dest := filepath.Join(e.dataDir(), dbName)
	cmd := fmt.Sprintf("cp -f %s %s", shellQuote(path), shellQuote(dest))
	res, err := ExecCommand(cmd, 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

