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

package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GriffinGuard/Griffino/internal/metrics"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	routerExchange         = "griffino.router"
	actionsExchange        = "griffino.actions"
	invokeQueueName        = "griffino.router.invoke.inbox"
	dispatchQueueName      = "griffino.router.dispatch.inbox"
	invokeBindingPattern   = "invoke.griffino.router.#"
	dispatchBindingPattern = "dispatch.griffino.router.#"

	// providerReplyTimeout is the max wait for a single provider's reply; on timeout, switch to the next candidate / 单个 provider 的回复等待上限；超时则切换到下一个候选
	providerReplyTimeout = 60 * time.Second
)

type ProviderEntry struct {
	ProviderID    string `json:"providerId"`
	ProviderTopic string `json:"providerTopic"`
	Weight        int    `json:"weight"`
	// InterfaceRef is the provider's standard interface (e.g.
	// "griffino.interfaces.ai.chat@1.0.0"), filled server-side from the provider's
	// manifest. Providers are treated as interchangeable only when their interface
	// major versions are compatible / provider 的标准接口，用于互换前的主版本兼容校验.
	InterfaceRef string `json:"interfaceRef,omitempty"`
}

// Scheduler abstracts the taskscheduler package so Router depends on an interface, avoiding circular imports / taskscheduler 包的抽象，Router 通过接口依赖，避免循环引用
type Scheduler interface {
	HandleDispatch(msg amqp.Delivery)
}

// ProviderHealthChecker abstracts provider health checks, avoiding a direct Router→store dependency.
// Conservative semantics: only returns false when a provider is definitively unhealthy; unknown/undetermined cases are treated as healthy / 抽象 provider 健康状态查询，保守语义：仅确定不健康时返回 false，未知一律视为健康
type ProviderHealthChecker interface {
	IsProviderHealthy(providerID string) bool
}

type Route struct {
	PluginID       string          `json:"pluginId"`
	Slot           string          `json:"slot,omitempty"`
	CapabilityType string          `json:"capabilityType"`
	Providers      []ProviderEntry `json:"providers"`
	Strategy       string          `json:"strategy"` // "fallback" | "round_robin"
}

type Router struct {
	rdb     *redis.Client
	amqpURL string // kept for automatic reconnection / 保存用于断线自动重连

	// chMu guards amqpConn/amqpCh/dispatchCh, which are swapped on reconnect.
	// Publishers read the current channel pointer under RLock and use it without
	// holding the lock during I/O; a stale (closed) channel simply errors out and
	// is handled as a normal failure / 保护连接与 channel 指针，重连时整体替换.
	chMu       sync.RWMutex
	amqpConn   *amqp.Connection
	amqpCh     *amqp.Channel // invoke-dedicated channel / invoke 专用 channel
	dispatchCh *amqp.Channel // dispatch-dedicated channel / dispatch 专用 channel
	scheduler  Scheduler     // dispatch message handler interface / dispatch 消息处理器接口
	ctx        context.Context
	cancel     context.CancelFunc

	// round_robin in-process counter, keyed by route signature (single-node; avoids a Redis round-trip per message) / round_robin 进程内计数器，key 为 route 签名，单机场景避免每条消息一次 Redis 往返
	rrMu       sync.Mutex
	rrCounters map[string]*atomic.Uint64

	health ProviderHealthChecker // Can be nil (no health filtering) / 可为 nil（不做健康过滤）
}

// SetHealthChecker injects a provider health checker; nil means no health filtering / 注入 provider 健康检查器，nil 表示不做健康过滤
func (r *Router) SetHealthChecker(h ProviderHealthChecker) {
	r.health = h
}

func New(redisAddr, password string, scheduler Scheduler) *Router {
	ctx, cancel := context.WithCancel(context.Background())
	return &Router{
		rdb: redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: password,
		}),
		scheduler:  scheduler,
		ctx:        ctx,
		cancel:     cancel,
		rrCounters: make(map[string]*atomic.Uint64),
	}
}

func (r *Router) Start(amqpURL string) error {
	r.amqpURL = amqpURL

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("router: failed to connect to RabbitMQ: %w", err)
	}
	r.chMu.Lock()
	r.amqpConn = conn
	r.chMu.Unlock()

	if err := r.setupTopology(); err != nil {
		conn.Close()
		return err
	}

	slog.Info("router started",
		"invoke_pattern", invokeBindingPattern,
		"dispatch_pattern", dispatchBindingPattern)

	// Supervise the connection and reconnect automatically if it drops /
	// 监督连接，断线自动重连.
	go r.superviseConnection()
	return nil
}

