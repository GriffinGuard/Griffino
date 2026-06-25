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

// ─── Blueprint (stored in BoltDB, persisted permanently) ────────────────────

type Blueprint struct {
	ID        string        `json:"id"`
	UserID    string        `json:"userId"`
	Name      string        `json:"name"`
	Trigger   Trigger       `json:"trigger"`
	Nodes     []Node        `json:"nodes"`
	Metadata  BlueprintMeta `json:"metadata,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type Trigger struct {
	EventType string `json:"eventType"`
	PluginID  string `json:"pluginId,omitempty"` // Empty means any source / 为空表示任意来源
}

type Node struct {
	ID                  string   `json:"id"`
	PluginID            string   `json:"pluginId"`            // "__builtin__" for built-in nodes / 表示内置节点
	CapabilityID        string   `json:"capabilityId"`        // Built-in: if/loop/input/output / 内置节点
	RequestTopicPattern string   `json:"requestTopicPattern"` // Built-in nodes reuse this for expressions/variable declarations / 内置节点复用此字段存表达式/变量声明
	NextNodes           []string `json:"nextNodes"`
	TimeoutMs           int      `json:"timeoutMs,omitempty"`
}

// BlueprintMeta is maintained by the frontend; the backend stores it opaquely and the execution engine ignores it / 由前端维护，后端透传存储，执行引擎忽略
type BlueprintMeta struct {
	FlowPositions map[string]FlowPosition `json:"flowPositions,omitempty"`
}

type FlowPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ─── Built-in node constants ────────────────────────────────────────────────

const (
	BuiltinPluginID = "__builtin__"

	// CapabilityID values / CapabilityID 值
	BuiltinCapIF     = "if"
	BuiltinCapLoop   = "loop"
	BuiltinCapInput  = "input"
	BuiltinCapOutput = "output"
	BuiltinCapJoin   = "join" // Parallel merge: wait for all incoming branches, then continue / 并行汇合

	// INPUT node system built-in source prefix / INPUT 节点系统内置 source 前缀
	BuiltinSourceNow    = "__builtin__.now"
	BuiltinSourceUserID = "__builtin__.userId"
)

// ─── Task (instance stored in Redis, TTL 24h) ───────────────────────────────

type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID              string         `json:"id"`
	BlueprintID     string         `json:"blueprintId"`
	UserID          string         `json:"userId"`
	DisplayName     string         `json:"displayName,omitempty"`
	Status          TaskStatus     `json:"status"`
	TriggerPluginID string         `json:"triggerPluginId"` // Triggering plugin ID, used for callbacks / 触发方插件 ID，用于回调
	CurrentNodeID   string         `json:"currentNodeId"`   // Most recently advanced node (best-effort, observation-only; not unique in parallel) / 最近推进到的节点
	Context         map[string]any `json:"context"`         // Pipeline accumulated variables, merged after each step (concurrent writes: last-write-wins) / 流水线累积变量
	LoopState       map[string]int `json:"loopState"`       // key: nodeId, value: current iteration count / 当前迭代次数

	// ─── Parallel execution state (W5) ──────────────────────────────────────
	// ActiveBranches: number of currently live parallel branches. Starts at 1 when a Task is created;
	// += N-1 when a node fans out to N NextNodes;
	// -= 1 when a branch reaches a terminal leaf, triggering completeTask when it hits zero;
	// net -= k-1 when a join absorbs k incoming edges / 当前存活的并行分支数
	ActiveBranches int `json:"activeBranches"`
	// ActiveNodes: in-flight plugin node → its reply deadline. Replaces the old single Deadline;
	// in parallel, multiple in-flight nodes can coexist. The watchdog scans this table, marking the Task as timed out if any entry expires / 在途插件节点及其回复截止时间
	ActiveNodes map[string]time.Time `json:"activeNodes,omitempty"`
	// JoinState: join node ID → current arrival count. Resets to 0 when arrivals reach the static in-degree,
	// supporting join re-entry inside loops / join 节点 ID → 当前已到达的分支数
	JoinState map[string]int `json:"joinState,omitempty"`

	FailReason string    `json:"failReason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ─── Dispatch message envelope ──────────────────────────────────────────────

// DispatchEnvelope is the standard body format for dispatch messages / dispatch 消息的标准 body 格式
type DispatchEnvelope struct {
	TaskID      string          `json:"taskId,omitempty"`
	MsgID       string          `json:"msgId"`
	UserID      string          `json:"userId"`
	DisplayName string          `json:"displayName,omitempty"`
	PluginID    string          `json:"pluginId"` // Triggering plugin ID / 触发方插件 ID
	Event       string          `json:"event"`    // eventType
	Payload     json.RawMessage `json:"payload"`
}

// NodeResultEnvelope is the message format a node returns to the Router upon completion.
// Sent via dispatch with taskId and stepOutput / 节点完成后返回给 Router 的消息格式
type NodeResultEnvelope struct {
	TaskID      string          `json:"taskId"`
	MsgID       string          `json:"msgId"`
	UserID      string          `json:"userId"`
	DisplayName string          `json:"displayName,omitempty"`
	PluginID    string          `json:"pluginId"` // Plugin ID of the executing node / 执行节点的插件 ID
	NodeID      string          `json:"nodeId"`   // ID of the node that just completed / 刚完成的节点 ID
	Ok          bool            `json:"ok"`       // false means the node execution failed / false 表示节点执行失败
	Output      json.RawMessage `json:"output"`   // Node output payload, merged into Task.Context / 节点输出 payload
	FailReason  string          `json:"failReason,omitempty"`
}

// ─── Schema cache (stored in BoltDB schemas bucket) ─────────────────────────

// CachedSchema is port info pulled from the griffino-schemas repo and cached after parsing / 从 griffino-schemas 仓库拉取并解析后缓存的端口信息
type CachedSchema struct {
	InterfaceRef string     `json:"interfaceRef"` // e.g. griffino.interfaces.ai.chat@1.0.0 / 如 griffino.interfaces.ai.chat@1.0.0
	InputPorts   []PortSpec `json:"inputPorts"`
	OutputPorts  []PortSpec `json:"outputPorts"`
	FetchedAt    time.Time  `json:"fetchedAt"`
}

type PortSpec struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // text/float/int/bool/audio/image/llm etc. / text/float/int/bool/audio/image/llm 等
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}
