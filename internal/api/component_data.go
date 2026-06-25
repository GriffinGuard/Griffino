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

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	componentDataMethod  = "__component_data"
	componentDataTimeout = 5 * time.Second
)

var errComponentDataNotSupported = errors.New("component data not supported")

type componentDataRenderer interface {
	RenderComponentData(ctx context.Context, pluginID, componentID, userID string) (map[string]any, error)
}

type rabbitComponentDataRenderer struct {
	sysMgr *system.Manager
}

func newRabbitComponentDataRenderer(sysMgr *system.Manager) componentDataRenderer {
	return &rabbitComponentDataRenderer{sysMgr: sysMgr}
}

func (r *rabbitComponentDataRenderer) RenderComponentData(ctx context.Context, pluginID, componentID, userID string) (map[string]any, error) {
	if r == nil || r.sysMgr == nil {
		return nil, errComponentDataNotSupported
	}
	sysState, err := r.sysMgr.GetSystemState()
	if err != nil {
		return nil, err
	}
	amqpURL := fmt.Sprintf("amqp://%s:%s@localhost:%d/",
		sysState.RabbitMQAdminUser,
		sysState.RabbitMQAdminPassword,
		sysState.RabbitMQPort,
	)

	callCtx, cancel := context.WithTimeout(ctx, componentDataTimeout)
	defer cancel()

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	replyQ, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return nil, err
	}
	msgs, err := ch.Consume(replyQ.Name, "", true, true, false, false, nil)
	if err != nil {
		return nil, err
	}
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))

	correlationID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{
		"msgId":  correlationID,
		"method": componentDataMethod,
		"payload": map[string]any{
			"componentID": componentID,
			"userId":      userID,
		},
	})
	queueName := fmt.Sprintf("plugin.%s.consumer.%s", pluginID, componentDataMethod)
	if err := ch.PublishWithContext(callCtx, "", queueName, true, false, amqp.Publishing{
		ContentType:   "application/json",
		CorrelationId: correlationID,
		ReplyTo:       replyQ.Name,
		Body:          body,
		Expiration:    fmt.Sprintf("%d", componentDataTimeout.Milliseconds()),
	}); err != nil {
		return nil, err
	}

	for {
		select {
		case <-callCtx.Done():
			return nil, callCtx.Err()
		case returned := <-returns:
			if returned.CorrelationId == "" || returned.CorrelationId == correlationID {
				return nil, errComponentDataNotSupported
			}
		case msg, ok := <-msgs:
			if !ok {
				return nil, errComponentDataNotSupported
			}
			if msg.CorrelationId != "" && msg.CorrelationId != correlationID {
				continue
			}
			return decodeComponentDataResponse(msg.Body)
		}
	}
}

func decodeComponentDataResponse(body []byte) (map[string]any, error) {
	var resp struct {
		Data  map[string]any `json:"data"`
		Error string         `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		if resp.Error == "not-supported" || resp.Error == "not_supported" || resp.Error == "unsupported" {
			return nil, errComponentDataNotSupported
		}
		return nil, errors.New(resp.Error)
	}
	if resp.Data == nil {
		return map[string]any{}, nil
	}
	return resp.Data, nil
}