// setupTopology (re)creates the channels, declares the exchanges/queues/bindings,
// starts the consumers, and launches the processing goroutines on the current
// connection. It is called by Start and on every reconnect; all declarations are
// idempotent / 在当前连接上建立 channel/exchange/queue/binding 与消费者，初次启动与每次重连都调用.
func (r *Router) setupTopology() error {
	r.chMu.RLock()
	conn := r.amqpConn
	r.chMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("router: no amqp connection")
	}

	amqpCh, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("router: failed to create invoke channel: %w", err)
	}
	dispatchCh, err := conn.Channel()
	if err != nil {
		amqpCh.Close()
		return fmt.Errorf("router: failed to create dispatch channel: %w", err)
	}

	// On any failure below, close both channels to avoid leaking them / 失败时关闭两个 channel 避免泄漏.
	setup := func() error {
		// Declare exchange (idempotent) / 声明 exchange（幂等）
		for _, ch := range []*amqp.Channel{amqpCh, dispatchCh} {
			if err := ch.ExchangeDeclare(routerExchange, "topic", true, false, false, false, nil); err != nil {
				return fmt.Errorf("router: failed to declare exchange: %w", err)
			}
		}
		// Declare actions exchange (for plugin action triggers) / 声明 actions exchange.
		if err := amqpCh.ExchangeDeclare(actionsExchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("router: failed to declare actions exchange: %w", err)
		}

		// Invoke queue / invoke 队列
		invokeQ, err := amqpCh.QueueDeclare(invokeQueueName, true, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("router: failed to declare invoke queue: %w", err)
		}
		if err := amqpCh.QueueBind(invokeQ.Name, invokeBindingPattern, routerExchange, false, nil); err != nil {
			return fmt.Errorf("router: failed to bind invoke queue: %w", err)
		}

		// Dispatch queue / dispatch 队列
		dispatchQ, err := dispatchCh.QueueDeclare(dispatchQueueName, true, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("router: failed to declare dispatch queue: %w", err)
		}
		if err := dispatchCh.QueueBind(dispatchQ.Name, dispatchBindingPattern, routerExchange, false, nil); err != nil {
			return fmt.Errorf("router: failed to bind dispatch queue: %w", err)
		}

		// Start consumers / 启动消费者
		invokeMsgs, err := amqpCh.Consume(invokeQ.Name, "griffino-router-invoke", false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("router: failed to start invoke consumer: %w", err)
		}
		dispatchMsgs, err := dispatchCh.Consume(dispatchQ.Name, "griffino-router-dispatch", false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("router: failed to start dispatch consumer: %w", err)
		}

		// Publish the new channels atomically, then start the per-channel readers.
		// The previous readers (if any) exit on their own when their old delivery
		// channel closes / 原子替换 channel 后启动读取 goroutine，旧 goroutine 在旧 channel 关闭时自行退出.
		r.chMu.Lock()
		r.amqpCh = amqpCh
		r.dispatchCh = dispatchCh
		r.chMu.Unlock()

		go r.processInvokeMessages(invokeMsgs)
		go r.processDispatchMessages(dispatchMsgs)
		return nil
	}

	if err := setup(); err != nil {
		amqpCh.Close()
		dispatchCh.Close()
		return err
	}
	return nil
}

// superviseConnection watches the current connection and reconnects when it drops
// unexpectedly. It exits on graceful close or when the router is stopped / 监督连接，意外断开时重连.
func (r *Router) superviseConnection() {
	for {
		r.chMu.RLock()
		conn := r.amqpConn
		r.chMu.RUnlock()
		if conn == nil {
			return
		}
		closeCh := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-r.ctx.Done():
			return
		case amqpErr := <-closeCh:
			if amqpErr == nil {
				return // graceful close (Stop) / 主动关闭
			}
			slog.Warn("router: amqp connection lost, reconnecting", "error", amqpErr)
			if !r.reconnect() {
				return // router stopped during reconnect / 重连期间被停止
			}
			slog.Info("router: amqp reconnected")
		}
	}
}

