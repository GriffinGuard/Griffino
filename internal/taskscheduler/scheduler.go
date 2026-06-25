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
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/GriffinGuard/Griffino/internal/metrics"
	"github.com/expr-lang/expr"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	dispatchExchange   = "griffino.router"
	pluginsExchange    = "griffino.plugins"
	taskCompletedEvent = "task.completed"
	taskFailedEvent    = "task.failed"

	// maxLoopIterations: safety cap for conditional-expression loops, preventing infinite loops from bad/malicious expressions / 条件表达式循环的安全上限，防止错误/恶意表达式造成无限循环
	maxLoopIterations = 10000

	// defaultNodeTimeout: default reply timeout for plugin nodes that don't declare TimeoutMs / 插件节点未声明 TimeoutMs 时的默认回复超时
	defaultNodeTimeout = 5 * time.Minute
	// watchdogInterval: interval at which the watchdog scans running Tasks / 看门狗扫描运行中 Task 的周期
	watchdogInterval = 30 * time.Second
)

// Scheduler processes dispatch messages and drives Blueprint execution / 负责处理 dispatch 消息，驱动 Blueprint 执行
type Scheduler struct {
	bpStore   *BlueprintStore
	taskStore *TaskStore
	amqpConn  *amqp.Connection
	amqpCh    *amqp.Channel
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewScheduler(
	bpStore *BlueprintStore,
	taskStore *TaskStore,
	amqpConn *amqp.Connection,
) (*Scheduler, error) {
	ch, err := amqpConn.Channel()
	if err != nil {
		return nil, fmt.Errorf("taskscheduler: failed to create channel: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		bpStore:   bpStore,
		taskStore: taskStore,
		amqpConn:  amqpConn,
		amqpCh:    ch,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (s *Scheduler) Stop() {
	s.cancel()
	if s.amqpCh != nil {
		s.amqpCh.Close()
	}
}

// HandleDispatch is the entry point when the Router receives a dispatch message.
// Called by the Router's dispatch consumer goroutine / Router 收到 dispatch 消息后的入口，由 dispatch 消费者 goroutine 调用
func (s *Scheduler) HandleDispatch(msg amqp.Delivery) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("taskscheduler: panic recovered", "error", rec)
			msg.Nack(false, false)
		}
	}()

	var env DispatchEnvelope
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		slog.Warn("taskscheduler: failed to parse dispatch envelope", "error", err)
		msg.Ack(false)
		return
	}

	slog.Info("taskscheduler: received dispatch",
		"taskId", env.TaskID,
		"userId", env.UserID,
		"pluginId", env.PluginID,
		"event", env.Event)

	var err error
	if env.TaskID == "" {
		// No taskId: new trigger, find matching Blueprint and create a Task / 无 taskId：新触发，查找匹配的 Blueprint 并创建 Task
		err = s.handleNewDispatch(env)
	} else {
		// Has taskId: a blueprint node finished executing, advance to next step / 有 taskId：蓝图节点执行完成，推进到下一步
		err = s.handleNodeResult(env)
	}

	if err != nil {
		slog.Error("taskscheduler: dispatch handling failed", "error", err, "taskId", env.TaskID)
	}
	msg.Ack(false)
}

// ─── New trigger: create Task ────────────────────────────────────────────────

func (s *Scheduler) handleNewDispatch(env DispatchEnvelope) error {
	blueprints, err := s.bpStore.FindByTrigger(env.UserID, env.Event, env.PluginID)
	if err != nil {
		return fmt.Errorf("find blueprints: %w", err)
	}
	if len(blueprints) == 0 {
		slog.Debug("taskscheduler: no blueprints matched, discarding",
			"userId", env.UserID, "event", env.Event)
		return nil
	}

	// Create one independent Task per matching Blueprint, triggering in parallel / 每个匹配的蓝图各创建一个独立 Task，并行触发
	for _, bp := range blueprints {
		if err := s.createAndStartTask(bp, env); err != nil {
			slog.Error("taskscheduler: failed to start task",
				"blueprintId", bp.ID, "error", err)
			// One blueprint failing doesn't affect others / 单个蓝图失败不影响其他蓝图
		}
	}
	return nil
}

