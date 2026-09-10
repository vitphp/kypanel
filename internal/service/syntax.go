package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SyntaxError 单条语法错误
type SyntaxError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// SyntaxResult 语法检查结果
type SyntaxResult struct {
	Supported bool          `json:"supported"`
	OK        bool          `json:"ok"`
	Errors    []SyntaxError `json:"errors"`
	Message   string        `json:"message"`
	Raw       string        `json:"raw"`
}

var (
	rePhpLine    = regexp.MustCompile(`on line (\d+)`)
	reNodeLine   = regexp.MustCompile(`^[^\n]+:(\d+)(?::(\d+))?`)
	rePyLine     = regexp.MustCompile(`line (\d+)`)
	reGoLine     = regexp.MustCompile(`:(\d+):(\d+):`)
	reBashLine   = regexp.MustCompile(`line (\d+)`)
)

// checkYAML YAML 结构体检：Tab 缩进、缩进层级不一致、引号/括号未闭合。
// 不做完整语法解析（无法识别的写法按正常处理），只报确定的问题。
func checkYAML(content string) (int, string) {
	lines := strings.Split(content, "\n")
	var levels []int // 已出现的缩进层级
	blockIndent := -1
	for i, raw := range lines {
		no := i + 1
		line := strings.TrimRight(raw, " \r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "...") {
			continue
		}
		if strings.ContainsRune(line[:len(line)-len(trimmed)], '\t') {
			return no, fmt.Sprintf("第 %d 行使用了 Tab 缩进，YAML 只允许空格缩进", no)
		}
		indent := len(line) - len(trimmed)
		// 块标量（| 或 >）内部：缩进更深的内容按字面量处理，不参与结构校验
		if blockIndent >= 0 {
			if indent > blockIndent {
				continue
			}
			blockIndent = -1
		}
		if msg := checkQuotesAndBrackets(trimmed); msg != "" {
			return no, fmt.Sprintf("第 %d 行%s", no, msg)
		}
		switch {
		case len(levels) == 0 || indent > levels[len(levels)-1]:
			levels = append(levels, indent)
		default:
			for len(levels) > 0 && indent < levels[len(levels)-1] {
				levels = levels[:len(levels)-1]
			}
			if len(levels) == 0 || indent != levels[len(levels)-1] {
				return no, fmt.Sprintf("第 %d 行缩进层级不一致（%d 个空格），同级内容需对齐", no, indent)
			}
		}
		if isBlockScalar(trimmed) {
			blockIndent = indent
		}
	}
	return 0, ""
}

// isBlockScalar 判断该行是否为块标量起始（key: | / key: > / - | 等）
func isBlockScalar(trimmed string) bool {
	body := trimmed
	if idx := strings.LastIndex(body, ":"); idx >= 0 {
		body = strings.TrimSpace(body[idx+1:])
	} else if strings.HasPrefix(body, "- ") {
		body = strings.TrimSpace(body[2:])
	}
	if body == "" || (body[0] != '|' && body[0] != '>') {
		return false
	}
	for _, c := range body[1:] {
		if c != '+' && c != '-' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// checkQuotesAndBrackets 检查一行内引号与流式括号是否闭合（引号内与注释内的括号忽略）
func checkQuotesAndBrackets(s string) string {
	var (
		inSingle, inDouble, escaped, done bool
		prevSpace                         = true
		stack                             []rune
	)
	for _, r := range s {
		if done {
			break
		}
		if inDouble && escaped {
			escaped = false
			continue
		}
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			}
		case inDouble:
			if r == '\\' {
				escaped = true
			} else if r == '"' {
				inDouble = false
			}
		case r == '\'':
			inSingle = true
		case r == '"':
			inDouble = true
		case r == '#' && prevSpace:
			done = true
		case r == '[' || r == '{':
			stack = append(stack, r)
		case r == ']' || r == '}':
			if len(stack) == 0 {
				return "有多余的 " + string(r)
			}
			open := stack[len(stack)-1]
			if (r == ']' && open != '[') || (r == '}' && open != '{') {
				return "括号不匹配：" + string(open) + " 与 " + string(r)
			}
			stack = stack[:len(stack)-1]
		}
		prevSpace = r == ' ' || r == '\t'
	}
	if inSingle {
		return "单引号未闭合"
	}
	if inDouble {
		return "双引号未闭合"
	}
	if len(stack) > 0 {
		return "括号未闭合"
	}
	return ""
}

// runTimeout 执行命令并返回输出，带超时（命令不存在时返回 ErrNotFound 标志）
func runTimeout(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out.String() + "\n[超时]", false
		}
		if _, ok := err.(*exec.Error); ok {
			return "", false // 命令不存在
		}
	}
	return out.String(), true
}

// syntaxExt 语言 → 检查用文件扩展名与临时文件名
func syntaxExt(lang string) string {
	switch lang {
	case "php":
		return ".php"
	case "js", "mjs", "cjs":
		return ".js"
	case "py":
		return ".py"
	case "go":
		return ".go"
	case "bash", "sh":
		return ".sh"
	default:
		return ""
	}
}