// reconnect redials with exponential backoff and rebuilds the topology. It returns
// false if the router is stopped before reconnection succeeds / 指数退避重拨并重建拓扑.
func (r *Router) reconnect() bool {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-r.ctx.Done():
			return false
		case <-time.After(backoff):
		}

		conn, err := amqp.Dial(r.amqpURL)
		if err != nil {
			slog.Warn("router: redial failed", "error", err, "retryIn", backoff)
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}
		r.chMu.Lock()
		r.amqpConn = conn
		r.chMu.Unlock()

		if err := r.setupTopology(); err != nil {
			slog.Warn("router: topology re-setup failed after reconnect", "error", err)
			conn.Close()
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// invokeCh returns the current invoke channel under a read lock. Callers use the
// returned pointer without holding the lock; if it is nil or has been closed by a
// concurrent reconnect, the publish/consume operation errors and is handled as a
// normal failure / 返回当前 invoke channel 指针，调用方不持锁使用.
func (r *Router) invokeCh() *amqp.Channel {
	r.chMu.RLock()
	defer r.chMu.RUnlock()
	return r.amqpCh
}

func (r *Router) Stop() {
	r.cancel()
	r.chMu.Lock()
	defer r.chMu.Unlock()
	if r.amqpCh != nil {
		r.amqpCh.Close()
	}
	if r.dispatchCh != nil {
		r.dispatchCh.Close()
	}
	if r.amqpConn != nil {
		r.amqpConn.Close()
	}
}

func (r *Router) processInvokeMessages(msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			go r.handleMessage(msg)
		}
	}
}

func (r *Router) processDispatchMessages(msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			// Pass dispatch message to scheduler, which starts a goroutine internally / dispatch 消息交给 scheduler 处理，scheduler 内部起 goroutine
			go r.scheduler.HandleDispatch(msg)
		}
	}
}

func (r *Router) handleMessage(msg amqp.Delivery) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("router: panic recovered", "error", rec)
			r.replyError(msg, "internal router error")
			msg.Nack(false, false)
		}
	}()

	slog.Info("router received message",
		"routing_key", msg.RoutingKey,
		"correlation_id", msg.CorrelationId,
		"reply_to", msg.ReplyTo,
		"body_len", len(msg.Body))

	parts := strings.Split(msg.RoutingKey, ".")
	// invoke.griffino.router.{userId}.{capabilityType}.v1
	// Minimum: invoke(0) griffino(1) router(2) userId(3) capType(4) v1(5) = 6 segments / 最少 6 段
	if len(parts) < 6 {
		slog.Warn("router: invalid routing key", "parts", len(parts), "key", msg.RoutingKey)
		r.replyError(msg, "invalid routing key format")
		metrics.RouterMessage("dropped")
		msg.Ack(false)
		return
	}

	userID := parts[3]
	// parts[4:len(parts)-1] removes the fixed prefix and trailing v1; everything in between is capabilityType / 去掉首部固定段和末尾 v1，剩余全部是 capabilityType
	capabilityType := strings.Join(parts[4:len(parts)-1], ".")

	// Parse pluginId and slot from body / 从 body 解析 pluginId 和 slot
	var envelope struct {
		PluginID string `json:"pluginId"`
		Slot     string `json:"slot"`
	}
	if err := json.Unmarshal(msg.Body, &envelope); err != nil {
		slog.Warn("router: failed to parse message body", "error", err)
		r.replyError(msg, "invalid message body")
		metrics.RouterMessage("dropped")
		msg.Ack(false)
		return
	}

	slog.Info("router: parsed message",
		"userID", userID,
		"capabilityType", capabilityType,
		"pluginId", envelope.PluginID,
		"slot", envelope.Slot)

	route, err := r.findRoute(userID, envelope.PluginID, capabilityType, envelope.Slot)
	if err != nil {
		slog.Warn("router: route lookup failed",
			"userId", userID, "capabilityType", capabilityType, "error", err)
		r.replyError(msg, fmt.Sprintf("no route configured for capability: %s", capabilityType))
		metrics.RouterMessage("error")
		msg.Ack(false)
		return
	}
	if route == nil {
		slog.Warn("router: no route found",
			"userId", userID,
			"pluginId", envelope.PluginID,
			"capabilityType", capabilityType,
			"slot", envelope.Slot)
		r.replyError(msg, fmt.Sprintf("slot not configured: pluginId=%s capabilityType=%s slot=%s",
			envelope.PluginID, capabilityType, envelope.Slot))
		metrics.RouterMessage("dropped")
		msg.Ack(false)
		return
	}

	providers := r.orderedProviders(route)
	if len(providers) == 0 {
		slog.Warn("router: no provider available", "route", route)
		r.replyError(msg, "no provider available")
		metrics.RouterMessage("dropped")
		msg.Ack(false)
		return
	}

	ctx, span := otel.Tracer("griffino-router").Start(r.ctx, "router.forward",
		trace.WithAttributes(
			attribute.String("plugin.id", route.PluginID),
			attribute.String("capability.type", route.CapabilityType),
		))
	defer span.End()

	start := time.Now()
	result := r.forwardWithFailover(ctx, msg, route, providers)
	metrics.ObserveRouterMessage(time.Since(start).Seconds())
	metrics.RouterMessage(result)
	msg.Ack(false)
}

