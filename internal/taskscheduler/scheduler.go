package taskscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	dispatchExchange = "griffino.router"
	pluginsExchange  = "griffino.plugins"
	taskCompletedEvent = "task.completed"
	taskFailedEvent    = "task.failed"
)

// Scheduler 负责处理 dispatch 消息，驱动 Blueprint 执行
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

// HandleDispatch 是 Router 收到 dispatch 消息后的入口
// 由 Router 的 dispatch 消费者 goroutine 调用
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
		// 无 taskId：新触发，查找匹配的 Blueprint 并创建 Task
		err = s.handleNewDispatch(env)
	} else {
		// 有 taskId：蓝图节点执行完成，推进到下一步
		err = s.handleNodeResult(env)
	}

	if err != nil {
		slog.Error("taskscheduler: dispatch handling failed", "error", err, "taskId", env.TaskID)
	}
	msg.Ack(false)
}

// ─── 新触发：创建 Task ────────────────────────────────────────────────────────

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

	// 每个匹配的蓝图各创建一个独立 Task，并行触发
	for _, bp := range blueprints {
		if err := s.createAndStartTask(bp, env); err != nil {
			slog.Error("taskscheduler: failed to start task",
				"blueprintId", bp.ID, "error", err)
			// 单个蓝图失败不影响其他蓝图
		}
	}
	return nil
}