func (s *Scheduler) createAndStartTask(bp *Blueprint, env DispatchEnvelope) error {
	if len(bp.Nodes) == 0 {
		slog.Warn("taskscheduler: blueprint has no nodes, skipping", "blueprintId", bp.ID)
		return nil
	}

	// Initialize pipeline context: parse dispatch payload as initial variables / 初始化流水线 context：把 dispatch payload 解析后作为初始变量
	initialCtx := make(map[string]any)
	if len(env.Payload) > 0 {
		_ = json.Unmarshal(env.Payload, &initialCtx)
	}

	// Inject system built-in variables / 注入系统内置变量
	initialCtx["__userId"] = env.UserID
	initialCtx["__displayName"] = env.DisplayName
	initialCtx["__triggerEvent"] = env.Event
	initialCtx["__now"] = time.Now().UTC().Format(time.RFC3339)

	task := &Task{
		ID:              uuid.New().String(),
		BlueprintID:     bp.ID,
		UserID:          env.UserID,
		DisplayName:     env.DisplayName,
		Status:          TaskStatusRunning,
		TriggerPluginID: env.PluginID,
		CurrentNodeID:   bp.Nodes[0].ID,
		Context:         initialCtx,
		LoopState:       make(map[string]int),
		ActiveBranches:  1, // Entry node is the first branch / 入口节点即第一个分支
		ActiveNodes:     make(map[string]time.Time),
		JoinState:       make(map[string]int),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.taskStore.Save(s.ctx, task); err != nil {
		return fmt.Errorf("save task: %w", err)
	}

	slog.Info("taskscheduler: task created",
		"taskId", task.ID,
		"blueprintId", bp.ID,
		"firstNode", bp.Nodes[0].ID)

	return s.executeNode(task, bp, &bp.Nodes[0], env.Payload)
}

// ─── Node completion: advance Task ──────────────────────────────────────────

func (s *Scheduler) handleNodeResult(env DispatchEnvelope) error {
	// On node completion, env.Event is the "node.result" internal event / 节点完成时，env.Event 是 "node.result" 内部事件
	// env.Payload is the NodeResultEnvelope JSON / env.Payload 是 NodeResultEnvelope 的 JSON
	var result NodeResultEnvelope
	if err := json.Unmarshal(env.Payload, &result); err != nil {
		return fmt.Errorf("parse node result: %w", err)
	}

	task, err := s.taskStore.Get(s.ctx, result.TaskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task.Status != TaskStatusRunning {
		slog.Warn("taskscheduler: task is no longer running, ignoring result",
			"taskId", task.ID, "status", task.Status)
		return nil
	}

	bp, err := s.bpStore.Get(task.BlueprintID)
	if err != nil {
		return fmt.Errorf("get blueprint: %w", err)
	}

	// Node execution failed / 节点执行失败
	if !result.Ok {
		slog.Warn("taskscheduler: node failed",
			"taskId", task.ID, "nodeId", result.NodeID, "reason", result.FailReason)
		return s.failTask(task, result.FailReason)
	}

	// This plugin node is done; remove from in-flight set (watchdog won't time it out anymore).
	// The returned latest is the snapshot after removal; other branches may have modified the Task concurrently / 该插件节点已完成，从在途集合移除，看门狗不再计超时
	latest, err := s.taskStore.RemoveActiveNode(s.ctx, task.ID, result.NodeID)
	if err != nil {
		return fmt.Errorf("remove active node: %w", err)
	}
	task = latest

	// Merge node output into Task.Context (atomic; concurrent writes to the same key use last-write-wins) / merge 节点输出到 Task.Context，原子，多分支并发写同 key 为 last-write-wins
	if len(result.Output) > 0 {
		var output map[string]any
		if err := json.Unmarshal(result.Output, &output); err != nil {
			return s.failTask(task, fmt.Sprintf("invalid node output: %v", err))
		}
		merged, err := s.taskStore.MergeContext(s.ctx, task.ID, output)
		if err != nil {
			return fmt.Errorf("merge context: %w", err)
		}
		task = merged // Use latest snapshot so subsequent branches see all merged context / 用最新快照，后续分支推进能看到所有已合并的 context
	}

	// Find the node that just completed / 找到刚完成的节点
	completedNode := findNode(bp, result.NodeID)
	if completedNode == nil {
		return fmt.Errorf("node not found in blueprint: %s", result.NodeID)
	}

	return s.advanceTask(task, bp, completedNode)
}

// ─── Core: node advance logic ────────────────────────────────────────────────

// advanceTask decides the next step based on the current node type and nextNodes / 根据当前节点的类型和 nextNodes 决定下一步走向
func (s *Scheduler) advanceTask(task *Task, bp *Blueprint, current *Node) error {
	switch {
	case current.PluginID == BuiltinPluginID && current.CapabilityID == BuiltinCapIF:
		return s.handleIFNode(task, bp, current)
	case current.PluginID == BuiltinPluginID && current.CapabilityID == BuiltinCapLoop:
		return s.handleLoopNode(task, bp, current)
	default:
		return s.handleFanOut(task, bp, current)
	}
}

// handleFanOut advances to all NextNodes of the current node:
//   - 0 next: this branch reaches a terminal leaf, ends the branch; Task completes when branches reach zero;
//   - 1 next: linear advance (branch count unchanged);
//   - N next: fan-out, active branches += N-1, dispatch all branches in parallel / 推进到所有 NextNodes
//
// IF/LOOP nodes don't go through here (they pick one branch internally, handled by their own handlers) / IF/LOOP 节点不走这里
func (s *Scheduler) handleFanOut(task *Task, bp *Blueprint, current *Node) error {
	if len(current.NextNodes) == 0 {
		// Terminal leaf: end this branch; Task completes when the last branch ends / 终端叶子：结束本分支，最后一个分支结束时完成 Task
		remaining, err := s.taskStore.EndBranch(s.ctx, task.ID)
		if err != nil {
			return err
		}
		if remaining <= 0 {
			return s.completeTask(task)
		}
		slog.Debug("taskscheduler: branch ended, others still running",
			"taskId", task.ID, "nodeId", current.ID, "remainingBranches", remaining)
		return nil
	}

	// Parse and validate all next nodes first, avoiding partial dispatch on invalid nodes / 先解析并校验所有 next 节点，避免部分派发后才发现非法节点
	nextNodes := make([]*Node, 0, len(current.NextNodes))
	for _, id := range current.NextNodes {
		n := findNode(bp, id)
		if n == nil {
			return s.failTask(task, fmt.Sprintf("next node not found: %s", id))
		}
		nextNodes = append(nextNodes, n)
	}

	// fan-out: N branches, active branches += N-1 (must complete atomically before dispatching any branch,
	// otherwise an early-completing branch might judge the Task done before the count is updated) / fan-out 时活跃分支数 += N-1，必须原子完成
	if len(nextNodes) > 1 {
		if _, err := s.taskStore.AddBranches(s.ctx, task.ID, len(nextNodes)-1); err != nil {
			return err
		}
		slog.Info("taskscheduler: fan-out", "taskId", task.ID,
			"nodeId", current.ID, "branches", len(nextNodes))
	}

	for _, nextNode := range nextNodes {
		if err := s.taskStore.AdvanceNode(s.ctx, task.ID, nextNode.ID); err != nil {
			return err
		}
		task.CurrentNodeID = nextNode.ID
		if err := s.executeNode(task, bp, nextNode, nil); err != nil {
			return err
		}
	}
	return nil
}

// handleJoinNode handles a branch arriving at a JOIN node: waits for all incoming edges before proceeding.
// expected = blueprint static in-degree (count of nodes whose NextNodes contain this join).
// Note: join should be paired with fan-out (normal node's multiple NextNodes); don't put join
// downstream of an IF that discards a branch, or the missing edge will never arrive / 处理分支到达 JOIN 节点，等待所有入边分支
func (s *Scheduler) handleJoinNode(task *Task, bp *Blueprint, node *Node) error {
	expected := countInEdges(bp, node.ID)
	if expected <= 1 {
		// In-degree 0 or 1: no merge needed, pass through directly / 入度 0 或 1，无需汇合，直接透传继续
		return s.handleFanOut(task, bp, node)
	}

	proceed, err := s.taskStore.ArriveAtJoin(s.ctx, task.ID, node.ID, expected)
	if err != nil {
		return err
	}
	if !proceed {
		slog.Debug("taskscheduler: branch arrived at join, waiting for others",
			"taskId", task.ID, "joinId", node.ID, "expected", expected)
		return nil // This branch is absorbed; the last arrival will continue / 本分支被吸收，等待最后一个到达者继续
	}

	slog.Info("taskscheduler: join complete, all branches merged",
		"taskId", task.ID, "joinId", node.ID, "expected", expected)
	// Continue as the sole surviving branch; join itself advances its NextNodes per fan-out rules / 作为唯一幸存分支继续，join 自身按 fan-out 规则推进 NextNodes
	return s.handleFanOut(task, bp, node)
}

// handleIFNode: IF node — evaluates the expression; true goes to nextNodes[0], false goes to nextNodes[1] / IF 节点，对表达式求值
func (s *Scheduler) handleIFNode(task *Task, bp *Blueprint, node *Node) error {
	if len(node.NextNodes) < 2 {
		return s.failTask(task, "IF node requires exactly 2 nextNodes (true branch, false branch)")
	}

	result, err := evalExpr(node.RequestTopicPattern, task.Context)
	if err != nil {
		return s.failTask(task, fmt.Sprintf("IF expression eval failed: %v", err))
	}

	var nextNodeID string
	if result {
		nextNodeID = node.NextNodes[0]
		slog.Info("taskscheduler: IF branch taken", "taskId", task.ID, "branch", "true")
	} else {
		nextNodeID = node.NextNodes[1]
		slog.Info("taskscheduler: IF branch taken", "taskId", task.ID, "branch", "false")
	}

	if nextNodeID == "" {
		return s.completeTask(task)
	}
	nextNode := findNode(bp, nextNodeID)
	if nextNode == nil {
		return s.failTask(task, fmt.Sprintf("IF branch node not found: %s", nextNodeID))
	}
	if err := s.taskStore.AdvanceNode(s.ctx, task.ID, nextNode.ID); err != nil {
		return err
	}
	task.CurrentNodeID = nextNode.ID
	return s.executeNode(task, bp, nextNode, nil)
}

// handleLoopNode: LOOP node — fixed-count or conditional loop.
// nextNodes[0] is the loop body entry, nextNodes[1] is the loop exit / LOOP 节点，固定次数或条件循环
func (s *Scheduler) handleLoopNode(task *Task, bp *Blueprint, node *Node) error {
	if len(node.NextNodes) < 2 {
		return s.failTask(task, "LOOP node requires exactly 2 nextNodes (body, exit)")
	}

	expr_ := strings.TrimSpace(node.RequestTopicPattern)
	shouldContinue := false

	// Determine whether it's a fixed count or a conditional expression / 判断是固定次数还是条件表达式
	if count, err := strconv.Atoi(expr_); err == nil {
		// Fixed-count loop / 固定次数循环
		current, err := s.taskStore.IncrLoopCount(s.ctx, task.ID, node.ID)
		if err != nil {
			return err
		}
		// IncrLoopCount increments before execution, so current <= count means we need to continue / IncrLoopCount 在执行前递增，所以 current <= count 表示还需要继续
		shouldContinue = current <= count
		slog.Info("taskscheduler: LOOP fixed count",
			"taskId", task.ID, "nodeId", node.ID,
			"current", current, "max", count, "continue", shouldContinue)
	} else {
		// Conditional expression loop: increment iteration count and validate safety limit to prevent infinite loops / 条件表达式循环，先递增迭代计数并校验安全上限
		iter, err := s.taskStore.IncrLoopCount(s.ctx, task.ID, node.ID)
		if err != nil {
			return err
		}
		if iter > maxLoopIterations {
			return s.failTask(task, fmt.Sprintf("LOOP exceeded max iterations (%d)", maxLoopIterations))
		}
		result, err := evalExpr(expr_, task.Context)
		if err != nil {
			return s.failTask(task, fmt.Sprintf("LOOP expression eval failed: %v", err))
		}
		shouldContinue = result
		slog.Info("taskscheduler: LOOP condition",
			"taskId", task.ID, "nodeId", node.ID, "iter", iter, "continue", shouldContinue)
	}

	var nextNodeID string
	if shouldContinue {
		nextNodeID = node.NextNodes[0] // Loop body / 循环体
	} else {
		nextNodeID = node.NextNodes[1] // Loop exit / 出口
	}

	if nextNodeID == "" {
		return s.completeTask(task)
	}
	nextNode := findNode(bp, nextNodeID)
	if nextNode == nil {
		return s.failTask(task, fmt.Sprintf("LOOP next node not found: %s", nextNodeID))
	}
	if err := s.taskStore.AdvanceNode(s.ctx, task.ID, nextNode.ID); err != nil {
		return err
	}
	task.CurrentNodeID = nextNode.ID
	return s.executeNode(task, bp, nextNode, nil)
}

// ─── Node execution ─────────────────────────────────────────────────────────────

// executeNode dispatches execution based on node type / 按节点类型分发执行.
func (s *Scheduler) executeNode(task *Task, bp *Blueprint, node *Node, triggerPayload json.RawMessage) error {
	if node.PluginID == BuiltinPluginID {
		return s.executeBuiltinNode(task, bp, node)
	}
	return s.executePluginNode(task, node, triggerPayload)
}

// executeBuiltinNode executes built-in control-flow nodes / 执行内置控制流节点
func (s *Scheduler) executeBuiltinNode(task *Task, bp *Blueprint, node *Node) error {
	switch node.CapabilityID {
	case BuiltinCapInput:
		return s.executeInputNode(task, bp, node)
	case BuiltinCapOutput:
		return s.executeOutputNode(task, bp, node)
	case BuiltinCapJoin:
		// JOIN node: tally arrivals on merge, don't send messages to the plugin / JOIN 节点：分支到达时做汇合记账，不发消息给插件
		return s.handleJoinNode(task, bp, node)
	case BuiltinCapIF, BuiltinCapLoop:
		// IF/LOOP nodes are handled by advanceTask when "completed"; no message to the plugin needed / IF/LOOP 节点在完成时由 advanceTask 处理，不需要发消息给插件
		// drive it right here (treated as immediately complete) / 直接在此驱动（视为立即完成）
		return s.advanceTask(task, bp, node)
	default:
		return s.failTask(task, fmt.Sprintf("unknown builtin capabilityId: %s", node.CapabilityID))
	}
}

// executeInputNode handles an INPUT node: inject variables into context, then continue / INPUT 节点：注入变量到 context 后继续.
func (s *Scheduler) executeInputNode(task *Task, bp *Blueprint, node *Node) error {
	// requestTopicPattern format: varName:sourceType / requestTopicPattern 格式：varName:sourceType
	parts := strings.SplitN(node.RequestTopicPattern, ":", 2)
	if len(parts) != 2 {
		return s.failTask(task, fmt.Sprintf("INPUT node invalid format: %s", node.RequestTopicPattern))
	}
	varName := parts[0]
	sourceType := parts[1]

	var value any
	switch sourceType {
	case BuiltinSourceNow:
		value = time.Now().UTC().Format(time.RFC3339)
	case BuiltinSourceUserID:
		value = task.UserID
	default:
		// Manual input value: find a variable of the same name in Task.Context (injected by payload at trigger time) / 手动输入值，从 Task.Context 里找同名变量（触发时由 payload 注入）
		val, ok := task.Context[varName]
		if !ok {
			return s.failTask(task, fmt.Sprintf("INPUT node: variable '%s' not found in context", varName))
		}
		value = val
	}

	merged, err := s.taskStore.MergeContext(s.ctx, task.ID, map[string]any{varName: value})
	if err != nil {
		return err
	}
	task = merged

	slog.Info("taskscheduler: INPUT node resolved",
		"taskId", task.ID, "varName", varName, "sourceType", sourceType)

	return s.handleFanOut(task, bp, node)
}

// executeOutputNode handles an OUTPUT node: record output variables, then continue / OUTPUT 节点：记录输出变量后继续.
func (s *Scheduler) executeOutputNode(task *Task, bp *Blueprint, node *Node) error {
	parts := strings.SplitN(node.RequestTopicPattern, ":", 2)
	if len(parts) != 2 {
		return s.failTask(task, fmt.Sprintf("OUTPUT node invalid format: %s", node.RequestTopicPattern))
	}
	varName := parts[0]
	val := task.Context[varName]

	slog.Info("taskscheduler: OUTPUT node",
		"taskId", task.ID, "varName", varName, "value", val)

	// OUTPUT node records the variable into context's __outputs namespace / OUTPUT 节点把变量记录到 context 的 __outputs 命名空间
	outputs, _ := task.Context["__outputs"].(map[string]any)
	if outputs == nil {
		outputs = make(map[string]any)
	}
	outputs[varName] = val
	merged, err := s.taskStore.MergeContext(s.ctx, task.ID, map[string]any{"__outputs": outputs})
	if err != nil {
		return err
	}
	task = merged

	return s.handleFanOut(task, bp, node)
}

// executePluginNode sends a task message to the plugin and waits for the plugin to finish
// and call back via dispatch / 向插件发送任务消息，等其完成后通过 dispatch 回调.
func (s *Scheduler) executePluginNode(task *Task, node *Node, triggerPayload json.RawMessage) error {
	// Build the message body to send to the plugin / 构造发给插件的消息 body
	// Plugin processes then dispatches a message with taskId back to the main system / 插件收到后处理，完成后 dispatch 一个带 taskId 的消息回主系统
	body := map[string]any{
		"taskId":      task.ID,
		"msgId":       uuid.New().String(),
		"userId":      task.UserID,
		"displayName": task.DisplayName,
		"nodeId":      node.ID,
		"context":     task.Context,
	}

	// First node uses the original trigger payload as input / 首个节点使用触发时的原始 payload 作为输入
	if triggerPayload != nil {
		body["payload"] = triggerPayload
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal node message: %w", err)
	}

	expiration := "60000" // 默认 60s
	if node.TimeoutMs > 0 {
		expiration = strconv.Itoa(node.TimeoutMs)
	}

	// Register in-flight plugin node and its reply deadline for watchdog timeout detection.
	// In parallel mode, multiple in-flight nodes can exist concurrently, each with independent timeouts / 登记在途插件节点及其回复截止时间，看门狗检测插件无响应导致的卡死
	timeout := defaultNodeTimeout
	if node.TimeoutMs > 0 {
		timeout = time.Duration(node.TimeoutMs) * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	if err := s.taskStore.AddActiveNode(s.ctx, task.ID, node.ID, deadline); err != nil {
		slog.Warn("taskscheduler: failed to register active node", "taskId", task.ID, "error", err)
	}

	err = s.amqpCh.PublishWithContext(
		s.ctx,
		pluginsExchange,
		node.RequestTopicPattern,
		false, false,
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: uuid.New().String(),
			Body:          data,
			Expiration:    expiration,
			Headers: amqp.Table{
				"x-griffino-task-id":      task.ID,
				"x-griffino-node-id":      node.ID,
				"x-griffino-user-id":      task.UserID,
				"x-griffino-display-name": task.DisplayName,
			},
		},
	)
	if err != nil {
		return s.failTask(task, fmt.Sprintf("publish to plugin failed: %v", err))
	}

	slog.Info("taskscheduler: dispatched to plugin node",
		"taskId", task.ID,
		"nodeId", node.ID,
		"topic", node.RequestTopicPattern)

	return nil
}

// ─── Task terminal-state handling / Task 终态处理 ─────────────────────────────

func (s *Scheduler) completeTask(task *Task) error {
	changed, _, err := s.taskStore.MarkTerminal(s.ctx, task.ID, TaskStatusCompleted, "")
	if err != nil {
		return err
	}
	if !changed {
		// already marked terminal by another branch/watchdog; the callback fires only once / 已被其他分支/看门狗置为终态，回调只发一次
		return nil
	}
	metrics.TaskRun("ok", time.Since(task.CreatedAt).Seconds())
	slog.Info("taskscheduler: task completed", "taskId", task.ID)
	return s.sendCallback(task, taskCompletedEvent)
}

func (s *Scheduler) failTask(task *Task, reason string) error {
	changed, _, err := s.taskStore.MarkTerminal(s.ctx, task.ID, TaskStatusFailed, reason)
	if err != nil {
		return err
	}
	if !changed {
		// already terminal (another branch failed or completed first); don't repeat the callback / 已是终态，不重复回调
		return nil
	}
	// Distinguish watchdog timeout from normal failure in reporting: reason "node timeout" means it's a timeout / 看门狗超时与普通失败区分上报，reason 为 "node timeout" 即超时
	result := "error"
	if reason == "node timeout" {
		result = "timeout"
	}
	metrics.TaskRun(result, time.Since(task.CreatedAt).Seconds())
	slog.Warn("taskscheduler: task failed", "taskId", task.ID, "reason", reason)
	return s.sendCallback(task, taskFailedEvent)
}

// sendCallback sends the Task completed/failed callback message to the triggering plugin / 向触发方插件发送完成/失败回调.
func (s *Scheduler) sendCallback(task *Task, event string) error {
	if task.TriggerPluginID == "" {
		return nil
	}

	// Get latest Task state (including context and outputs) / 获取最新 Task 状态（含 context 和 outputs）
	latest, err := s.taskStore.Get(s.ctx, task.ID)
	if err != nil {
		slog.Warn("taskscheduler: failed to get task for callback", "taskId", task.ID, "error", err)
		latest = task
	}

	payload := map[string]any{
		"taskId":     task.ID,
		"status":     latest.Status,
		"outputs":    latest.Context["__outputs"],
		"failReason": latest.FailReason,
	}
	payloadData, _ := json.Marshal(payload)

	// Callback topic: plugins listen via @client.consumer(event="task.completed/task.failed")
	// Queue naming: plugin.{pluginId}.consumer.{eventName} / 回调 topic 和队列命名规则
	callbackQueue := fmt.Sprintf("plugin.%s.consumer.%s",
		task.TriggerPluginID,
		strings.ReplaceAll(event, ".", "_"))

	body := map[string]any{
		"taskId":      task.ID,
		"msgId":       uuid.New().String(),
		"userId":      task.UserID,
		"displayName": task.DisplayName,
		"pluginId":    task.TriggerPluginID,
		"event":       event,
		"payload":     json.RawMessage(payloadData),
	}
	data, _ := json.Marshal(body)

	err = s.amqpCh.PublishWithContext(
		s.ctx,
		"", // Default exchange, send directly to queue name / 默认 exchange，直接发到队列
		callbackQueue,
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        data,
		},
	)
	if err != nil {
		// Callback failure doesn't affect Task state; just log it / 回调失败不影响 Task 状态，只记录日志
		slog.Warn("taskscheduler: callback publish failed",
			"taskId", task.ID, "queue", callbackQueue, "error", err)
	}
	return nil
}

// ─── Health-check interface / 健康检查接口 ────────────────────────────────────

// FailTasksByPlugin is called by health.go to bulk-mark related Tasks failed when a
// plugin container goes down / 插件容器挂掉时批量标记相关 Task 失败.
func (s *Scheduler) FailTasksByPlugin(pluginID string) {
	tasks, err := s.taskStore.FindRunningByPlugin(s.ctx, pluginID)
	if err != nil {
		slog.Error("taskscheduler: FailTasksByPlugin scan failed", "pluginId", pluginID, "error", err)
		return
	}
	for _, task := range tasks {
		slog.Warn("taskscheduler: failing task due to plugin down",
			"taskId", task.ID, "pluginId", pluginID)
		if err := s.failTask(task, fmt.Sprintf("plugin %s is down", pluginID)); err != nil {
			slog.Error("taskscheduler: failed to fail task", "taskId", task.ID, "error", err)
		}
	}
}

// ─── Timeout watchdog / 超时看门狗 ────────────────────────────────────────────

// StartWatchdog starts a background goroutine that periodically scans running Tasks and
// marks as failed any Task past its node reply deadline (Deadline), so an unresponsive
// plugin can't leave a Task stuck in running (until the 24h TTL). It exits when the passed
// ctx is cancelled.
// 启动后台 goroutine，周期扫描运行中的 Task，将已过节点回复截止时间的标记为失败；随 ctx 取消退出。
func (s *Scheduler) StartWatchdog(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(watchdogInterval)
		defer ticker.Stop()
		slog.Info("taskscheduler: watchdog started", "interval", watchdogInterval)
		for {
			select {
			case <-ctx.Done():
				slog.Info("taskscheduler: watchdog stopped")
				return
			case <-ticker.C:
				s.sweepExpiredTasks(ctx)
			}
		}
	}()
}

