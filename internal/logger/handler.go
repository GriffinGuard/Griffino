package logger

import (
	"context"
	"io"
	"log/slog"
)

// multiHandler 将日志同时发送到多个 handler（用于 devMode）
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			_ = handler.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// routingHandler daemon 模式下的路由 handler：
//   - 所有日志 → mainLog（griffino.log）
//   - Warn/Error + 有 pluginID attr → pluginErrLog（plugin-error.log）
//   - Warn/Error + 无 pluginID attr → systemErrLog（griffino-error.log）
type routingHandler struct {
	mainHandler      slog.Handler
	systemErrHandler slog.Handler
	pluginErrHandler slog.Handler
}

func newRoutingHandler(mainW, systemErrW, pluginErrW io.Writer) *routingHandler {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	errOpts := &slog.HandlerOptions{Level: slog.LevelWarn}
	return &routingHandler{
		mainHandler:      slog.NewTextHandler(mainW, opts),
		systemErrHandler: slog.NewTextHandler(systemErrW, errOpts),
		pluginErrHandler: slog.NewTextHandler(pluginErrW, errOpts),
	}
}

func (h *routingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *routingHandler) Handle(ctx context.Context, r slog.Record) error {
	// 所有日志写入 griffino.log
	if h.mainHandler.Enabled(ctx, r.Level) {
		_ = h.mainHandler.Handle(ctx, r.Clone())
	}

	// Warn/Error 额外路由
	if r.Level >= slog.LevelWarn {
		if hasPluginID(r) {
			_ = h.pluginErrHandler.Handle(ctx, r.Clone())
		} else {
			_ = h.systemErrHandler.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (h *routingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &routingHandler{
		mainHandler:      h.mainHandler.WithAttrs(attrs),
		systemErrHandler: h.systemErrHandler.WithAttrs(attrs),
		pluginErrHandler: h.pluginErrHandler.WithAttrs(attrs),
	}
}

func (h *routingHandler) WithGroup(name string) slog.Handler {
	return &routingHandler{
		mainHandler:      h.mainHandler.WithGroup(name),
		systemErrHandler: h.systemErrHandler.WithGroup(name),
		pluginErrHandler: h.pluginErrHandler.WithGroup(name),
	}
}

// hasPluginID 检查 record 的 attrs 里是否有 pluginID 字段
func hasPluginID(r slog.Record) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "pluginID" {
			found = true
			return false
		}
		return true
	})
	return found
}