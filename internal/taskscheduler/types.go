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

package taskscheduler

import (
	"encoding/json"
	"time"
)

// ─── Blueprint（蓝图，存 BoltDB，永久保存）───────────────────────────────────

type Blueprint struct {
	ID        string          `json:"id"`
	UserID    string          `json:"userId"`
	Name      string          `json:"name"`
	Trigger   Trigger         `json:"trigger"`
	Nodes     []Node          `json:"nodes"`
	Metadata  BlueprintMeta   `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type Trigger struct {
	EventType string `json:"eventType"`
	PluginID  string `json:"pluginId,omitempty"` // 为空表示任意来源
}

type Node struct {
	ID                  string   `json:"id"`
	PluginID            string   `json:"pluginId"`            // "__builtin__" 表示内置节点
	CapabilityID        string   `json:"capabilityId"`        // 内置节点：if/loop/input/output
	RequestTopicPattern string   `json:"requestTopicPattern"` // 内置节点复用此字段存表达式/变量声明
	NextNodes           []string `json:"nextNodes"`
	TimeoutMs           int      `json:"timeoutMs,omitempty"`
}

// BlueprintMeta 由前端维护，后端透传存储，执行引擎忽略
type BlueprintMeta struct {
	FlowPositions map[string]FlowPosition `json:"flowPositions,omitempty"`
}

type FlowPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ─── 内置节点常量 ─────────────────────────────────────────────────────────────

const (
	BuiltinPluginID = "__builtin__"

	// CapabilityID 值
	BuiltinCapIF     = "if"
	BuiltinCapLoop   = "loop"
	BuiltinCapInput  = "input"
	BuiltinCapOutput = "output"

	// INPUT 节点系统内置 source 前缀
	BuiltinSourceNow    = "__builtin__.now"
	BuiltinSourceUserID = "__builtin__.userId"
)

// ─── Task（任务实例，存 Redis，TTL 24h）──────────────────────────────────────

type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID              string            `json:"id"`
	BlueprintID     string            `json:"blueprintId"`
	UserID          string            `json:"userId"`
	Status          TaskStatus        `json:"status"`
	TriggerPluginID string            `json:"triggerPluginId"` // 触发方插件 ID，用于回调
	CurrentNodeID   string            `json:"currentNodeId"`
	Context         map[string]any    `json:"context"`   // 流水线累积变量，每步完成后 merge
	LoopState       map[string]int    `json:"loopState"` // key: nodeId，value: 当前迭代次数
	FailReason      string            `json:"failReason,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// ─── Dispatch 消息 Envelope ───────────────────────────────────────────────────

// DispatchEnvelope 是 dispatch 消息的标准 body 格式
type DispatchEnvelope struct {
	TaskID   string          `json:"taskId,omitempty"`
	MsgID    string          `json:"msgId"`
	UserID   string          `json:"userId"`
	PluginID string          `json:"pluginId"` // 触发方插件 ID
	Event    string          `json:"event"`    // eventType
	Payload  json.RawMessage `json:"payload"`
}

// NodeResultEnvelope 是节点完成后返回给 Router 的消息格式
// 节点通过 dispatch 发送，带上 taskId 和 stepOutput
type NodeResultEnvelope struct {
	TaskID    string          `json:"taskId"`
	MsgID     string          `json:"msgId"`
	UserID    string          `json:"userId"`
	PluginID  string          `json:"pluginId"`  // 执行节点的插件 ID
	NodeID    string          `json:"nodeId"`    // 刚完成的节点 ID
	Ok        bool            `json:"ok"`        // false 表示节点执行失败
	Output    json.RawMessage `json:"output"`    // 节点输出 payload，merge 进 Task.Context
	FailReason string         `json:"failReason,omitempty"`
}

// ─── Schema 缓存（存 BoltDB schemas bucket）──────────────────────────────────

// CachedSchema 是从 griffino-schemas 仓库拉取并解析后缓存的端口信息
type CachedSchema struct {
	InterfaceRef string      `json:"interfaceRef"` // 如 griffino.interfaces.ai.chat@1.0.0
	InputPorts   []PortSpec  `json:"inputPorts"`
	OutputPorts  []PortSpec  `json:"outputPorts"`
	FetchedAt    time.Time   `json:"fetchedAt"`
}

type PortSpec struct {
	ID          string `json:"id"`
	Type        string `json:"type"`        // text/float/int/bool/audio/image/llm 等
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}