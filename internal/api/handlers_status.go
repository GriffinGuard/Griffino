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
	"fmt"

	"github.com/redis/go-redis/v9"
)

// statusViewReader is created during Server init for the widget-data handler to read plugin state directly from Redis.
// Uses admin account (system Redis), not subject to plugin ACL restrictions / 在 Server 初始化时创建，供 widget-data handler 直接读插件状态 Redis，使用 admin 账号，不受插件 ACL 限制.
type statusViewReader struct {
	rdb *redis.Client
}

func newStatusViewReader(addr, password string) *statusViewReader {
	return &statusViewReader{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

// readKV reads values at plugin:{pluginId}:state:{userId}:{key}.
// When keyPattern contains wildcards, uses KEYS to scan and return multiple relative keys; otherwise reads a single key / 读取 plugin:{pluginId}:state:{userId}:{key} 的值，含通配符时用 KEYS 扫描返回多个相对 key，否则读取单个 key.
func (r *statusViewReader) readKV(ctx context.Context, pluginID, userID, keyPattern string) (map[string]string, error) {
	prefix := fmt.Sprintf("plugin:%s:state:%s:", pluginID, userID)
	fullKey := prefix + keyPattern

	if containsWildcard(keyPattern) {
		keys, err := r.rdb.Keys(ctx, fullKey).Result()
		if err != nil {
			return nil, err
		}
		result := make(map[string]string, len(keys))
		for _, k := range keys {
			val, err := r.rdb.Get(ctx, k).Result()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				return nil, err
			}
			// Strip prefix, keep only relative key / 去掉 prefix，只保留相对 key
			result[k[len(prefix):]] = val
		}
		return result, nil
	}

	// Single key / 单 key
	val, err := r.rdb.Get(ctx, fullKey).Result()
	if err == redis.Nil {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{keyPattern: val}, nil
}

func containsWildcard(s string) bool {
	for _, c := range s {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}
