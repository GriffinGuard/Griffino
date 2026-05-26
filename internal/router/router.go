package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	routerExchange        = "griffino.router"
	actionsExchange       = "griffino.actions" 
	invokeQueueName       = "griffino.router.invoke.inbox"
	dispatchQueueName     = "griffino.router.dispatch.inbox"
	invokeBindingPattern  = "invoke.griffino.router.#"
	dispatchBindingPattern = "dispatch.griffino.router.#"
)

type ProviderEntry struct {
	ProviderID    string `json:"providerId"`
	ProviderTopic string `json:"providerTopic"`
	Weight        int    `json:"weight"`
}

// Scheduler 是 taskscheduler 包的抽象，Router 通过接口依赖，避免循环引用
type Scheduler interface {
	HandleDispatch(msg amqp.Delivery)
}

type Route struct {
	PluginID       string          `json:"pluginId"`
	Slot           string          `json:"slot,omitempty"`
	CapabilityType string          `json:"capabilityType"`
	Providers      []ProviderEntry `json:"providers"`
	Strategy       string          `json:"strategy"` // "fallback" | "round_robin"
}

type Router struct {
	rdb        *redis.Client
	amqpConn   *amqp.Connection
	amqpCh     *amqp.Channel     // invoke 专用 channel
	dispatchCh *amqp.Channel     // dispatch 专用 channel
	scheduler  Scheduler         // dispatch 消息处理器接口
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(redisAddr, password string, scheduler Scheduler) *Router {
	ctx, cancel := context.WithCancel(context.Background())
	return &Router{
		rdb: redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: password,
		}),
		scheduler: scheduler,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (r *Router) Start(amqpURL string) error {
	var err error

	r.amqpConn, err = amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("router: failed to connect to RabbitMQ: %w", err)
	}

	// invoke channel
	r.amqpCh, err = r.amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("router: failed to create invoke channel: %w", err)
	}

	// dispatch channel（独立，互不阻塞）
	r.dispatchCh, err = r.amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("router: failed to create dispatch channel: %w", err)
	}

	// 声明 exchange（幂等）
	for _, ch := range []*amqp.Channel{r.amqpCh, r.dispatchCh} {
		if err := ch.ExchangeDeclare(
			routerExchange, "topic", true, false, false, false, nil,
		); err != nil {
			return fmt.Errorf("router: failed to declare exchange: %w", err)
		}
	}

	// 声明 actions exchange（供插件动作触发使用）
	if err := r.amqpCh.ExchangeDeclare(
		actionsExchange, "topic", true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("router: failed to declare actions exchange: %w", err)
	}

	// invoke 队列
	invokeQ, err := r.amqpCh.QueueDeclare(
		invokeQueueName, true, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("router: failed to declare invoke queue: %w", err)
	}
	if err := r.amqpCh.QueueBind(
		invokeQ.Name, invokeBindingPattern, routerExchange, false, nil,
	); err != nil {
		return fmt.Errorf("router: failed to bind invoke queue: %w", err)
	}

	// dispatch 队列
	dispatchQ, err := r.dispatchCh.QueueDeclare(
		dispatchQueueName, true, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("router: failed to declare dispatch queue: %w", err)
	}
	if err := r.dispatchCh.QueueBind(
		dispatchQ.Name, dispatchBindingPattern, routerExchange, false, nil,
	); err != nil {
		return fmt.Errorf("router: failed to bind dispatch queue: %w", err)
	}

	// 启动 invoke 消费者
	invokeMsgs, err := r.amqpCh.Consume(
		invokeQ.Name, "griffino-router-invoke", false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("router: failed to start invoke consumer: %w", err)
	}

	// 启动 dispatch 消费者
	dispatchMsgs, err := r.dispatchCh.Consume(
		dispatchQ.Name, "griffino-router-dispatch", false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("router: failed to start dispatch consumer: %w", err)
	}

	slog.Info("router started",
		"invoke_pattern", invokeBindingPattern,
		"dispatch_pattern", dispatchBindingPattern)

	go r.processInvokeMessages(invokeMsgs)
	go r.processDispatchMessages(dispatchMsgs)
	return nil
}

