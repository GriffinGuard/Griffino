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

// Package devdaemon 实现 griffino dev 命令与 daemon 进程之间的
// Unix Domain Socket 通信协议。
//
// 只有通过 griffino dev install/start/stop 才会使用此包，
// Web-UI 和普通 CLI 命令不经过这里。
package devdaemon

import "encoding/json"

// ── 请求 ────────────────────────────────────────────────────────────────────

// Request 是 CLI 发往 daemon 的消息。
// Op 字段驱动 daemon 侧的分发逻辑；Payload 是操作相关的参数。
type Request struct {
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// InstallPayload dev_install 操作的参数。
type InstallPayload struct {
	Path string `json:"path"` // 插件目录绝对路径
}

// PluginIDPayload dev_start / dev_stop 操作的参数。
type PluginIDPayload struct {
	PluginID string `json:"pluginId"`
}

// 已注册的操作名常量，便于后续扩展时集中管理。
const (
	OpDevInstall   = "dev_install"
	OpDevStart     = "dev_start"
	OpDevStop      = "dev_stop"
	OpDevUninstall = "dev_uninstall"
)

// UninstallPayload dev_uninstall 操作的参数。
type UninstallPayload struct {
	PluginID string `json:"pluginId"`
	Force    bool   `json:"force"` // true 时先执行 stop 再删除
}

// ── 响应 ────────────────────────────────────────────────────────────────────

// Response 是 daemon 返回给 CLI 的消息。
type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// InstallData dev_install 成功时 Data 字段的内容。
type InstallData struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PluginVersion string `json:"PluginVersion"`
}

// StartData dev_start 成功时 Data 字段的内容。
type StartData struct {
	Network    string            `json:"network"`
	RabbitMQUser string          `json:"rabbitmqUser"`
	Containers map[string]string `json:"containers"`
}