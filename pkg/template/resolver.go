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

package template

import (
	"fmt"
	"regexp"
	"strings"
)

// SystemContext is system-level context used to resolve {{system.*}} placeholders / 系统级上下文.
type SystemContext struct {
	RabbitMQ RabbitMQContext
	Redis    RedisContext
}

type RabbitMQContext struct {
	Host     string
	Port     int
	User     string
	Password string
	Vhost    string
}

type RedisContext struct {
	Host     string
	Port     int
	User     string
	Password string
}

// ServiceContext is runtime info of a started service, used to resolve {{services.*}}
// placeholders / 已启动服务的运行时信息.
type ServiceContext struct {
	// container name / 容器名称
	Name string
	// map[portName]internalPort
	Ports map[string]int
}

// ResolveContext is the full resolution context / 完整的解析上下文.
type ResolveContext struct {
	System   SystemContext
	Services map[string]ServiceContext
}

var placeholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Resolve resolves all placeholders in a single string / 解析字符串中的所有占位符.
// e.g. "http://{{services.redis.name}}:6379" → "http://griffino_telegram_bot_redis:6379"
func Resolve(tmpl string, ctx *ResolveContext) (string, error) {
	var resolveErr error

	result := placeholderRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		if resolveErr != nil {
			return match
		}
		// strip {{ }} to get the inner key / 去掉 {{ }} 取出内部 key
		key := strings.TrimSpace(match[2 : len(match)-2])
		value, err := resolveKey(key, ctx)
		if err != nil {
			resolveErr = err
			return match
		}
		return value
	})

	return result, resolveErr
}

// resolveKey looks up a value from the context by dot-separated key / 按点分隔 key 从 context 取值.
func resolveKey(key string, ctx *ResolveContext) (string, error) {
	parts := strings.Split(key, ".")

	switch parts[0] {
	case "system":
		return resolveSystemKey(parts[1:], ctx)
	case "services":
		return resolveServiceKey(parts[1:], ctx)
	default:
		return "", fmt.Errorf("unknown placeholder namespace: %s", parts[0])
	}
}

func resolveSystemKey(parts []string, ctx *ResolveContext) (string, error) {
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid system placeholder format, expected system.<component>.<field>")
	}
	component, field := parts[0], parts[1]

	switch component {
	case "rabbitmq":
		switch field {
		case "host":
			return ctx.System.RabbitMQ.Host, nil
		case "port":
			return fmt.Sprintf("%d", ctx.System.RabbitMQ.Port), nil
		case "user":
			return ctx.System.RabbitMQ.User, nil
		case "password":
			return ctx.System.RabbitMQ.Password, nil
		case "vhost":
			return ctx.System.RabbitMQ.Vhost, nil
		default:
			return "", fmt.Errorf("unknown rabbitmq field: %s", field)
		}
	case "redis":
		switch field {
		case "host":
			return ctx.System.Redis.Host, nil
		case "port":
			return fmt.Sprintf("%d", ctx.System.Redis.Port), nil
		case "user":
			return ctx.System.Redis.User, nil
		case "password":
			return ctx.System.Redis.Password, nil
		default:
			return "", fmt.Errorf("unknown redis field: %s", field)
		}
	default:
		return "", fmt.Errorf("unknown system component: %s", component)
	}
}

func resolveServiceKey(parts []string, ctx *ResolveContext) (string, error) {
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid services placeholder format")
	}
	serviceID := parts[0]
	svc, ok := ctx.Services[serviceID]
	if !ok {
		return "", fmt.Errorf("service %s not found in runtime context", serviceID)
	}

	switch parts[1] {
	case "name":
		return svc.Name, nil
	case "ports":
		// format: services.<serviceId>.ports.<portName>.internal / 格式见此
		if len(parts) < 4 {
			return "", fmt.Errorf("invalid ports placeholder format, expected services.<id>.ports.<portName>.internal")
		}
		portName := parts[2]
		port, ok := svc.Ports[portName]
		if !ok {
			return "", fmt.Errorf("service %s has no port named %s", serviceID, portName)
		}
		return fmt.Sprintf("%d", port), nil
	default:
		return "", fmt.Errorf("unknown service field: %s", parts[1])
	}
}
