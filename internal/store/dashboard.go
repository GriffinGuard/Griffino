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

// DashboardStore saves per-user dashboard layouts. The backend treats them as opaque JSON —
// no schema validation (DashboardState shape evolves with the Web-UI), just per-user read/write / 保存每个用户的仪表盘布局，后端当作不透明 JSON 存储，不校验结构，仅按用户隔离读写.
type DashboardStore struct {
	rdb *redis.Client
}

func NewDashboardStore(addr, password string) *DashboardStore {
	return &DashboardStore{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

func dashboardKey(userID string) string {
	return fmt.Sprintf("user:%s:dashboard", userID)
}

// Get returns the user's saved raw dashboard JSON; returns (nil, nil) if never saved / 返回用户保存的原始仪表盘 JSON，从未保存过时返回 (nil, nil).
func (s *DashboardStore) Get(ctx context.Context, userID string) (json.RawMessage, error) {
	val, err := s.rdb.Get(ctx, dashboardKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(val), nil
}

// Set stores the dashboard JSON as-is / 原样存储传入的仪表盘 JSON.
func (s *DashboardStore) Set(ctx context.Context, userID string, state json.RawMessage) error {
	return s.rdb.Set(ctx, dashboardKey(userID), []byte(state), 0).Err()
}
