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
	"context"
	"testing"
	"time"
)

// TestHandleLoopNode_MaxIterations verifies that a conditional loop exceeding the safety cap marks the Task as failed / 验证条件循环超过安全上限时 Task 被标记失败
func TestHandleLoopNode_MaxIterations(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}

	loopNode := Node{
		ID:                  "loop1",
		PluginID:            BuiltinPluginID,
		CapabilityID:        BuiltinCapLoop,
		RequestTopicPattern: "x > 0", // Conditional expression (non-integer) → take the conditional loop branch / 条件表达式（非整数）→ 走条件循环分支
		NextNodes:           []string{"body", "exit"},
	}
	bp := &Blueprint{ID: "bp1", Nodes: []Node{loopNode}}

	// Pre-set iteration count at the limit, TriggerPluginID left empty so sendCallback won't send AMQP / 预置迭代计数已达上限，TriggerPluginID 留空使 sendCallback 不发 AMQP
	task := &Task{
		ID:        "task1",
		UserID:    "u1",
		Status:    TaskStatusRunning,
		Context:   map[string]any{},
		LoopState: map[string]int{"loop1": maxLoopIterations},
		CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	if err := s.handleLoopNode(task, bp, &loopNode); err != nil {
		t.Fatalf("handleLoopNode returned error: %v", err)
	}

	got, err := ts.Get(ctx, "task1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Fatalf("expected task to be failed after exceeding max iterations, got %s", got.Status)
	}
}

// TestHandleLoopNode_ConditionContinues verifies that when under the limit and condition is true, the loop body is entered / 验证未达上限且条件为真时进入循环体
func TestHandleLoopNode_ConditionFalseExits(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}

	loopNode := Node{
		ID:                  "loop1",
		PluginID:            BuiltinPluginID,
		CapabilityID:        BuiltinCapLoop,
		RequestTopicPattern: "count > 10",
		NextNodes:           []string{"body", "exit"},
	}
	// exit is a regular plugin node (not built-in output) that would try to send a message; use a no-next built-in output to end / exit 是内置 output 之外的普通插件节点
	exitNode := Node{ID: "exit", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput,
		RequestTopicPattern: "result:manual", NextNodes: nil}
	bp := &Blueprint{ID: "bp1", Nodes: []Node{loopNode, exitNode}}

	task := &Task{
		ID:        "task1",
		UserID:    "u1",
		Status:    TaskStatusRunning,
		Context:   map[string]any{"count": 0, "result": "done"},
		LoopState: map[string]int{},
		CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	// count(0) > 10 is false → take exit branch, Task completes / count(0) > 10 为 false → 走 exit 分支，Task 完成
	if err := s.handleLoopNode(task, bp, &loopNode); err != nil {
		t.Fatalf("handleLoopNode returned error: %v", err)
	}

	got, err := ts.Get(ctx, "task1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("expected task completed via exit branch, got %s", got.Status)
	}
}

// TestSweepExpiredTasks verifies that the watchdog marks expired Tasks as failed and keeps unexpired/no-deadline Tasks / 验证看门狗将过期 Task 标记失败、保留未过期/无截止时间的 Task
func TestSweepExpiredTasks(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}
	now := time.Now()

	cases := []*Task{
		{ID: "expired", UserID: "u1", Status: TaskStatusRunning,
			ActiveNodes: map[string]time.Time{"n1": now.Add(-time.Minute)}, CreatedAt: now},
		{ID: "future", UserID: "u1", Status: TaskStatusRunning,
			ActiveNodes: map[string]time.Time{"n1": now.Add(time.Hour)}, CreatedAt: now},
		{ID: "nodeadline", UserID: "u1", Status: TaskStatusRunning, CreatedAt: now},
	}
	for _, tk := range cases {
		if err := ts.Save(ctx, tk); err != nil {
			t.Fatalf("save %s: %v", tk.ID, err)
		}
	}

	s.sweepExpiredTasks(ctx)

	want := map[string]TaskStatus{
		"expired":    TaskStatusFailed,
		"future":     TaskStatusRunning,
		"nodeadline": TaskStatusRunning,
	}
	for id, status := range want {
		got, err := ts.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status != status {
			t.Fatalf("task %s: expected status %s, got %s", id, status, got.Status)
		}
	}
}