func (s *Scheduler) createAndStartTask(bp *Blueprint, env DispatchEnvelope) error {
	if len(bp.Nodes) == 0 {
		slog.Warn("taskscheduler: blueprint has no nodes, skipping", "blueprintId", bp.ID)
		return nil
	}

	// 初始化流水线 context：把 dispatch payload 解析后作为初始变量
	initialCtx := make(map[string]any)
	if len(env.Payload) > 0 {
		_ = json.Unmarshal(env.Payload, &initialCtx)
	}

	// 注入系统内置变量
	initialCtx["__userId"] = env.UserID
	initialCtx["__triggerEvent"] = env.Event
	initialCtx["__now"] = time.Now().UTC().Format(time.RFC3339)

	task := &Task{
		ID:              uuid.New().String(),
		BlueprintID:     bp.ID,
		UserID:          env.UserID,
		Status:          TaskStatusRunning,
		TriggerPluginID: env.PluginID,
		CurrentNodeID:   bp.Nodes[0].ID,
		Context:         initialCtx,
		LoopState:       make(map[string]int),
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

// ─── 节点完成：推进 Task ──────────────────────────────────────────────────────

func (s *Scheduler) handleNodeResult(env DispatchEnvelope) error {
	// 节点完成时，env.Event 是 "node.result" 内部事件
	// env.Payload 是 NodeResultEnvelope 的 JSON
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

	// 节点执行失败
	if !result.Ok {
		slog.Warn("taskscheduler: node failed",
			"taskId", task.ID, "nodeId", result.NodeID, "reason", result.FailReason)
		return s.failTask(task, result.FailReason)
	}

	// merge 节点输出到 Task.Context
	if len(result.Output) > 0 {
		var output map[string]any
		if err := json.Unmarshal(result.Output, &output); err == nil {
			if err := s.taskStore.MergeContext(s.ctx, task.ID, output); err != nil {
				return fmt.Errorf("merge context: %w", err)
			}
			// 本地也 merge，后续执行当前节点的 next 时能用到最新 context
			for k, v := range output {
				task.Context[k] = v
			}
		}
	}

	// 找到刚完成的节点
	completedNode := findNode(bp, result.NodeID)
	if completedNode == nil {
		return fmt.Errorf("node not found in blueprint: %s", result.NodeID)
	}

	return s.advanceTask(task, bp, completedNode)
}

// ─── 核心：节点推进逻辑 ───────────────────────────────────────────────────────

// advanceTask 根据当前节点的类型和 nextNodes 决定下一步走向
func (s *Scheduler) advanceTask(task *Task, bp *Blueprint, current *Node) error {
	switch {
	case current.PluginID == BuiltinPluginID && current.CapabilityID == BuiltinCapIF:
		return s.handleIFNode(task, bp, current)
	case current.PluginID == BuiltinPluginID && current.CapabilityID == BuiltinCapLoop:
		return s.handleLoopNode(task, bp, current)
	default:
		return s.handleLinearNext(task, bp, current)
	}
}

// handleLinearNext 线性执行：取 nextNodes[0]，没有则 Task 完成
func (s *Scheduler) handleLinearNext(task *Task, bp *Blueprint, current *Node) error {
	if len(current.NextNodes) == 0 {
		return s.completeTask(task)
	}
	nextNode := findNode(bp, current.NextNodes[0])
	if nextNode == nil {
		return s.failTask(task, fmt.Sprintf("next node not found: %s", current.NextNodes[0]))
	}
	if err := s.taskStore.AdvanceNode(s.ctx, task.ID, nextNode.ID); err != nil {
		return err
	}
	task.CurrentNodeID = nextNode.ID
	return s.executeNode(task, bp, nextNode, nil)
}

// handleIFNode IF 节点：对表达式求值，true 走 nextNodes[0]，false 走 nextNodes[1]
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

// handleLoopNode LOOP 节点：固定次数或条件循环
// nextNodes[0]：循环体入口，nextNodes[1]：循环完成出口
func (s *Scheduler) handleLoopNode(task *Task, bp *Blueprint, node *Node) error {
	if len(node.NextNodes) < 2 {
		return s.failTask(task, "LOOP node requires exactly 2 nextNodes (body, exit)")
	}

	expr_ := strings.TrimSpace(node.RequestTopicPattern)
	shouldContinue := false

	// 判断是固定次数还是条件表达式
	if count, err := strconv.Atoi(expr_); err == nil {
		// 固定次数循环
		current, err := s.taskStore.IncrLoopCount(s.ctx, task.ID, node.ID)
		if err != nil {
			return err
		}
		// IncrLoopCount 在执行前递增，所以 current <= count 表示还需要继续
		shouldContinue = current <= count
		slog.Info("taskscheduler: LOOP fixed count",
			"taskId", task.ID, "nodeId", node.ID,
			"current", current, "max", count, "continue", shouldContinue)
	} else {
		// 条件表达式循环
		result, err := evalExpr(expr_, task.Context)
		if err != nil {
			return s.failTask(task, fmt.Sprintf("LOOP expression eval failed: %v", err))
		}
		shouldContinue = result
		slog.Info("taskscheduler: LOOP condition",
			"taskId", task.ID, "nodeId", node.ID, "continue", shouldContinue)
	}

	var nextNodeID string
	if shouldContinue {
		nextNodeID = node.NextNodes[0] // 循环体
	} else {
		nextNodeID = node.NextNodes[1] // 出口
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

// ─── 节点执行 ─────────────────────────────────────────────────────────────────

// executeNode 根据节点类型分发执行
func (s *Scheduler) executeNode(task *Task, bp *Blueprint, node *Node, triggerPayload json.RawMessage) error {
	if node.PluginID == BuiltinPluginID {
		return s.executeBuiltinNode(task, bp, node)
	}
	return s.executePluginNode(task, node, triggerPayload)
}

// executeBuiltinNode 执行内置控制流节点
func (s *Scheduler) executeBuiltinNode(task *Task, bp *Blueprint, node *Node) error {
	switch node.CapabilityID {
	case BuiltinCapInput:
		return s.executeInputNode(task, bp, node)
	case BuiltinCapOutput:
		return s.executeOutputNode(task, bp, node)
	case BuiltinCapIF, BuiltinCapLoop:
		// IF/LOOP 节点在节点"完成"时由 advanceTask 处理，不需要发消息给插件
		// 直接在此处驱动（视为立即完成）
		return s.advanceTask(task, bp, node)
	default:
		return s.failTask(task, fmt.Sprintf("unknown builtin capabilityId: %s", node.CapabilityID))
	}
}

// executeInputNode INPUT 节点：注入变量到 context，然后继续
func (s *Scheduler) executeInputNode(task *Task, bp *Blueprint, node *Node) error {
	// requestTopicPattern 格式：varName:sourceType
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
		// 手动输入值：从 Task.Context 里找同名变量（触发时由 payload 注入）
		val, ok := task.Context[varName]
		if !ok {
			return s.failTask(task, fmt.Sprintf("INPUT node: variable '%s' not found in context", varName))
		}
		value = val
	}

	if err := s.taskStore.MergeContext(s.ctx, task.ID, map[string]any{varName: value}); err != nil {
		return err
	}
	task.Context[varName] = value

	slog.Info("taskscheduler: INPUT node resolved",
		"taskId", task.ID, "varName", varName, "sourceType", sourceType)

	return s.handleLinearNext(task, bp, node)
}

// executeOutputNode OUTPUT 节点：记录输出变量，然后继续
func (s *Scheduler) executeOutputNode(task *Task, bp *Blueprint, node *Node) error {
	parts := strings.SplitN(node.RequestTopicPattern, ":", 2)
	if len(parts) != 2 {
		return s.failTask(task, fmt.Sprintf("OUTPUT node invalid format: %s", node.RequestTopicPattern))
	}
	varName := parts[0]
	val := task.Context[varName]

	slog.Info("taskscheduler: OUTPUT node",
		"taskId", task.ID, "varName", varName, "value", val)

	// OUTPUT 节点把变量记录到 context 的 __output 命名空间
	outputs, _ := task.Context["__outputs"].(map[string]any)
	if outputs == nil {
		outputs = make(map[string]any)
	}
	outputs[varName] = val
	if err := s.taskStore.MergeContext(s.ctx, task.ID, map[string]any{"__outputs": outputs}); err != nil {
		return err
	}
	task.Context["__outputs"] = outputs

	return s.handleLinearNext(task, bp, node)
}

// executePluginNode 向插件发送任务消息，等待插件完成后通过 dispatch 回调
func (s *Scheduler) executePluginNode(task *Task, node *Node, triggerPayload json.RawMessage) error {
	// 构造发给插件的消息 body
	// 插件收到后处理，完成后 dispatch 一个带 taskId 的消息回主系统
	body := map[string]any{
		"taskId":  task.ID,
		"msgId":   uuid.New().String(),
		"userId":  task.UserID,
		"nodeId":  node.ID,
		"context": task.Context,
	}

	// 首个节点使用触发时的原始 payload 作为输入
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
				"x-griffino-task-id": task.ID,
				"x-griffino-node-id": node.ID,
				"x-griffino-user-id": task.UserID,
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

// ─── Task 终态处理 ────────────────────────────────────────────────────────────

func (s *Scheduler) completeTask(task *Task) error {
	slog.Info("taskscheduler: task completed", "taskId", task.ID)
	if err := s.taskStore.UpdateStatus(s.ctx, task.ID, TaskStatusCompleted, ""); err != nil {
		return err
	}
	return s.sendCallback(task, taskCompletedEvent)
}

func (s *Scheduler) failTask(task *Task, reason string) error {
	slog.Warn("taskscheduler: task failed", "taskId", task.ID, "reason", reason)
	if err := s.taskStore.UpdateStatus(s.ctx, task.ID, TaskStatusFailed, reason); err != nil {
		return err
	}
	return s.sendCallback(task, taskFailedEvent)
}

// sendCallback 向触发方插件发送 Task 完成/失败的回调消息
func (s *Scheduler) sendCallback(task *Task, event string) error {
	if task.TriggerPluginID == "" {
		return nil
	}

	// 获取最新 Task 状态（含 context 和 outputs）
	latest, err := s.taskStore.Get(s.ctx, task.ID)
	if err != nil {
		slog.Warn("taskscheduler: failed to get task for callback", "taskId", task.ID, "error", err)
		latest = task
	}

	payload := map[string]any{
		"taskId":    task.ID,
		"status":    latest.Status,
		"outputs":   latest.Context["__outputs"],
		"failReason": latest.FailReason,
	}
	payloadData, _ := json.Marshal(payload)

	// 回调 topic：插件通过 @client.consumer(event="task.completed/task.failed") 监听
	// 队列命名：plugin.{pluginId}.consumer.{eventName}
	callbackQueue := fmt.Sprintf("plugin.%s.consumer.%s",
		task.TriggerPluginID,
		strings.ReplaceAll(event, ".", "_"))

	body := map[string]any{
		"taskId":  task.ID,
		"msgId":   uuid.New().String(),
		"userId":  task.UserID,
		"event":   event,
		"payload": json.RawMessage(payloadData),
	}
	data, _ := json.Marshal(body)

	err = s.amqpCh.PublishWithContext(
		s.ctx,
		"",           // 默认 exchange，直接发到队列
		callbackQueue,
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        data,
		},
	)
	if err != nil {
		// 回调失败不影响 Task 状态，只记录日志
		slog.Warn("taskscheduler: callback publish failed",
			"taskId", task.ID, "queue", callbackQueue, "error", err)
	}
	return nil
}

// ─── 健康检查接口 ─────────────────────────────────────────────────────────────

// FailTasksByPlugin 由 health.go 调用，当插件容器挂掉时批量标记相关 Task 失败
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

// ─── 工具函数 ─────────────────────────────────────────────────────────────────

func findNode(bp *Blueprint, nodeID string) *Node {
	for i := range bp.Nodes {
		if bp.Nodes[i].ID == nodeID {
			return &bp.Nodes[i]
		}
	}
	return nil
}

// evalExpr 使用 expr 库对表达式求值，返回 bool 结果
// ctx 是流水线 context，表达式可以访问其中的所有变量
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