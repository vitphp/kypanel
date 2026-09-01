package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// getOrCreateDBAccount 获取或初始化一条数据库账号记录
func getOrCreateDBAccount(dbType, dbName, username string) *model.DatabaseAccount {
	var acc model.DatabaseAccount
	err := model.DB.Where("type = ? AND db_name = ?", dbType, dbName).First(&acc).Error
	if err != nil {
		acc = model.DatabaseAccount{Type: dbType, DbName: dbName, Username: username}
	}
	if acc.Username == "" && username != "" {
		acc.Username = username
	}
	return &acc
}

// saveDBAccount 保存数据库账号记录
func saveDBAccount(acc *model.DatabaseAccount) error {
	return model.Upsert(acc.ID, acc)
}

// deleteDBAccount 删除数据库账号记录
func deleteDBAccount(dbType, dbName string) {
	model.DB.Where("type = ? AND db_name = ?", dbType, dbName).Delete(&model.DatabaseAccount{})
}

// DatabaseType 数据库类型
type DatabaseType string

const (
	DBTypeMySQL     DatabaseType = "mysql"
	DBTypeSQLServer DatabaseType = "sqlserver"
	DBTypeMongoDB   DatabaseType = "mongodb"
	DBTypeRedis     DatabaseType = "redis"
	DBTypePgSQL     DatabaseType = "pgsql"
	DBTypeSQLite    DatabaseType = "sqlite"
)

// DBTypeList 返回所有受支持的数据库类型（顺序对应 UI Tab）
func DBTypeList() []DatabaseType {
	return []DatabaseType{DBTypeMySQL, DBTypeSQLServer, DBTypeMongoDB, DBTypeRedis, DBTypePgSQL, DBTypeSQLite}
}

// DBTypeMeta 数据库类型元信息
type DBTypeMeta struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

// ListDatabaseTypes 返回所有数据库类型元信息
func ListDatabaseTypes() []DBTypeMeta {
	var res []DBTypeMeta
	for _, t := range DBTypeList() {
		res = append(res, DBTypeMeta{Type: string(t), Label: engineLabel(t)})
	}
	return res
}

func engineLabel(t DatabaseType) string {
	switch t {
	case DBTypeMySQL:
		return "MySQL"
	case DBTypeSQLServer:
		return "SQLServer"
	case DBTypeMongoDB:
		return "MongoDB"
	case DBTypeRedis:
		return "Redis"
	case DBTypePgSQL:
		return "PgSQL"
	case DBTypeSQLite:
		return "SQLite"
	}
	return string(t)
}

// DatabaseEngine 抽象不同数据库的管理接口
type DatabaseEngine interface {
	Type() DatabaseType
	Label() string
	Available() (bool, string)
	List() ([]map[string]interface{}, error)
	Create(req CreateDatabaseReq) error
	Delete(name string) error
}

var engines = map[DatabaseType]DatabaseEngine{
	DBTypeMySQL:     mysqlEngine{},
	DBTypePgSQL:     pgsqlEngine{},
	DBTypeRedis:     redisEngine{},
	DBTypeMongoDB:   mongodbEngine{},
	DBTypeSQLServer: sqlserverEngine{},
	DBTypeSQLite:    sqliteEngine{},
}

// GetDatabaseEngine 按类型获取引擎
func GetDatabaseEngine(dbType string) (DatabaseEngine, error) {
	dt := DatabaseType(dbType)
	e, ok := engines[dt]
	if !ok {
		return nil, fmt.Errorf("未知的数据库类型: %s", dbType)
	}
	return e, nil
}

// DatabaseAvailable 检测某类数据库是否可用
func DatabaseAvailable(dbType string) (bool, string) {
	e, err := GetDatabaseEngine(dbType)
	if err != nil {
		return false, err.Error()
	}
	return e.Available()
}

// ListDatabasesByType 按类型列出数据库
func ListDatabasesByType(dbType string) ([]map[string]interface{}, error) {
	e, err := GetDatabaseEngine(dbType)
	if err != nil {
		return nil, err
	}
	list, err := e.List()
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		item["type"] = dbType
	}
	return list, nil
}

// CreateDatabaseByType 按类型创建数据库
func CreateDatabaseByType(dbType string, req CreateDatabaseReq) error {
	e, err := GetDatabaseEngine(dbType)
	if err != nil {
		return err
	}
	return e.Create(req)
}