func (r *Router) findAllRoutes(userID string) ([]Route, error) {
	key := routeKey(userID)
	val, err := r.rdb.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return []Route{}, nil
	}
	if err != nil {
		return nil, err
	}
	var routes []Route
	json.Unmarshal([]byte(val), &routes)
	return routes, nil
}

// findRoute locates a route by userId, pluginId, capabilityType, and slot.
// Match order: exact match (pluginId+capabilityType+slot) → fallback match (pluginId+capabilityType, slot empty) / 根据 userId、pluginId、capabilityType、slot 四维查找路由
func (r *Router) findRoute(userID, pluginID, capabilityType, slot string) (*Route, error) {
	key := routeKey(userID)
	val, err := r.rdb.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var routes []Route
	if err := json.Unmarshal([]byte(val), &routes); err != nil {
		return nil, err
	}

	var fallback *Route
	for i, route := range routes {
		if route.PluginID != pluginID || route.CapabilityType != capabilityType {
			continue
		}
		if route.Slot == slot {
			return &routes[i], nil // Exact match / 精确匹配
		}
		if route.Slot == "" && slot != "" {
			fallback = &routes[i] // Record fallback candidate / 记录降级候选
		}
	}
	return fallback, nil
}

// forwardWithFailover tries candidates in order until one replies within the timeout or all fail.
// Only switches to the next candidate on "no reply (timeout)" or "send failure";
// a normal provider reply (even if the body is a business error) counts as success —
// we can't distinguish business errors from provider failures.
// Return value is used for observability classification:
//   - "ok": first candidate succeeded
//   - "failover": succeeded after switching to a later candidate
//   - "error": all candidates failed / 依次尝试候选 provider，超时或发送失败时切换下一个，正常回复即视为成功
func (r *Router) forwardWithFailover(ctx context.Context, original amqp.Delivery, route *Route, providers []ProviderEntry) string {
	for i := range providers {
		provider := &providers[i]
		slog.Info("router: forwarding",
			"pluginId", route.PluginID,
			"capabilityType", route.CapabilityType,
			"providerId", provider.ProviderID,
			"providerTopic", provider.ProviderTopic,
			"attempt", i+1, "of", len(providers))

		replied, err := r.tryProvider(ctx, original, route, provider)
		if err != nil {
			slog.Warn("router: provider attempt failed, trying next",
				"providerId", provider.ProviderID, "error", err)
			continue
		}
		if replied {
			if i == 0 {
				return "ok"
			}
			return "failover"
		}
		slog.Warn("router: provider timeout, trying next",
			"providerId", provider.ProviderID, "correlationId", original.CorrelationId)
	}

	// All candidates failed / 所有候选都失败
	slog.Warn("router: all providers failed",
		"tried", len(providers), "correlationId", original.CorrelationId)
	if original.ReplyTo != "" {
		if ch := r.invokeCh(); ch != nil {
			ch.PublishWithContext(ctx, "", original.ReplyTo, false, false, amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: original.CorrelationId,
				Body:          []byte(fmt.Sprintf(`{"error":"all providers failed (%d tried)"}`, len(providers))),
			})
		}
	}
	return "error"
}