func (r *Router) Stop() {
	r.cancel()
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
			// dispatch 消息交给 scheduler 处理，scheduler 内部起 goroutine
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
	// 最少：invoke(0) griffino(1) router(2) userId(3) capType(4) v1(5) = 6 段
	if len(parts) < 6 {
		slog.Warn("router: invalid routing key", "parts", len(parts), "key", msg.RoutingKey)
		r.replyError(msg, "invalid routing key format")
		msg.Ack(false)
		return
	}

	userID := parts[3]
	// parts[4:len(parts)-1] 去掉首部固定段和末尾 v1，剩余全部是 capabilityType
	capabilityType := strings.Join(parts[4:len(parts)-1], ".")

	// 从 body 解析 pluginId 和 slot
	var envelope struct {
		PluginID string `json:"pluginId"`
		Slot     string `json:"slot"`
	}
	if err := json.Unmarshal(msg.Body, &envelope); err != nil {
		slog.Warn("router: failed to parse message body", "error", err)
		r.replyError(msg, "invalid message body")
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
		msg.Ack(false)
		return
	}

	provider := r.selectProvider(route)
	if provider == nil {
		slog.Warn("router: no provider available", "route", route)
		r.replyError(msg, "no provider available")
		msg.Ack(false)
		return
	}

	slog.Info("router: forwarding",
		"pluginId", envelope.PluginID,
		"capabilityType", capabilityType,
		"slot", envelope.Slot,
		"providerId", provider.ProviderID,
		"providerTopic", provider.ProviderTopic)

	if err := r.forward(msg, route, provider); err != nil {
		slog.Error("router: forward failed", "error", err)
		r.replyError(msg, fmt.Sprintf("forward failed: %v", err))
	}
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

// findRoute 根据 userId、pluginId、capabilityType、slot 四维查找路由
// 匹配顺序：精确匹配(pluginId+capabilityType+slot) → 降级匹配(pluginId+capabilityType，slot为空)
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
			return &routes[i], nil // 精确匹配
		}
		if route.Slot == "" && slot != "" {
			fallback = &routes[i] // 记录降级候选
		}
	}
	return fallback, nil
}

func (r *Router) forward(original amqp.Delivery, route *Route, provider *ProviderEntry) error {
	headers := amqp.Table{}
	for k, v := range original.Headers {
		headers[k] = v
	}
	headers["x-griffino-router-plugin"] = route.PluginID
	headers["x-griffino-router-provider"] = provider.ProviderID
	headers["x-griffino-original-reply-to"] = original.ReplyTo
	headers["x-griffino-original-correlation-id"] = original.CorrelationId

	replyTo := ""
	if original.ReplyTo != "" {
		replyQ, err := r.amqpCh.QueueDeclare(
			"", false, true, true, false, nil,
		)
		if err != nil {
			return fmt.Errorf("failed to create reply queue: %w", err)
		}
		replyTo = replyQ.Name
		go r.awaitAndRelay(replyQ.Name, original.ReplyTo, original.CorrelationId)
	}

	return r.amqpCh.PublishWithContext(
		r.ctx,
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

// selectProvider 根据路由策略从 providers 列表中选出一个目标
// fallback：按顺序取第一个（未来可扩展为按健康状态跳过失败节点）
// round_robin：按请求计数轮询（当前简化为取第一个，完整实现留待后续）
func (r *Router) selectProvider(route *Route) *ProviderEntry {
	if len(route.Providers) == 0 {
		return nil
	}
	// TODO: round_robin 完整实现（需要原子计数器）
	return &route.Providers[0]
}

func (r *Router) awaitAndRelay(replyQName, originalReplyTo, correlationId string) {
	msgs, err := r.amqpCh.Consume(
		replyQName, "", true, true, false, false, nil,
	)
	if err != nil {
		slog.Error("router: failed to consume reply queue", "error", err)
		return
	}

	timeout := time.After(60 * time.Second)
	for {
		select {
		case <-timeout:
			slog.Warn("router: provider response timeout", "correlationId", correlationId)
			r.amqpCh.PublishWithContext(r.ctx, "", originalReplyTo, false, false, amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: correlationId,
				Body:          []byte(`{"error":"provider timeout"}`),
			})
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			r.amqpCh.PublishWithContext(r.ctx, "", originalReplyTo, false, false, amqp.Publishing{
				ContentType:   msg.ContentType,
				CorrelationId: correlationId,
				Body:          msg.Body,
			})
			return
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Router) replyError(msg amqp.Delivery, errMsg string) {
	if msg.ReplyTo == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"error": errMsg})
	r.amqpCh.PublishWithContext(
		r.ctx, "", msg.ReplyTo, false, false,
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: msg.CorrelationId,
			Body:          body,
		},
	)
}

// PublishAction 向 griffino.actions exchange 发布一条动作消息
// routingKey 格式：action.{pluginId}.{userId}.{actionId}.v1
func (r *Router) PublishAction(routingKey string, body []byte) error {
	return r.amqpCh.PublishWithContext(
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