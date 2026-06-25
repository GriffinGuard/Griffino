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

package redisacl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func NewClient(host string, port int, password string) *Client {
	return &Client{
		rdb: redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", host, port),
			Password: password,
		}),
	}
}

func (c *Client) setUser(ctx context.Context, username, password string, ownPatterns []string, readOnlyPatterns []string) error {
	args := []interface{}{
		"ACL", "SETUSER", username,
		"on",
		">" + password,
	}

	// plugin's own data: read-write access / 插件自身数据：读写权限
	for _, pattern := range ownPatterns {
		args = append(args, "~"+pattern)
	}

	// read-only data (user config): declare read-only keys with the %R prefix (Redis 7.0+)
	// 只读数据（用户配置）：用 %R 前缀声明只读 key
	for _, pattern := range readOnlyPatterns {
		args = append(args, "%R~"+pattern)
	}

	for _, cmd := range []string{
		"+ping",
		"+get", "+set", "+del", "+exists",
		"+hget", "+hset", "+hdel", "+hgetall", "+hmget", "+hmset",
		"+lpush", "+rpush", "+lrange", "+llen",
		"+sadd", "+smembers", "+srem",
		"+zadd", "+zrange", "+zrem", "+zscore",
		"+expire", "+ttl", "+type",
	} {
		args = append(args, cmd)
	}
	return c.rdb.Do(ctx, args...).Err()
}

// ProvisionPlugin creates a dedicated Redis user for a plugin / 为插件创建专属 Redis 用户.
func (c *Client) ProvisionPlugin(ctx context.Context, pluginID string) (username, password string, err error) {
	username = fmt.Sprintf("griffino.plugin.%s", pluginID)
	password, err = generatePassword()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate password: %w", err)
	}

	ownPatterns := []string{
		fmt.Sprintf("plugin:%s:*", pluginID),
		fmt.Sprintf("plugin:%s:state:*", pluginID), // status-view writes (redundant but explicit) / 状态视图写入
	}
	readOnlyPatterns := []string{
		fmt.Sprintf("user:*:plugin:%s:config", pluginID), // user config, read-only / 用户配置，只读
	}

	if err := c.setUser(ctx, username, password, ownPatterns, readOnlyPatterns); err != nil {
		return "", "", fmt.Errorf("failed to create Redis ACL user: %w", err)
	}
	slog.Info("created Redis ACL user", "username", username)
	return username, password, nil
}

// SyncPlugin updates an existing user's password (restart scenario) / 重启场景：更新已有用户密码.
func (c *Client) SyncPlugin(ctx context.Context, pluginID, password string) error {
	username := fmt.Sprintf("griffino.plugin.%s", pluginID)
	ownPatterns := []string{
		fmt.Sprintf("plugin:%s:*", pluginID),
		fmt.Sprintf("plugin:%s:state:*", pluginID), // status-view writes (redundant but explicit) / 状态视图写入
	}
	readOnlyPatterns := []string{
		fmt.Sprintf("user:*:plugin:%s:config", pluginID),
	}
	return c.setUser(ctx, username, password, ownPatterns, readOnlyPatterns)
}

// DeletePlugin removes a plugin's Redis ACL user / 删除插件的 Redis ACL 用户.
func (c *Client) DeletePlugin(ctx context.Context, pluginID string) error {
	username := fmt.Sprintf("griffino.plugin.%s", pluginID)
	return c.rdb.Do(ctx, "ACL", "DELUSER", username).Err()
}

func generatePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
