package service

import (
	"os/exec"
	"strings"
)

// LookPathBin 检测命令是否存在
func LookPathBin(name string) (string, error) {
	return exec.LookPath(name)
}

// shellQuote 用单引号包裹参数，内部单引号转义
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin 把参数数组拼成一行 shell 命令
func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	return strings.Join(quoted, " ")
}