// tryProvider forwards to a single provider and waits for a reply.
//   - replied=true: received a reply and forwarded to the original caller (success), or no reply needed (empty ReplyTo, send-and-forget succeeds)
//   - replied=false: timed out waiting; caller should switch to the next provider
//   - err != nil: queue creation or send failure; caller should switch / 向单个 provider 转发并等待回复
func (r *Router) tryProvider(ctx context.Context, original amqp.Delivery, route *Route, provider *ProviderEntry) (bool, error) {
	headers := amqp.Table{}
	for k, v := range original.Headers {
		headers[k] = v
	}
	headers["x-griffino-router-plugin"] = route.PluginID
	headers["x-griffino-router-provider"] = provider.ProviderID
	headers["x-griffino-original-reply-to"] = original.ReplyTo
	headers["x-griffino-original-correlation-id"] = original.CorrelationId

	// Snapshot the current channel for this whole attempt; if a concurrent reconnect
	// swaps/closes it, the operations below error and the caller fails over / 本次尝试取一次当前 channel.
	ch := r.invokeCh()
	if ch == nil {
		return false, fmt.Errorf("amqp channel unavailable")
	}

	// Fire-and-forget calls: send once and return; can't determine success, so no failover / 无需回复的调用：发一次即返回，无法判定成功，故不做失败切换
	if original.ReplyTo == "" {
		return true, r.publishToProvider(ctx, ch, original, provider, "", headers)
	}

	replyQ, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return false, fmt.Errorf("create reply queue: %w", err)
	}
	// This attempt has an exclusive consumer that is cancelled on function return; late replies fall into the auto-delete queue and are discarded,
	// so they won't leak into the next attempt or be forwarded twice / 本次尝试独占一个消费者，函数返回时取消，迟到回复落到 auto-delete 队列被丢弃
	consumerTag := "router-reply-" + replyQ.Name
	msgs, err := ch.Consume(replyQ.Name, consumerTag, true, true, false, false, nil)
	if err != nil {
		return false, fmt.Errorf("consume reply queue: %w", err)
	}
	defer ch.Cancel(consumerTag, false)

	if err := r.publishToProvider(ctx, ch, original, provider, replyQ.Name, headers); err != nil {
		return false, fmt.Errorf("publish: %w", err)
	}

	timeout := time.After(providerReplyTimeout)
	for {
		select {
		case <-timeout:
			return false, nil // Timeout → try the next candidate / 超时 → 切下一个
		case msg, ok := <-msgs:
			if !ok {
				return false, nil
			}
			ch.PublishWithContext(ctx, "", original.ReplyTo, false, false, amqp.Publishing{
				ContentType:   msg.ContentType,
				CorrelationId: original.CorrelationId,
				Body:          msg.Body,
			})
			return true, nil
		case <-r.ctx.Done():
			return true, nil // Shutdown: treat as processed, stop trying / 关停：当作已处理，停止尝试
		}
	}
}

func (r *Router) publishToProvider(ctx context.Context, ch *amqp.Channel, original amqp.Delivery, provider *ProviderEntry, replyTo string, headers amqp.Table) error {
	return ch.PublishWithContext(
		ctx,
		"griffino.plugins",
		provider.ProviderTopic,
		false, false,
		amqp.Publishing{
			ContentType:   original.ContentType,
			CorrelationId: original.CorrelationId,
			ReplyTo:       replyTo,
			Body:          original.Body,
			Headers:       headers,
			Expiration:    "60000",
		},
	)
}

// selectProvider returns the preferred provider by strategy (= orderedProviders[0]) / 返回按策略选出的首选 provider
func (r *Router) selectProvider(route *Route) *ProviderEntry {
	ordered := r.orderedProviders(route)
	if len(ordered) == 0 {
		return nil
	}
	return &ordered[0]
}

