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

package env

import (
	"fmt"
	"strings"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
	tmpl "github.com/GriffinGuard/Griffino/pkg/template"
)

// ServiceEnvMap map[serviceId][]"KEY=VALUE"
type ServiceEnvMap map[string][]string

// Build assembles the final environment-variable list for each service.
// It handles three forms:
//
//	"KEY=VALUE"        → fixed value, used as-is
//	"KEY"              → looked up in adminConfig
//	"KEY={{...}}"      → template, resolved via the resolver
//
// 为每个服务组装环境变量列表，处理 固定值 / adminConfig 查找 / 模板 三种形式。
func Build(
	bootSpec *manifest.BootSpec,
	adminConfig map[string]map[string]string,
	ctx *tmpl.ResolveContext,
) (ServiceEnvMap, error) {
	result := make(ServiceEnvMap)

	for serviceID, svcSpec := range bootSpec.Services {
		var envList []string

		for _, raw := range svcSpec.Environment {
			resolved, err := resolveEnvEntry(raw, serviceID, adminConfig, ctx)
			if err != nil {
				return nil, fmt.Errorf("service %s: failed to resolve env entry %q: %w", serviceID, raw, err)
			}
			if resolved != "" {
				envList = append(envList, resolved)
			}
		}

		result[serviceID] = envList
	}

	return result, nil
}

// resolveEnvEntry resolves a single environment-variable declaration / 解析单条环境变量声明.
func resolveEnvEntry(
	raw string,
	serviceID string,
	adminConfig map[string]map[string]string,
	ctx *tmpl.ResolveContext,
) (string, error) {
	// Case 1: contains an "=" sign / 情况一：包含 = 号
	if idx := strings.Index(raw, "="); idx != -1 {
		key := raw[:idx]
		value := raw[idx+1:]

		// resolve if the value contains a template placeholder / value 含模板占位符则解析
		if strings.Contains(value, "{{") {
			resolved, err := tmpl.Resolve(value, ctx)
			if err != nil {
				return "", err
			}
			return key + "=" + resolved, nil
		}

		// otherwise it's a fixed value, returned directly / 否则是固定值，直接返回
		return raw, nil
	}

	// Case 2: KEY only, looked up in adminConfig / 情况二：只有 KEY，从 adminConfig 查找
	key := strings.TrimSpace(raw)
	svcConfig, ok := adminConfig[serviceID]
	if !ok {
		// service has no adminConfig, skip (e.g. redis and other config-free services)
		// 该服务无 adminConfig，跳过（如 redis 等无需配置的服务）
		return "", nil
	}
	value, ok := svcConfig[key]
	if !ok {
		// key absent from adminConfig, skip / adminConfig 里没有这个 key，跳过
		return "", nil
	}
	return key + "=" + value, nil
}
