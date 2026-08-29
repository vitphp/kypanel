package service

import (
	"errors"
	"fmt"
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

func importMysqlFile(dbName, path string) error {
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
	var cmd2 string
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		cmd2 = fmt.Sprintf("gunzip -c %s | %s %s %s", shellQuote(path), shellQuote(mysql), mysqlBaseArgs(), shellQuote(dbName))
	} else {
		cmd2 = fmt.Sprintf("%s %s %s < %s", shellQuote(mysql), mysqlBaseArgs(), shellQuote(dbName), shellQuote(path))
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

