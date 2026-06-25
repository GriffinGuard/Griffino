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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Build graphs using only built-in output/join nodes, avoiding the executePluginNode AMQP send path,
// so the entire graph can be driven synchronously in unit tests without RabbitMQ / 仅用内置 output/join 节点搭图，避免 AMQP 发送路径

// TestFanOutJoin_Completes verifies: A fans out to B and C, merges at join J,
// then J continues with a single branch, and the Task completes with zero active branches / 验证 fan-out 到 B、C 两条分支汇合后 Task 完成且活跃分支归零
func TestFanOutJoin_Completes(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}

	out := func(id string, next ...string) Node {
		return Node{ID: id, PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput,
			RequestTopicPattern: "result:manual", NextNodes: next}
	}
	bp := &Blueprint{ID: "bp1", Nodes: []Node{
		out("A", "B", "C"),
		out("B", "J"),
		out("C", "J"),
		{ID: "J", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapJoin, NextNodes: []string{"D"}},
		out("D"), // Terminal / 终端
	}}

	task := &Task{
		ID: "t1", UserID: "u1", Status: TaskStatusRunning,
		Context:        map[string]any{"result": "ok"},
		ActiveBranches: 1, ActiveNodes: map[string]time.Time{}, JoinState: map[string]int{},
		CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Start fan-out from entry A; all built-in nodes, runs synchronously / 从入口 A 开始 fan-out，全程内置节点同步跑完
	if err := s.handleFanOut(task, bp, &bp.Nodes[0]); err != nil {
		t.Fatalf("handleFanOut: %v", err)
	}

	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s (branches=%d)", got.Status, got.ActiveBranches)
	}
	if got.ActiveBranches != 0 {
		t.Fatalf("expected 0 active branches, got %d", got.ActiveBranches)
	}
}

// TestJoin_WaitsForAllBranches verifies: when only one branch arrives at the join, the branch is absorbed,
// the Task stays running without advancing, and the join count is 1 / 验证只有一条分支到达 join 时分支被吸收
func TestJoin_WaitsForAllBranches(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}

	bp := &Blueprint{ID: "bp1", Nodes: []Node{
		{ID: "B", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput,
			RequestTopicPattern: "result:manual", NextNodes: []string{"J"}},
		{ID: "C", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput,
			RequestTopicPattern: "result:manual", NextNodes: []string{"J"}},
		{ID: "J", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapJoin, NextNodes: []string{"D"}},
		{ID: "D", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput,
			RequestTopicPattern: "result:manual"},
	}}

	// Simulate fan-out already happened: two branches running / 模拟 fan-out 已发生，两条分支在跑
	task := &Task{
		ID: "t1", UserID: "u1", Status: TaskStatusRunning,
		Context:        map[string]any{"result": "ok"},
		ActiveBranches: 2, ActiveNodes: map[string]time.Time{}, JoinState: map[string]int{},
		CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Only let branch B arrive at join / 只让分支 B 到达 join
	if err := s.handleFanOut(task, bp, &bp.Nodes[0]); err != nil {
		t.Fatalf("handleFanOut B: %v", err)
	}

	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusRunning {
		t.Fatalf("expected still running while join waits, got %s", got.Status)
	}
	if got.ActiveBranches != 1 {
		t.Fatalf("expected 1 active branch after absorbing one, got %d", got.ActiveBranches)
	}
	if got.JoinState["J"] != 1 {
		t.Fatalf("expected join arrival count 1, got %d", got.JoinState["J"])
	}
}

// TestArriveAtJoin_Concurrent verifies correctness of concurrent join arrivals under optimistic locking:
// When expected=N branches arrive concurrently, exactly one gets proceed=true, and active branches converge to 1 / 验证乐观锁下并发到达 join 的正确性
func TestArriveAtJoin_Concurrent(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	const n = 16

	task := &Task{
		ID: "t1", UserID: "u1", Status: TaskStatusRunning,
		ActiveBranches: n, JoinState: map[string]int{}, CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	var proceedCount int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			proceed, err := ts.ArriveAtJoin(ctx, "t1", "J", n)
			if err != nil {
				t.Errorf("ArriveAtJoin: %v", err)
				return
			}
			if proceed {
				atomic.AddInt32(&proceedCount, 1)
			}
		}()
	}
	wg.Wait()

	if proceedCount != 1 {
		t.Fatalf("expected exactly 1 branch to proceed past join, got %d", proceedCount)
	}
	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ActiveBranches != 1 {
		t.Fatalf("expected active branches to converge to 1, got %d", got.ActiveBranches)
	}
	if got.JoinState["J"] != 0 {
		t.Fatalf("expected join counter reset to 0 after completion, got %d", got.JoinState["J"])
	}
}