// SyntaxCheck 对给定语言/内容做语法检查（用当前编辑内容写入临时文件，命令参数分离防注入）
func SyntaxCheck(lang, content string) SyntaxResult {
	res := SyntaxResult{Supported: true, OK: true}

	// 纯解析类（无需外部命令）
	switch lang {
	case "json":
		var v any
		if err := json.Unmarshal([]byte(content), &v); err != nil {
			line := 1
			if se, ok := err.(*json.SyntaxError); ok {
				line = 1 + bytes.Count([]byte(content)[:min64(se.Offset, int64(len(content)))], []byte("\n"))
			}
			res.OK = false
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: err.Error()})
			res.Raw = err.Error()
		} else {
			res.Message = "JSON 格式正确"
		}
		return res
	case "yaml", "yml":
		if line, msg := checkYAML(content); msg != "" {
			res.OK = false
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: msg})
			res.Raw = msg
		} else {
			res.Message = "YAML 格式正确"
		}
		return res
	}

	ext := syntaxExt(lang)
	if ext == "" {
		res.Supported = false
		res.Message = "暂不支持该语言的语法检查（支持：PHP / JS / Python / Go / Shell / JSON / YAML）"
		return res
	}

	// 写临时文件
	tmp, err := os.CreateTemp("", "panel-syntax-*"+ext)
	if err != nil {
		res.Supported = false
		res.Message = "创建临时文件失败: " + err.Error()
		return res
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		res.Supported = false
		res.Message = "写入临时文件失败: " + err.Error()
		return res
	}
	tmp.Close()

	switch lang {
	case "php":
		out, ran := runTimeout(8*time.Second, "php", "-l", tmpName)
		if !ran {
			res.Supported = false
			res.Message = "服务器未安装 php 命令，无法检查"
			return res
		}
		if strings.Contains(out, "No syntax errors") {
			res.Message = "PHP 语法检查通过"
			return res
		}
		res.OK = false
		res.Raw = out
		if m := rePhpLine.FindStringSubmatch(out); len(m) > 1 {
			line, _ := strconv.Atoi(m[1])
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: syntaxFirstLine(out)})
		} else {
			res.Errors = append(res.Errors, SyntaxError{Line: 0, Message: syntaxFirstLine(out)})
		}
	case "js", "mjs", "cjs":
		out, ran := runTimeout(8*time.Second, "node", "--check", tmpName)
		if !ran {
			res.Supported = false
			res.Message = "服务器未安装 node 命令，无法检查"
			return res
		}
		if strings.TrimSpace(out) == "" {
			res.Message = "JS 语法检查通过"
			return res
		}
		res.OK = false
		res.Raw = out
		line := 0
		if m := reNodeLine.FindStringSubmatch(out); len(m) > 1 {
			line, _ = strconv.Atoi(m[1])
		}
		res.Errors = append(res.Errors, SyntaxError{Line: line, Message: syntaxFirstLine(out)})
	case "py":
		out, ran := runTimeout(10*time.Second, "python3", "-m", "py_compile", tmpName)
		if !ran {
			out, ran = runTimeout(10*time.Second, "python", "-m", "py_compile", tmpName)
		}
		if !ran {
			res.Supported = false
			res.Message = "服务器未安装 python 命令，无法检查"
			return res
		}
		if strings.TrimSpace(out) == "" {
			res.Message = "Python 语法检查通过"
			return res
		}
		res.OK = false
		res.Raw = out
		if m := rePyLine.FindStringSubmatch(out); len(m) > 1 {
			line, _ := strconv.Atoi(m[1])
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: syntaxLastLine(out)})
		} else {
			res.Errors = append(res.Errors, SyntaxError{Line: 0, Message: syntaxFirstLine(out)})
		}
	case "go":
		out, ran := runTimeout(10*time.Second, "gofmt", "-e", tmpName)
		if !ran {
			res.Supported = false
			res.Message = "服务器未安装 gofmt 命令，无法检查"
			return res
		}
		if strings.TrimSpace(out) == "" {
			res.Message = "Go 语法检查通过"
			return res
		}
		res.OK = false
		res.Raw = out
		if m := reGoLine.FindStringSubmatch(out); len(m) > 2 {
			line, _ := strconv.Atoi(m[1])
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: syntaxFirstLine(out)})
		} else {
			res.Errors = append(res.Errors, SyntaxError{Line: 0, Message: syntaxFirstLine(out)})
		}
	case "bash", "sh":
		out, ran := runTimeout(8*time.Second, "bash", "-n", tmpName)
		if !ran {
			res.Supported = false
			res.Message = "服务器未安装 bash 命令，无法检查"
			return res
		}
		if strings.TrimSpace(out) == "" {
			res.Message = "Shell 语法检查通过"
			return res
		}
		res.OK = false
		res.Raw = out
		if m := reBashLine.FindStringSubmatch(out); len(m) > 1 {
			line, _ := strconv.Atoi(m[1])
			res.Errors = append(res.Errors, SyntaxError{Line: line, Message: syntaxFirstLine(out)})
		} else {
			res.Errors = append(res.Errors, SyntaxError{Line: 0, Message: syntaxFirstLine(out)})
		}
	}
	return res
}

func syntaxFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func syntaxLastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// SyntaxCheckLang 由文件名推断语言（供路由使用）
func SyntaxCheckLang(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".php":
		return "php"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".py":
		return "py"
	case ".go":
		return "go"
	case ".sh", ".bash":
		return "bash"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}
