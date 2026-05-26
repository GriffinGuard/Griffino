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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/lumberjack.v2"
)

// pluginLoggers 全局缓存，pluginID → *PluginLogger
// lumberjack 本身线程安全，map 访问用 mu 保护
var (
	pluginLogMu      sync.Mutex
	pluginLoggers    = make(map[string]*PluginLogger)
	globalPluginErrW io.Writer // 指向全局 plugin-error.log
	pluginLogDir     string    // 插件日志根目录，Init 时设置
)

// PluginLogger 持有单个插件的两个日志 writer
type PluginLogger struct {
	All  io.Writer // plugin.log（全量）
	Err  io.Writer // plugin-error.log（仅 Warn/Error）
}

// initPluginLogDir 由 logger.Init 调用，设置插件日志目录和全局 writer
func initPluginLogDir(logDir string, pluginErrW io.Writer) {
	pluginLogDir = filepath.Join(logDir, "plugins")
	globalPluginErrW = pluginErrW
}

// GetPluginLogger 获取或创建插件的日志 writer
// 多次调用同一 pluginID 返回同一实例
func GetPluginLogger(pluginID string) (*PluginLogger, error) {
	pluginLogMu.Lock()
	defer pluginLogMu.Unlock()

	if l, ok := pluginLoggers[pluginID]; ok {
		return l, nil
	}

	dir := filepath.Join(pluginLogDir, pluginID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin log dir: %w", err)
	}

	l := &PluginLogger{
		All: &lumberjack.Logger{
			Filename:   filepath.Join(dir, "plugin.log"),
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			Compress:   false,
		},
		Err: &lumberjack.Logger{
			Filename:   filepath.Join(dir, "plugin-error.log"),
			MaxSize:    maxSizeErrorMB,
			MaxBackups: maxBackups,
			Compress:   false,
		},
	}
	pluginLoggers[pluginID] = l
	return l, nil
}

// ClosePluginLogger 插件停止时从缓存中移除（lumberjack 无需显式 Close）
func ClosePluginLogger(pluginID string) {
	pluginLogMu.Lock()
	defer pluginLogMu.Unlock()
	delete(pluginLoggers, pluginID)
}

// GlobalPluginErrWriter 返回全局 plugin-error.log 的 writer
func GlobalPluginErrWriter() io.Writer {
	return globalPluginErrW
}