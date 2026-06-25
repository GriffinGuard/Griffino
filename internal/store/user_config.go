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

package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type UserConfigStore struct {
	rdb *redis.Client
}

func NewUserConfigStore(addr, password string) *UserConfigStore {
	return &UserConfigStore{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

func userConfigKey(userID, pluginID string) string {
	return fmt.Sprintf("user:%s:plugin:%s:config", userID, pluginID)
}

func (s *UserConfigStore) Get(ctx context.Context, userID, pluginID string) (map[string]any, error) {
	val, err := s.rdb.Get(ctx, userConfigKey(userID, pluginID)).Result()
	if err == redis.Nil {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *UserConfigStore) Set(ctx context.Context, userID, pluginID string, values map[string]any) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, userConfigKey(userID, pluginID), data, 0).Err()
}