// sweepExpiredTasks scans running Tasks and marks those past their Deadline as failed / 扫描运行中 Task，对已过 Deadline 的标记失败.
func (s *Scheduler) sweepExpiredTasks(ctx context.Context) {
	tasks, err := s.taskStore.ListRunning(ctx)
	if err != nil {
		slog.Error("taskscheduler: watchdog scan failed", "error", err)
		return
	}
	now := time.Now()
	for _, task := range tasks {
		// Scan all in-flight plugin nodes; if any is past its reply deadline, the Task is
		// timed out. Under parallelism there may be several in-flight nodes, so one stuck
		// branch fails the whole Task.
		// 扫描所有在途插件节点，任一已过回复截止时间即判 Task 超时；并行下一个分支卡死即整个 Task 失败。
		timedOutNode, deadline, expired := earliestExpired(task.ActiveNodes, now)
		if !expired {
			continue
		}
		slog.Warn("taskscheduler: node timeout, failing task",
			"taskId", task.ID, "nodeId", timedOutNode, "deadline", deadline)
		if err := s.failTask(task, "node timeout"); err != nil {
			slog.Error("taskscheduler: watchdog failTask error", "taskId", task.ID, "error", err)
		}
	}
}

// earliestExpired finds the earliest expired deadline in the in-flight node table.
// It returns that node's ID, its deadline, and whether any expired node exists.
// 在在途节点表中找出最早过期的 deadline，返回节点 ID、deadline 及是否存在过期节点。
func earliestExpired(active map[string]time.Time, now time.Time) (nodeID string, deadline time.Time, expired bool) {
	for id, dl := range active {
		if dl.IsZero() || now.Before(dl) {
			continue
		}
		if !expired || dl.Before(deadline) {
			nodeID, deadline, expired = id, dl, true
		}
	}
	return nodeID, deadline, expired
}

