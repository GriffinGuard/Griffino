// Copyright 2025 GriffinGuard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logger

import (
	"context"
	"io"
	"log/slog"
)

// multiHandler sends logs to multiple handlers simultaneously (used in devMode) / 将日志同时发送到多个 handler（用于 devMode）
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

// routingHandler is the daemon-mode routing handler:
//   - All logs → mainLog (griffino.log)
//   - Warn/Error + pluginID attr → pluginErrLog (plugin-error.log)
//   - Warn/Error + no pluginID attr → systemErrLog (griffino-error.log) / daemon 模式下的路由 handler
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
	// All logs go to griffino.log / 所有日志写入 griffino.log
	if h.mainHandler.Enabled(ctx, r.Level) {
		_ = h.mainHandler.Handle(ctx, r.Clone())
	}

	// Warn/Error additional routing / Warn/Error 额外路由
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

// hasPluginID checks whether the record's attrs contain a pluginID field / 检查 record 的 attrs 里是否有 pluginID 字段
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
