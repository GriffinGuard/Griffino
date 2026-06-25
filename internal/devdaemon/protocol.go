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

// Package devdaemon implements the Unix Domain Socket protocol between
// griffino dev commands and the daemon process / 实现 griffino dev 命令与 daemon 进程之间的 Unix Domain Socket 通信协议.
//
// Only griffino dev install/start/stop commands use this package;
// Web-UI and regular CLI commands don't go through here / 只有 griffino dev 命令使用此包，Web-UI 和普通 CLI 不经过这里.
package devdaemon

import "encoding/json"

// ── Request ────────────────────────────────────────────────────────────────

// Request is the message sent from CLI to daemon.
// Op drives dispatch logic on the daemon side; Payload holds operation-specific parameters / CLI 发往 daemon 的消息，Op 驱动分发逻辑，Payload 是操作参数.
type Request struct {
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// InstallPayload holds parameters for the dev_install operation / dev_install 操作的参数.
type InstallPayload struct {
	Path  string `json:"path"`  // Absolute path to the plugin directory / 插件目录绝对路径
	Force bool   `json:"force"` // When true, overwrite an already-installed plugin with the same ID / true 时覆盖同 ID 的已安装插件
}

// PluginIDPayload dev_start / dev_stop 操作的参数。
type PluginIDPayload struct {
	PluginID string `json:"pluginId"`
}

// Registered operation name constants, centrally managed for future extension / 已注册的操作名常量，便于后续扩展时集中管理.
const (
	OpDevInstall   = "dev_install"
	OpDevStart     = "dev_start"
	OpDevStop      = "dev_stop"
	OpDevUninstall = "dev_uninstall"
)

// UninstallPayload holds parameters for the dev_uninstall operation / dev_uninstall 操作的参数.
type UninstallPayload struct {
	PluginID string `json:"pluginId"`
	Force    bool   `json:"force"` // When true, stop first then delete / true 时先执行 stop 再删除
}

// ── Response ───────────────────────────────────────────────────────────────

// Response is the message returned from daemon to CLI / daemon 返回给 CLI 的消息.
type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// InstallData is the content of the Data field on a successful dev_install / dev_install 成功时 Data 字段的内容.
type InstallData struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	PluginVersion string `json:"PluginVersion"`
	Overwritten   bool   `json:"overwritten"` // True when an existing install was overwritten via --force / 经 --force 覆盖已有安装时为 true
}

// StartData is the content of the Data field on a successful dev_start / dev_start 成功时 Data 字段的内容.
type StartData struct {
	Network      string            `json:"network"`
	RabbitMQUser string            `json:"rabbitmqUser"`
	Containers   map[string]string `json:"containers"`
}
