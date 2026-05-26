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

// Build 为每个服务组装最终的环境变量列表
// 处理三种格式：
//   "KEY=VALUE"        → 固定值，直接使用
//   "KEY"              → 从 adminConfig 查找
//   "KEY={{...}}"      → 模板，通过 resolver 解析
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

// resolveEnvEntry 解析单条环境变量声明
func resolveEnvEntry(
    raw string,
    serviceID string,
    adminConfig map[string]map[string]string,
    ctx *tmpl.ResolveContext,
) (string, error) {
    // 情况一：包含 = 号
    if idx := strings.Index(raw, "="); idx != -1 {
        key := raw[:idx]
        value := raw[idx+1:]

        // 如果 value 包含模板占位符则解析
        if strings.Contains(value, "{{") {
            resolved, err := tmpl.Resolve(value, ctx)
            if err != nil {
                return "", err
            }
            return key + "=" + resolved, nil
        }

        // 否则是固定值，直接返回
        return raw, nil
    }

    // 情况二：只有 KEY，从 adminConfig 查找
    key := strings.TrimSpace(raw)
    svcConfig, ok := adminConfig[serviceID]
    if !ok {
        // 该服务没有 adminConfig，跳过（如 redis 这类无需配置的服务）
        return "", nil
    }
    value, ok := svcConfig[key]
    if !ok {
        // adminConfig 里没有这个 key，跳过
        return "", nil
    }
    return key + "=" + value, nil
}