// DeleteDatabaseByType 按类型删除数据库
func DeleteDatabaseByType(dbType, name string) error {
	e, err := GetDatabaseEngine(dbType)
	if err != nil {
		return err
	}
	return e.Delete(name)
}

// -------------- MySQL --------------

type mysqlEngine struct{}

func (mysqlEngine) Type() DatabaseType { return DBTypeMySQL }
func (mysqlEngine) Label() string      { return "MySQL" }

func (mysqlEngine) Available() (bool, string) {
	if _, err := LookPathBin("mysql"); err != nil {
		return false, "未检测到 mysql 命令，请先在应用商店安装 MySQL/MariaDB"
	}
	return true, ""
}

func mysqlBaseArgs() string {
	// 注意：mysql_root_pw 在数据库里是 enc:v1: 密文，
	// 必须用 GetMysqlRootPwd()（解密后）拼连接参数，否则会拿密文当密码导致 1045
	pw := GetMysqlRootPwd()
	if pw == "" {
		return "-uroot -N -s"
	}
	return fmt.Sprintf("-uroot -p%s -N -s", shellQuote(pw))
}

// mysqlCredFile 返回 MySQL root 凭据文件路径（0600，option 文件格式）。
// 计划任务「备份数据库」通过 --defaults-extra-file 引用它，
// 避免 MySQL root 密码明文写进系统 crontab / 任务命令列。
func mysqlCredFile() string {
	dir := config.Get().DataDir
	if dir == "" {
		dir = "/opt/kypanel"
	}
	return filepath.Join(dir, "conf", "mysql-root.cnf")
}

// ensureMysqlCredFile 把解密后的 MySQL root 密码写入 0600 凭据文件，
// 供 mysqldump / mysql 通过 --defaults-extra-file 免明文密码连接。
// 密码为空时删除文件（无密码环境退回 auth_socket，无需凭据文件）。
func ensureMysqlCredFile() error {
	pw := GetMysqlRootPwd()
	path := mysqlCredFile()
	if pw == "" {
		_ = os.Remove(path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// option 文件转义：引号内 # 为字面量，仅需转义反斜杠与双引号
	esc := strings.ReplaceAll(pw, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	content := "[client]\nuser=root\npassword=\"" + esc + "\"\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// mysqlCredArgs 返回 cron 备份任务用的连接参数：通过 0600 凭据文件认证，
// 密码不进入 crontab 命令行。--defaults-extra-file 必须是第一个参数。
// 密码为空（auth_socket 免密）时退回无密码形式，与 mysqlBaseArgs 行为一致。
func mysqlCredArgs() string {
	if GetMysqlRootPwd() == "" {
		return "-uroot -N -s"
	}
	return fmt.Sprintf("--defaults-extra-file=%s -uroot -N -s", shellQuote(mysqlCredFile()))
}

func (mysqlEngine) List() ([]map[string]interface{}, error) {
	ok, _ := MysqlAvailable()
	if !ok {
		// 未安装 MySQL：返回空列表而非报错，避免网站搬家等场景因缺环境直接失败
		return []map[string]interface{}{}, nil
	}
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(`SELECT schema_name, default_character_set_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys') ORDER BY schema_name`)
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}

	// 读取 MySQL 用户主机列表
	hostCmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(`SELECT user, host FROM mysql.user WHERE user NOT IN ('root','mysql.session','mysql.sys','debian-sys-maint') ORDER BY user, host`)
	hostRes, _ := ExecCommand(hostCmd, 15*time.Second)
	hostMap := make(map[string][]string)
	if hostRes != nil && hostRes.ExitCode == 0 {
		for _, line := range strings.Split(strings.TrimSpace(hostRes.Stdout), "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) >= 2 {
				hostMap[parts[0]] = append(hostMap[parts[0]], parts[1])
			}
		}
	}

	var rows []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		acc := getOrCreateDBAccount(string(DBTypeMySQL), name, name)
		if acc.ID == 0 {
			_ = saveDBAccount(acc)
		}
		hosts := hostMap[acc.Username]
		if len(hosts) == 0 {
			hosts = []string{"localhost"}
		}
		backupCount := DatabaseBackupCount("mysql", name)
		backupText := "0个"
		if backupCount > 0 {
			backupText = strconv.Itoa(backupCount) + "个"
		}
		rows = append(rows, map[string]interface{}{
			"name":     name,
			"charset":  parts[1],
			"user":     acc.Username,
			"password": acc.Password,
			"comment":  acc.Comment,
			"hosts":    strings.Join(hosts, ","),
			"location": "本地数据库",
			"backup":   backupText,
		})
	}
	return rows, nil
}

func (mysqlEngine) Create(req CreateDatabaseReq) error {
	ok, msg := MysqlAvailable()
	if !ok {
		return errors.New(msg)
	}
	if req.Name == "" {
		return errors.New("数据库名不能为空")
	}
	if !identRe.MatchString(req.Name) {
		return errors.New("数据库名只能包含字母、数字、下划线")
	}
	if len(req.Name) > 64 {
		return errors.New("数据库名过长")
	}
	user := req.User
	if user == "" {
		user = req.Name
	}
	if !identRe.MatchString(user) {
		return errors.New("用户名只能包含字母、数字、下划线")
	}
	if len(user) > 32 {
		return errors.New("用户名过长")
	}
	if req.Password == "" {
		return errors.New("请设置数据库密码")
	}

	base := mysqlBaseArgs()
	statements := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", req.Name),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", user, sqlEscapeString(req.Password)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'", req.Name, user),
		"FLUSH PRIVILEGES",
	}
	cmd := "mysql " + base + " -e " + shellQuote(strings.Join(statements, ";"))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	// 记录账号信息到本地 sqlite
	acc := getOrCreateDBAccount(string(DBTypeMySQL), req.Name, user)
	acc.Password = req.Password
	_ = saveDBAccount(acc)
	return nil
}