// orderedProviders returns failover candidates in order: filtered by health status first.
// fallback: preserves original order; round_robin: uses a weighted counter to pick the starting provider,
// then arranges them in ring order so the first rotates while the rest serve as fallback.
// Each call advances the round_robin counter / 返回失败切换的候选顺序
func (r *Router) orderedProviders(route *Route) []ProviderEntry {
	if len(route.Providers) == 0 {
		return nil
	}
	providers := compatibleProviders(r.healthyProviders(route.Providers))
	if len(providers) == 1 || route.Strategy != "round_robin" {
		return providers
	}

	totalWeight := 0
	for i := range providers {
		totalWeight += providerWeight(&providers[i])
	}
	pos := int(r.nextCount(routeSignature(route)) % uint64(totalWeight))
	start := 0
	for i := range providers {
		w := providerWeight(&providers[i])
		if pos < w {
			start = i
			break
		}
		pos -= w
	}

	ordered := make([]ProviderEntry, 0, len(providers))
	for i := 0; i < len(providers); i++ {
		ordered = append(ordered, providers[(start+i)%len(providers)])
	}
	return ordered
}

// healthyProviders filters out providers that are definitely unhealthy.
// If no health checker is injected or filtering yields an empty list, the original list is returned
// (prefer trying and reporting an error over silently dropping the request) / 过滤掉确定不健康的 provider，未注入检查器或过滤后为空时返回原列表
func (r *Router) healthyProviders(all []ProviderEntry) []ProviderEntry {
	if r.health == nil {
		return all
	}
	healthy := make([]ProviderEntry, 0, len(all))
	for _, p := range all {
		if r.health.IsProviderHealthy(p.ProviderID) {
			healthy = append(healthy, p)
		}
	}
	if len(healthy) == 0 {
		return all
	}
	return healthy
}

// interfaceMajor extracts the major version from an interfaceRef such as
// "griffino.interfaces.ai.chat@1.0.0" → "1". Returns "" when no version is present /
// 从 interfaceRef 提取主版本.
func interfaceMajor(ref string) string {
	at := strings.LastIndex(ref, "@")
	if at < 0 {
		return ""
	}
	ver := ref[at+1:]
	if dot := strings.IndexByte(ver, '.'); dot >= 0 {
		return ver[:dot]
	}
	return ver
}

// compatibleProviders keeps only providers whose interface major version matches the
// route's primary interface, so a provider declaring a different major is never
// silently treated as interchangeable. Providers without an interfaceRef are always
// kept (untyped, backward compatible); if filtering would drop everything, the
// original list is returned (prefer attempting over silently failing) /
// 仅保留主版本兼容的 provider；无 interfaceRef 的一律保留，过滤后为空则回退原列表.
func compatibleProviders(all []ProviderEntry) []ProviderEntry {
	ref := ""
	for _, p := range all {
		if m := interfaceMajor(p.InterfaceRef); m != "" {
			ref = m
			break
		}
	}
	if ref == "" {
		return all
	}
	out := make([]ProviderEntry, 0, len(all))
	for _, p := range all {
		if m := interfaceMajor(p.InterfaceRef); m == "" || m == ref {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func providerWeight(p *ProviderEntry) int {
	if p.Weight <= 0 {
		return 1
	}
	return p.Weight
}

// nextCount returns the next round-robin sequence number for the given route signature (starts at 0, monotonically increasing per process) / 返回指定 route 签名的下一个轮询序号
func (r *Router) nextCount(sig string) uint64 {
	r.rrMu.Lock()
	c, ok := r.rrCounters[sig]
	if !ok {
		c = new(atomic.Uint64)
		r.rrCounters[sig] = c
	}
	r.rrMu.Unlock()
	return c.Add(1) - 1
}

// routeSignature generates the round_robin counter key / 生成 round_robin 计数器的 key
func routeSignature(route *Route) string {
	return route.PluginID + "|" + route.CapabilityType + "|" + route.Slot
}

func (r *Router) replyError(msg amqp.Delivery, errMsg string) {
	if msg.ReplyTo == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"error": errMsg})
	ch := r.invokeCh()
	if ch == nil {
		return
	}
	ch.PublishWithContext(
		r.ctx, "", msg.ReplyTo, false, false,
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: msg.CorrelationId,
			Body:          body,
		},
	)
}

// PublishAction publishes an action message to the griffino.actions exchange.
// routingKey format: action.{pluginId}.{userId}.{actionId}.v1 / 向 griffino.actions exchange 发布一条动作消息
func (r *Router) PublishAction(routingKey string, body []byte) error {
	ch := r.invokeCh()
	if ch == nil {
		return fmt.Errorf("router: amqp channel unavailable")
	}
	return ch.PublishWithContext(
		r.ctx,
		actionsExchange,
		routingKey,
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}
