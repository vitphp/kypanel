package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Options 日志配置
type Options struct {
	Level  string // debug | info | warn | error
	File   string // 日志文件路径，为空则只输出到控制台
	MaxDay int    // 日志保留天数
}

// Setup 初始化全局 slog
func Setup(opt Options) (*slog.Logger, error) {
	level := slog.LevelInfo
	switch opt.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var writer io.Writer = os.Stdout

	if opt.File != "" {
		if err := os.MkdirAll(filepath.Dir(opt.File), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(opt.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		writer = io.MultiWriter(os.Stdout, f)
	}

	// 手动构造 slog 配置，日志带 2006-01-02 15:04:05 格式时间
	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, a.Value.Time().Format(time.DateTime))
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	return logger, nil
}