func (mysqlEngine) Delete(name string) error {
	ok, msg := MysqlAvailable()
	if !ok {
		return errors.New(msg)
	}
	if isSystemDatabase(name) {
		return errors.New("系统数据库不允许删除")
	}
	// 清理该数据库关联的所有用户主机
	acc := getOrCreateDBAccount(string(DBTypeMySQL), name, "")
	if acc.Username != "" {
		dropUserHosts(acc.Username)
	}
	statements := []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name),
	}
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(strings.Join(statements, ";"))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	deleteDBAccount(string(DBTypeMySQL), name)
	return nil
}

// dropUserHosts 删除某 MySQL 用户的全部主机记录
func dropUserHosts(user string) {
	if user == "" {
		return
	}
	hosts := getMysqlUserHosts(user)
	var stmts []string
	userEscaped := strings.ReplaceAll(user, "'", "''")
	for _, host := range hosts {
		stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", userEscaped, strings.ReplaceAll(host, "'", "''")))
	}
	if len(stmts) == 0 {
		return
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	_, _ = ExecCommand("mysql "+mysqlBaseArgs()+" -e "+shellQuote(strings.Join(stmts, ";")), 15*time.Second)
}

// getMysqlUserHosts 查询某 MySQL 用户的所有主机
func getMysqlUserHosts(user string) []string {
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(fmt.Sprintf(
		"SELECT host FROM mysql.user WHERE user='%s' ORDER BY host",
		strings.ReplaceAll(user, "'", "''"),
	))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil || res.ExitCode != 0 {
		return []string{}
	}
	var hosts []string
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			hosts = append(hosts, line)
		}
	}
	return hosts
}

