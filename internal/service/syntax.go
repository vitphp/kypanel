package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
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
	reYamlLine   = regexp.MustCompile(`(?m)^(\d+):(\d+):`)
	reYamlLine2  = regexp.MustCompile(`line (\d+), column (\d+)`)
)

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
		var v any
		if err := yaml.Unmarshal([]byte(content), &v); err != nil {
			line := 0
			msg := err.Error()
			if m := reYamlLine.FindStringSubmatch(msg); len(m) > 1 {
				line, _ = strconv.Atoi(m[1])
			} else if m := reYamlLine2.FindStringSubmatch(msg); len(m) > 1 {
				line, _ = strconv.Atoi(m[1])
			}
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
