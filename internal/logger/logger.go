package logger

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/lumberjack.v2"
)

const (
	// 日志文件大小上限（MB）
	maxSizeMB = 10
	maxSizeErrorMB = 5

	// 保留的旧日志文件数量
	maxBackups = 3
)

// pluginIDKey 是 slog context 里 pluginID 的 key
type contextKey string
const pluginIDKey contextKey = "pluginID"

// WithPluginID 在 context 里注入 pluginID，供 logger 路由使用
func WithPluginID(ctx context.Context, pluginID string) context.Context {
	return context.WithValue(ctx, pluginIDKey, pluginID)
}

// PluginIDFromContext 从 context 提取 pluginID
func PluginIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(pluginIDKey).(string); ok {
		return v
	}
	return ""
}

// Init 初始化全局 slog logger。
// devMode=true 时只输出到 stderr + griffino-dev.log，不写其他文件。
// logDir 是日志目录，通常是 ~/.griffino/logs/
func Init(logDir string, devMode bool) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	var handler slog.Handler

	if devMode {
		// 前台模式：stderr + griffino-dev.log
		devLog := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "griffino-dev.log"),
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			Compress:   false,
		}
		handler = newMultiHandler(
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
			slog.NewTextHandler(devLog, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	} else {
		// daemon 模式：按级别和来源路由到不同文件
		mainLog := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "griffino.log"),
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			Compress:   false,
		}
		systemErrLog := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "griffino-error.log"),
			MaxSize:    maxSizeErrorMB,
			MaxBackups: maxBackups,
			Compress:   false,
		}
		pluginErrLog := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "plugin-error.log"),
			MaxSize:    maxSizeErrorMB,
			MaxBackups: maxBackups,
			Compress:   false,
		}
		initPluginLogDir(logDir, pluginErrLog)
		handler = newRoutingHandler(mainLog, systemErrLog, pluginErrLog)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}