// mysqlUserStatements 生成创建/授权/回收 MySQL 用户的 SQL 语句
func mysqlUserStatements(dbName, user, password string, hosts []string) []string {
	var stmts []string
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		hostEscaped := strings.ReplaceAll(host, "'", "''")
		stmts = append(stmts, fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", user, hostEscaped, sqlEscapeString(password)))
		stmts = append(stmts, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", dbName, user, hostEscaped))
	}
	// 删除不在 hosts 列表中的旧主机
	existing := getMysqlUserHosts(user)
	for _, oldHost := range existing {
		found := false
		for _, h := range hosts {
			if strings.TrimSpace(h) == oldHost {
				found = true
				break
			}
		}
		if !found {
			stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", strings.ReplaceAll(user, "'", "''"), strings.ReplaceAll(oldHost, "'", "''")))
		}
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	return stmts
}

// SetMysqlHosts 设置 MySQL 数据库用户的允许访问主机
func SetMysqlHosts(dbName string, hosts []string) error {
	ok, msg := MysqlAvailable()
	if !ok {
		return errors.New(msg)
	}
	acc := getOrCreateDBAccount(string(DBTypeMySQL), dbName, "")
	if acc.Username == "" {
		return errors.New("未找到该数据库的用户名")
	}
	if acc.Password == "" {
		return errors.New("未记录该数据库密码，请先修改密码后再设置权限")
	}
	stmts := mysqlUserStatements(dbName, acc.Username, acc.Password, hosts)
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(strings.Join(stmts, ";"))
	res, err := ExecCommand(cmd, 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	acc.Hosts = strings.Join(hosts, ",")
	return saveDBAccount(acc)
}

// ChangeMysqlPassword 修改 MySQL 数据库用户密码并同步所有主机
func ChangeMysqlPassword(dbName, password string) error {
	ok, msg := MysqlAvailable()
	if !ok {
		return errors.New(msg)
	}
	if password == "" {
		return errors.New("密码不能为空")
	}
	acc := getOrCreateDBAccount(string(DBTypeMySQL), dbName, "")
	if acc.Username == "" {
		return errors.New("未找到该数据库的用户名")
	}
	hosts := getMysqlUserHosts(acc.Username)
	if len(hosts) == 0 {
		hosts = []string{"localhost"}
	}
	var stmts []string
	for _, host := range hosts {
		hostEscaped := strings.ReplaceAll(host, "'", "''")
		stmts = append(stmts, fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s'", acc.Username, hostEscaped, sqlEscapeString(password)))
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(strings.Join(stmts, ";"))
	res, err := ExecCommand(cmd, 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	acc.Password = password
	return saveDBAccount(acc)
}

// UpdateDatabaseComment 更新数据库备注
func UpdateDatabaseComment(dbType, dbName, comment string) error {
	acc := getOrCreateDBAccount(dbType, dbName, "")
	acc.Comment = comment
	return saveDBAccount(acc)
}

// -------------- PostgreSQL --------------

type pgsqlEngine struct{}

func (pgsqlEngine) Type() DatabaseType { return DBTypePgSQL }
func (pgsqlEngine) Label() string      { return "PostgreSQL" }

func (pgsqlEngine) Available() (bool, string) {
	if _, err := LookPathBin("psql"); err != nil {
		return false, "未检测到 psql 命令，请先在应用商店安装 PostgreSQL"
	}
	return true, ""
}

func (pgsqlEngine) execSql(sql string) (*ExecResult, error) {
	// 优先使用 sudo -u postgres；失败时降级到 su - postgres
	q := shellQuote(sql)
	cmd := fmt.Sprintf("sudo -u postgres psql -A -t -c %s", q)
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil || res.ExitCode != 0 {
		cmd = fmt.Sprintf("su - postgres -c %s", shellQuote("psql -A -t -c "+q))
		res, err = ExecCommand(cmd, 15*time.Second)
	}
	return res, err
}

func (pgsqlEngine) List() ([]map[string]interface{}, error) {
	res, err := pgsqlEngine{}.execSql(`SELECT datname, pg_encoding_to_char(encoding) FROM pg_database WHERE datistemplate=false ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}
	var rows []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "postgres" {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"name":    name,
			"charset": strings.TrimSpace(parts[1]),
		})
	}
	return rows, nil
}

func (e pgsqlEngine) Create(req CreateDatabaseReq) error {
	if err := checkDBIdentifier(req.Name, "数据库名"); err != nil {
		return err
	}
	user := req.User
	if user == "" {
		user = req.Name
	}
	if err := checkDBIdentifier(user, "用户名"); err != nil {
		return err
	}
	statements := []string{
		fmt.Sprintf("CREATE DATABASE %s ENCODING 'UTF8'", req.Name),
	}
	if req.Password != "" {
		statements = append(statements, fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", user, sqlEscapeString(req.Password)))
		statements = append(statements, fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", req.Name, user))
	}
	res, err := e.execSql(strings.Join(statements, ";"))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func (e pgsqlEngine) Delete(name string) error {
	if err := checkDBIdentifier(name, "数据库名"); err != nil {
		return err
	}
	statements := []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", name),
		fmt.Sprintf("DROP USER IF EXISTS %s", name),
	}
	res, err := e.execSql(strings.Join(statements, ";"))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

// -------------- Redis --------------

type redisEngine struct{}

func (redisEngine) Type() DatabaseType { return DBTypeRedis }
func (redisEngine) Label() string      { return "Redis" }

func (redisEngine) Available() (bool, string) {
	if _, err := LookPathBin("redis-cli"); err != nil {
		return false, "未检测到 redis-cli 命令，请先在应用商店安装 Redis"
	}
	res, err := ExecCommand("redis-cli ping", 2*time.Second)
	if err != nil || res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "PONG" {
		return false, "Redis 服务未运行"
	}
	return true, ""
}

func (redisEngine) List() ([]map[string]interface{}, error) {
	res, err := ExecCommand("redis-cli INFO keyspace", 5*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}

	keyspace := make(map[int]int)
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "db") {
			continue
		}
		// db0:keys=1,expires=0,avg_ttl=0
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		dbIdxStr := line[2:colon]
		dbIdx, _ := strconv.Atoi(dbIdxStr)
		count := 0
		if kv := strings.Split(line[colon+1:], ","); len(kv) > 0 {
			if kvParts := strings.Split(kv[0], "="); len(kvParts) == 2 {
				count, _ = strconv.Atoi(kvParts[1])
			}
		}
		keyspace[dbIdx] = count
	}

	var rows []map[string]interface{}
	for i := 0; i < 16; i++ {
		rows = append(rows, map[string]interface{}{
			"name":    fmt.Sprintf("db%d", i),
			"index":   i,
			"keys":    keyspace[i],
			"comment": "逻辑数据库",
		})
	}
	return rows, nil
}

func (redisEngine) Create(req CreateDatabaseReq) error {
	// Redis 默认固定 16 个逻辑库；创建即选择首个 keys 为 0 的库并打一个标记
	res, err := ExecCommand("redis-cli INFO keyspace", 5*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	used := make(map[int]bool)
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "db") {
			colon := strings.Index(line, ":")
			if colon > 0 {
				idx, _ := strconv.Atoi(line[2:colon])
				used[idx] = true
			}
		}
	}
	for i := 0; i < 16; i++ {
		if !used[i] {
			cmd := fmt.Sprintf("redis-cli SELECT %d && redis-cli SET __panel_db_mark true", i)
			res, err := ExecCommand(cmd, 5*time.Second)
			if err != nil {
				return err
			}
			if res.ExitCode != 0 {
				return errors.New(res.Stderr)
			}
			return nil
		}
	}
	return errors.New("Redis 16 个逻辑库已满，请手动清理后重试")
}

func (redisEngine) Delete(name string) error {
	idx := 0
	if strings.HasPrefix(name, "db") {
		idx, _ = strconv.Atoi(name[2:])
	}
	cmd := fmt.Sprintf("redis-cli SELECT %d && redis-cli FLUSHDB", idx)
	res, err := ExecCommand(cmd, 5*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

// -------------- MongoDB --------------

type mongodbEngine struct{}

func (mongodbEngine) Type() DatabaseType { return DBTypeMongoDB }
func (mongodbEngine) Label() string      { return "MongoDB" }

func (mongodbEngine) clientBin() string {
	if _, err := LookPathBin("mongosh"); err == nil {
		return "mongosh"
	}
	return "mongo"
}

func (mongodbEngine) Available() (bool, string) {
	bin := mongodbEngine{}.clientBin()
	if _, err := LookPathBin(bin); err != nil {
		return false, "未检测到 mongosh/mongo 命令，请先在应用商店安装 MongoDB"
	}
	return true, ""
}

func (e mongodbEngine) eval(script string) (*ExecResult, error) {
	cmd := fmt.Sprintf("%s --quiet --eval %s", e.clientBin(), shellQuote(script))
	return ExecCommand(cmd, 15*time.Second)
}

func (e mongodbEngine) List() ([]map[string]interface{}, error) {
	script := `JSON.stringify(db.adminCommand('listDatabases').databases.map(d => ({name: d.name, sizeOnDisk: d.sizeOnDisk})))`
	res, err := e.eval(script)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}
	raw := strings.TrimSpace(res.Stdout)
	if raw == "" {
		return []map[string]interface{}{}, nil
	}
	var items []struct {
		Name       string  `json:"name"`
		SizeOnDisk float64 `json:"sizeOnDisk"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, errors.New("解析 MongoDB 数据库列表失败: " + err.Error())
	}
	var rows []map[string]interface{}
	for _, item := range items {
		if item.Name == "admin" || item.Name == "local" || item.Name == "config" {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"name": item.Name,
			"size": item.SizeOnDisk,
		})
	}
	return rows, nil
}

func (e mongodbEngine) Create(req CreateDatabaseReq) error {
	if err := checkDBIdentifier(req.Name, "数据库名"); err != nil {
		return err
	}
	script := fmt.Sprintf(`use('%s'); db.createCollection('__panel_init')`, req.Name)
	res, err := e.eval(script)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func (e mongodbEngine) Delete(name string) error {
	if err := checkDBIdentifier(name, "数据库名"); err != nil {
		return err
	}
	script := fmt.Sprintf(`use('%s'); db.dropDatabase()`, name)
	res, err := e.eval(script)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

// -------------- SQLServer --------------

type sqlserverEngine struct{}

func (sqlserverEngine) Type() DatabaseType { return DBTypeSQLServer }
func (sqlserverEngine) Label() string      { return "SQLServer" }

func (sqlserverEngine) Available() (bool, string) {
	if _, err := LookPathBin("sqlcmd"); err != nil {
		return false, "未检测到 sqlcmd 命令，请先在应用商店安装 SQLServer"
	}
	return true, ""
}

func (sqlserverEngine) saPassword() string {
	return decryptSetting(model.GetSetting("mssql_sa_pw"))
}

func (e sqlserverEngine) sqlcmd(extra string) string {
	pw := e.saPassword()
	if pw != "" {
		return fmt.Sprintf("sqlcmd -S localhost -U sa -P %s %s", shellQuote(pw), extra)
	}
	return fmt.Sprintf("sqlcmd -S localhost %s", extra)
}

func (e sqlserverEngine) List() ([]map[string]interface{}, error) {
	cmd := e.sqlcmd("-Q \"SELECT name FROM sys.databases WHERE name NOT IN ('master','tempdb','model','msdb') ORDER BY name\"")
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(res.Stderr)
	}
	var rows []map[string]interface{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "---") || strings.Contains(line, "(") {
			continue
		}
		if line == "name" {
			continue
		}
		rows = append(rows, map[string]interface{}{"name": line})
	}
	return rows, nil
}

func (e sqlserverEngine) Create(req CreateDatabaseReq) error {
	if err := checkDBIdentifier(req.Name, "数据库名"); err != nil {
		return err
	}
	cmd := e.sqlcmd(fmt.Sprintf("-Q \"CREATE DATABASE [%s]\"", req.Name))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func (e sqlserverEngine) Delete(name string) error {
	if err := checkDBIdentifier(name, "数据库名"); err != nil {
		return err
	}
	cmd := e.sqlcmd(fmt.Sprintf("-Q \"DROP DATABASE IF EXISTS [%s]\"", name))
	res, err := ExecCommand(cmd, 15*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

// -------------- SQLite --------------

type sqliteEngine struct{}

func (sqliteEngine) Type() DatabaseType { return DBTypeSQLite }
func (sqliteEngine) Label() string      { return "SQLite" }

func (sqliteEngine) Available() (bool, string) {
	if _, err := LookPathBin("sqlite3"); err != nil {
		return false, "未检测到 sqlite3 命令，请先在应用商店安装 SQLite"
	}
	return true, ""
}

func (sqliteEngine) dataDir() string {
	return filepath.Join(config.Get().DataDir, "sqlite")
}

func (e sqliteEngine) List() ([]map[string]interface{}, error) {
	dir := e.dataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !(strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite")) {
			continue
		}
		info, _ := entry.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		rows = append(rows, map[string]interface{}{
			"name": name,
			"path": filepath.Join(dir, name),
			"size": size,
		})
	}
	return rows, nil
}

func (e sqliteEngine) Create(req CreateDatabaseReq) error {
	if req.Name == "" {
		return errors.New("数据库文件名不能为空")
	}
	// 文件名不允许包含路径分隔符 / 穿越字符，防止写入 dataDir 之外
	if strings.ContainsAny(req.Name, `/\`) || strings.Contains(req.Name, "..") {
		return errors.New("数据库文件名不能包含路径字符")
	}
	name := req.Name
	if !strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".sqlite") {
		name += ".db"
	}
	dir := e.dataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return errors.New("数据库文件已存在")
	}
	cmd := fmt.Sprintf("sqlite3 %s \"VACUUM;\"", shellQuote(path))
	res, err := ExecCommand(cmd, 10*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(res.Stderr)
	}
	return nil
}

func (e sqliteEngine) Delete(name string) error {
	// name 会拼进文件路径，白名单校验防止路径穿越删除 dataDir 外文件
	if !identRe.MatchString(name) {
		return errors.New("数据库名无效")
	}
	path := filepath.Join(e.dataDir(), name)
	return os.Remove(path)
}
