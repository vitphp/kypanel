package service

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// 危险命令黑名单，防止面板被利用执行破坏性操作。
// 注意：这是纵深防御，仅拦截明显的破坏性指令；真正的权限边界由超管校验保证。
var forbiddenCmds = []string{
	"rm -rf /", "rm -rf /*", "rm -fr /", "mkfs", "dd if=", "shutdown", "reboot", "halt",
	"chmod -R 777 /", "kill -9 1", "poweroff", "init 0", "init 6",
	"fdisk", "wipefs", "mkswap",
}

// forbiddenCmdsCompact 对「去除全部空格/制表符」后的紧凑命令串做匹配，
// 防 rm -r -f / 这类拆参数/插空格的绕过写法（与下方 rm 启发式配合）。
var forbiddenCmdsCompact = []string{
	"rm-rf/", "rm-rf./", "rm-fr/", "rm-fr./", "rm-r-f/", "rm-r-f./",
	"rm-rf*/", "rm-fr*/", "rm-r*/",
	"chmod-r777/", "chmod-777-r/",
	"kill-91", "kill--91",
	"ddif=/dev/zero", "ddif=/dev/urandom",
	"init0", "init6",
	":(){", // fork 炸弹函数定义特征（| ; 已被清洗为空格，保留核心语法）
}

// 默认命令执行超时
var defaultExecTimeout = 10 * time.Second

// DefaultExecTimeout 返回默认命令执行超时
func DefaultExecTimeout() time.Duration {
	return defaultExecTimeout
}

// ExecResult 命令执行结果
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// ExecCommand 执行一条 shell 命令（带超时和黑名单校验）
// 超时后不仅杀掉 /bin/sh 壳，还会杀掉它派生的整个进程组，
// 防止 systemctl 等命令挂起时留下孤儿进程堆积。
func ExecCommand(cmdStr string, timeout time.Duration) (*ExecResult, error) {
	if err := checkForbidden(cmdStr); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	setProcAttr(cmd) // 独立进程组，便于超时时整体清理
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, errors.New("命令启动失败: " + err.Error())
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				return nil, errors.New("命令执行失败: " + err.Error())
			}
		}
		return &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: code,
		}, nil
	case <-ctx.Done():
		// 超时：杀死整个进程组（含 /bin/sh 派生的 systemctl 等），并等待回收
		killProcessGroup(cmd.Process.Pid)
		<-done
		return nil, errors.New("命令执行超时")
	}
}

func checkForbidden(cmdStr string) error {
	lower := strings.ToLower(cmdStr)
	// 去除常见干扰：分号连接、引号包裹
	cleaned := strings.NewReplacer(";", " ", "&&", " ", "|", " ", "'", "", `"`, "").Replace(lower)
	for _, f := range forbiddenCmds {
		if strings.Contains(cleaned, f) {
			return errors.New("命令包含危险操作，已被面板拦截: " + f)
		}
	}
	// 紧凑匹配：去掉所有空白后再比对，防拆参数/插空格绕过
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(cleaned)
	for _, f := range forbiddenCmdsCompact {
		if strings.Contains(compact, f) {
			return errors.New("命令包含危险操作，已被面板拦截: " + f)
		}
	}
	// rm 启发式：rm 同时带递归(-r/-R)与强制(-f)，且目标指向根/通配符/相对上级时拦截。
	// 普通删除（如 rm -f /tmp/x、rm -r 某目录树）不受影响。
	if err := checkDangerousRm(cleaned); err != nil {
		return err
	}
	return nil
}

// checkDangerousRm 拦截危险 rm 组合：-r 与 -f 同时出现，且路径以 / 开头、
// 含通配符 *、或以 .. 开头（可能越过目录向上删）。
// 同时覆盖 sudo rm -rf /、rm --recursive --force / 等变体。
func checkDangerousRm(cleaned string) error {
	tokens := strings.Fields(cleaned)
	for i := 0; i < len(tokens); i++ {
		if tokens[i] != "rm" {
			continue
		}
		recursive := false
		force := false
		var paths []string
		for j := i + 1; j < len(tokens); j++ {
			a := tokens[j]
			if a == ";" || a == "&&" || a == "||" {
				break
			}
			if strings.HasPrefix(a, "-") {
				flags := strings.TrimLeft(a, "-")
				if strings.Contains(flags, "r") || strings.Contains(flags, "R") {
					recursive = true
				}
				if strings.Contains(flags, "f") {
					force = true
				}
				continue
			}
			paths = append(paths, a)
		}
		if !(recursive && force) {
			continue
		}
		for _, p := range paths {
			if strings.HasPrefix(p, "/") ||
				strings.Contains(p, "*") ||
				strings.HasPrefix(p, "..") {
				return errors.New("命令包含危险 rm 操作（递归强制删除敏感路径），已被面板拦截")
			}
		}
	}
	return nil
}

// RestartPanel 重启面板自身服务（systemctl restart kypanel）。
// 延迟 800ms 执行，让当前 HTTP response 先返回；异步执行避免阻塞请求。
func RestartPanel() {
	go func() {
		time.Sleep(800 * time.Millisecond)
		_ = exec.Command("systemctl", "restart", "kypanel").Start()
	}()
}

// RestartServer 重启整台服务器（reboot）。延迟 800ms 让 response 先返回。
func RestartServer() {
	go func() {
		time.Sleep(800 * time.Millisecond)
		_ = exec.Command("reboot").Start()
	}()
}