// TestEndBranch_Concurrent verifies no count loss when branches end concurrently:
// When N branches end concurrently, exactly one sees remaining==0, and active branches converge to 0 / 验证并发结束分支时计数不丢失
func TestEndBranch_Concurrent(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	const n = 16

	task := &Task{
		ID: "t1", UserID: "u1", Status: TaskStatusRunning,
		ActiveBranches: n, CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	var lastCount int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			remaining, err := ts.EndBranch(ctx, "t1")
			if err != nil {
				t.Errorf("EndBranch: %v", err)
				return
			}
			if remaining == 0 {
				atomic.AddInt32(&lastCount, 1)
			}
		}()
	}
	wg.Wait()

	if lastCount != 1 {
		t.Fatalf("expected exactly 1 goroutine to observe last branch, got %d", lastCount)
	}
	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ActiveBranches != 0 {
		t.Fatalf("expected 0 active branches, got %d", got.ActiveBranches)
	}
}

// TestMarkTerminal_SingleFire verifies that when terminal state is triggered concurrently, only one wins,
// ensuring sendCallback fires exactly once / 验证并发触发终态时只有一个赢家，sendCallback 只发一次
func TestMarkTerminal_SingleFire(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	const n = 16

	task := &Task{ID: "t1", UserID: "u1", Status: TaskStatusRunning, CreatedAt: time.Now()}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	var changedCount int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, _, err := ts.MarkTerminal(ctx, "t1", TaskStatusFailed, "boom")
			if err != nil {
				t.Errorf("MarkTerminal: %v", err)
				return
			}
			if changed {
				atomic.AddInt32(&changedCount, 1)
			}
		}()
	}
	wg.Wait()

	if changedCount != 1 {
		t.Fatalf("expected exactly 1 terminal transition, got %d", changedCount)
	}
	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Fatalf("expected failed, got %s", got.Status)
	}
}

// TestArriveAtJoin_Reentry verifies that join count resets after completion, supporting repeated merges inside loops / 验证 join 计数在完成后复位，支持循环体内重复汇合
func TestArriveAtJoin_Reentry(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()

	task := &Task{
		ID: "t1", UserID: "u1", Status: TaskStatusRunning,
		ActiveBranches: 2, JoinState: map[string]int{}, CreatedAt: time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	// First round: two branches arrive, the second proceeds / 第一轮：两条分支到达，第二条 proceed
	if p, _ := ts.ArriveAtJoin(ctx, "t1", "J", 2); p {
		t.Fatal("first arrival should not proceed")
	}
	if p, _ := ts.ArriveAtJoin(ctx, "t1", "J", 2); !p {
		t.Fatal("second arrival should proceed")
	}

	// Count reset; the second round (loop re-entry) should work the same way / 计数已复位，第二轮（循环重入）应同样工作
	if p, _ := ts.ArriveAtJoin(ctx, "t1", "J", 2); p {
		t.Fatal("first arrival of second round should not proceed (counter not reset?)")
	}
	if p, _ := ts.ArriveAtJoin(ctx, "t1", "J", 2); !p {
		t.Fatal("second arrival of second round should proceed")
	}
}

// TestCountInEdges verifies join in-degree counting / 验证 join 入度统计
func TestCountInEdges(t *testing.T) {
	bp := &Blueprint{Nodes: []Node{
		{ID: "A", NextNodes: []string{"J"}},
		{ID: "B", NextNodes: []string{"J"}},
		{ID: "C", NextNodes: []string{"J", "D"}},
		{ID: "J", NextNodes: []string{"E"}},
		{ID: "D"},
		{ID: "E"},
	}}
	if got := countInEdges(bp, "J"); got != 3 {
		t.Fatalf("expected in-degree 3 for J, got %d", got)
	}
	if got := countInEdges(bp, "E"); got != 1 {
		t.Fatalf("expected in-degree 1 for E, got %d", got)
	}
	if got := countInEdges(bp, "A"); got != 0 {
		t.Fatalf("expected in-degree 0 for A, got %d", got)
	}
}