// ─── Utility functions / 工具函数 ─────────────────────────────────────────────

func findNode(bp *Blueprint, nodeID string) *Node {
	for i := range bp.Nodes {
		if bp.Nodes[i].ID == nodeID {
			return &bp.Nodes[i]
		}
	}
	return nil
}

// countInEdges counts how many nodes in the blueprint have NextNodes pointing at nodeID,
// i.e. that node's static in-degree. A JOIN node uses this to know how many incoming
// branches to wait for.
// 统计有多少节点的 NextNodes 指向 nodeID（静态入度）；JOIN 节点据此确定要等多少条入边分支。
func countInEdges(bp *Blueprint, nodeID string) int {
	count := 0
	for i := range bp.Nodes {
		for _, next := range bp.Nodes[i].NextNodes {
			if next == nodeID {
				count++
			}
		}
	}
	return count
}

// evalExpr evaluates an expression with the expr library and returns a bool result.
// ctx is the pipeline context; the expression can access all variables in it.
// 用 expr 库对表达式求值返回 bool；ctx 为流水线 context，表达式可访问其中所有变量。
func evalExpr(expression string, ctx map[string]any) (bool, error) {
	program, err := expr.Compile(expression, expr.Env(ctx), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("compile expression '%s': %w", expression, err)
	}
	result, err := expr.Run(program, ctx)
	if err != nil {
		return false, fmt.Errorf("run expression '%s': %w", expression, err)
	}
	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression '%s' did not return bool", expression)
	}
	return b, nil
}
